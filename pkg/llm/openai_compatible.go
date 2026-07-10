package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	sharedjson "github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/jsonutil"
	"github.com/park285/shared-go/pkg/llm/internal/openaidiag"
)

var (
	ErrOpenAIEmptyOutput   = openaidiag.ErrEmptyOutput
	ErrOpenAIRefusalOutput = openaidiag.ErrRefusalOutput
)

type OpenAICompatibleConfig struct {
	BaseURL                      string
	APIKey                       string
	HTTPClient                   *http.Client
	AllowChatCompletionsFallback bool
}

type OpenAICompatibleJSONGenerator struct {
	client                       openai.Client
	allowChatCompletionsFallback bool
}

func NewOpenAICompatibleJSONGenerator(cfg OpenAICompatibleConfig) (*OpenAICompatibleJSONGenerator, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("llm: openai-compatible api key is empty")
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	return &OpenAICompatibleJSONGenerator{
		client:                       openai.NewClient(opts...),
		allowChatCompletionsFallback: cfg.AllowChatCompletionsFallback,
	}, nil
}

func (g *OpenAICompatibleJSONGenerator) GenerateJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	if g == nil {
		return JSONResponse{}, ErrNilJSONGenerator
	}
	if ctx == nil {
		return JSONResponse{}, ErrNilContext
	}
	if err := ValidateJSONRequest(req); err != nil {
		return JSONResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return JSONResponse{}, err
	}

	if req.ChatCompletions {
		resp, err := g.generateChatCompletionsJSON(ctx, req)
		return resp, safeOpenAICompatibleError(err)
	}

	resp, err := g.generateResponsesJSON(ctx, req)
	if err == nil {
		return resp, nil
	}

	if !g.allowChatCompletionsFallback || !shouldFallbackToChatCompletions(err) {
		return JSONResponse{}, safeOpenAICompatibleError(err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return JSONResponse{}, ctxErr
	}

	fallbackResp, fallbackErr := g.generateChatCompletionsJSON(ctx, req)
	if fallbackErr != nil {
		return JSONResponse{}, fmt.Errorf("openai responses failed (%w) and chat completions fallback failed: %w", safeOpenAICompatibleError(err), safeOpenAICompatibleError(fallbackErr))
	}
	fallbackResp.FallbackUsed = true
	return fallbackResp, nil
}

const defaultResponsesSchemaName = "schema"

var responsesSchemaNameInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// OpenAI Responses API의 text.format.name은 [a-zA-Z0-9_-](최대 64자)만 허용한다.
// taskName을 그대로 schema name으로 넘기면 점 등 비허용 문자가 섞여 400(invalid_value)이 난다.
func sanitizeResponsesSchemaName(name string) string {
	cleaned := responsesSchemaNameInvalidChars.ReplaceAllString(strings.TrimSpace(name), "_")
	if cleaned == "" {
		return defaultResponsesSchemaName
	}
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned
}

func (g *OpenAICompatibleJSONGenerator) generateResponsesJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	params := responses.ResponseNewParams{
		Model:        req.Model,
		Instructions: openai.String(req.SystemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(req.UserPrompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   sanitizeResponsesSchemaName(req.SchemaName),
					Schema: req.Schema,
					Strict: openai.Bool(true),
				},
			},
		},
	}
	if req.WebSearch {
		params.Tools = []responses.ToolUnionParam{
			responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch),
		}
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort)}
	}

	resp, err := g.client.Responses.New(ctx, params)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("openai responses API: %w", err)
	}

	text, err := extractResponsesOutputText(resp)
	if err != nil {
		return JSONResponse{}, err
	}
	return JSONResponse{
		Text:  text,
		Model: responseModel(resp, req.Model),
		Usage: usageFromResponse(resp),
	}, nil
}

func (g *OpenAICompatibleJSONGenerator) generateChatCompletionsJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	systemPrompt, err := chatCompletionsSystemPrompt(req.SystemPrompt, req.Schema)
	if err != nil {
		return JSONResponse{}, err
	}

	params := openai.ChatCompletionNewParams{
		Model: req.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(req.UserPrompt),
		},
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(effort)
	}

	completion, err := g.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("openai chat completions API: %w", err)
	}
	if len(completion.Choices) == 0 {
		return JSONResponse{}, fmt.Errorf("%w: choices=0", ErrOpenAIEmptyOutput)
	}

	extracted, err := jsonutil.Extract(completion.Choices[0].Message.Content)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("openai chat completions JSON extract failed: %w", err)
	}

	return JSONResponse{
		Text:  string(extracted),
		Model: strings.TrimSpace(completion.Model),
		Usage: usageFromChatCompletion(completion),
	}, nil
}

func chatCompletionsSystemPrompt(systemPrompt string, schema map[string]any) (string, error) {
	schemaJSON, err := sharedjson.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("marshal json schema: %w", err)
	}
	return fmt.Sprintf(`%s

IMPORTANT: You MUST respond with ONLY a valid JSON object that follows this schema (no markdown, no explanation, just the JSON):
%s

Do not include any text before or after the JSON. Only output the JSON object.`, systemPrompt, string(schemaJSON)), nil
}

func responseModel(resp *responses.Response, requestedModel string) string {
	if resp == nil {
		return strings.TrimSpace(requestedModel)
	}
	if model := strings.TrimSpace(resp.Model); model != "" {
		return model
	}
	return strings.TrimSpace(requestedModel)
}

func usageFromResponse(resp *responses.Response) Usage {
	if resp == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:           int(resp.Usage.InputTokens),
		OutputTokens:          int(resp.Usage.OutputTokens),
		TotalTokens:           int(resp.Usage.TotalTokens),
		CachedInputTokens:     int(resp.Usage.InputTokensDetails.CachedTokens),
		ReasoningOutputTokens: int(resp.Usage.OutputTokensDetails.ReasoningTokens),
	}
}

func usageFromChatCompletion(completion *openai.ChatCompletion) Usage {
	if completion == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:           int(completion.Usage.PromptTokens),
		OutputTokens:          int(completion.Usage.CompletionTokens),
		TotalTokens:           int(completion.Usage.TotalTokens),
		CachedInputTokens:     int(completion.Usage.PromptTokensDetails.CachedTokens),
		ReasoningOutputTokens: int(completion.Usage.CompletionTokensDetails.ReasoningTokens),
	}
}
