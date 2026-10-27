package llm

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/park285/shared-go/v2/pkg/internal/jsonutil"
	"github.com/park285/shared-go/v2/pkg/llm/internal/openaicfg"
	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
)

var (
	ErrOpenAIEmptyOutput   = openaidiag.ErrEmptyOutput
	ErrOpenAIRefusalOutput = openaidiag.ErrRefusalOutput
)

type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	// MaxRetries는 호출자가 정하는 SDK 재시도 수다. 0은 재시도 없음이고 음수는
	// NewOpenAICompatibleJSONGenerator가 거부한다. 라이브러리 기본값은 없다.
	// 소비자 자체 재시도와 겹치면 총 시도는 (소비자 시도) × (1 + MaxRetries)다.
	MaxRetries int
}

type OpenAICompatibleJSONGenerator struct {
	client openai.Client
}

func NewOpenAICompatibleJSONGenerator(cfg OpenAICompatibleConfig) (*OpenAICompatibleJSONGenerator, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("llm: openai-compatible api key is empty")
	}

	if err := openaicfg.ValidateMaxRetries(cfg.MaxRetries); err != nil {
		return nil, fmt.Errorf("llm: openai-compatible config: %w", err)
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(cfg.MaxRetries),
	}
	if baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}

	return &OpenAICompatibleJSONGenerator{client: openai.NewClient(opts...)}, nil
}

func (g *OpenAICompatibleJSONGenerator) GenerateJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	if g == nil {
		return JSONResponse{}, ErrNilJSONGenerator
	}

	if ctx == nil {
		return JSONResponse{}, ErrNilContext
	}

	if err := validateJSONRequest(req); err != nil {
		return JSONResponse{}, err
	}

	if err := ctx.Err(); err != nil {
		return JSONResponse{}, fmt.Errorf("llm: context done: %w", err)
	}

	if req.ChatCompletions {
		resp, err := g.generateChatCompletionsJSON(ctx, req)
		if safeErr := safeOpenAICompatibleError(err); safeErr != nil {
			return resp, fmt.Errorf("llm: safe open AI compatible error: %w", safeErr)
		}

		return resp, nil
	}

	resp, err := g.generateResponsesJSON(ctx, req)
	if safeErr := safeOpenAICompatibleError(err); safeErr != nil {
		return JSONResponse{}, fmt.Errorf("llm: safe open AI compatible error: %w", safeErr)
	}

	return resp, nil
}

func (g *OpenAICompatibleJSONGenerator) generateResponsesJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	messages, err := AdaptInstructionMessages(layeredJSONMessages(req), InstructionProfileForModel(req.Model))
	if err != nil {
		return JSONResponse{}, fmt.Errorf("adapt instruction messages: %w", err)
	}

	params := responses.ResponseNewParams{
		Model: req.Model,
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   openaicfg.ResponsesSchemaName(req.SchemaName),
					Schema: req.Schema,
					Strict: openai.Bool(true),
				},
			},
		},
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: responsesInput(messages)},
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

	if cacheKey := strings.TrimSpace(req.CacheKey); cacheKey != "" {
		params.PromptCacheKey = openai.String(cacheKey)
	}

	resp, err := g.client.Responses.New(ctx, params)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("openai responses API: %w", err)
	}

	text, err := extractResponsesOutputText(resp)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("extract responses output text: %w", err)
	}

	return JSONResponse{
		Text:  text,
		Model: responseModel(resp, req.Model),
		Usage: usageFromResponse(resp),
	}, nil
}

func (g *OpenAICompatibleJSONGenerator) generateChatCompletionsJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	messages, err := AdaptInstructionMessages(layeredJSONMessages(req), InstructionProfileSingleSystem)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("adapt instruction messages: %w", err)
	}

	systemPrompt, err := chatCompletionsSystemPrompt(singleSystemInstruction(messages), req.Schema)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("chat completions system prompt: %w", err)
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

	if cacheKey := strings.TrimSpace(req.CacheKey); cacheKey != "" {
		params.PromptCacheKey = openai.String(cacheKey)
	}

	completion, err := g.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("openai chat completions API: %w", err)
	}

	if completion == nil {
		return JSONResponse{}, fmt.Errorf("%w: null chat completion", ErrOpenAIEmptyOutput)
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

// layeredJSONMessages는 요청을 invariant·developer·user 순서의 메시지로 만든다. 빈 지시 계층은
// AdaptInstructionMessages가 건너뛰고, user 메시지는 항상 마지막에 남는다.
func layeredJSONMessages(req JSONRequest) []Message {
	return []Message{
		{Role: roleSystem, Content: req.InvariantPrompt},
		{Role: roleDeveloper, Content: req.DeveloperPrompt},
		{Role: roleUser, Content: req.UserPrompt},
	}
}

// singleSystemInstruction은 단일 system profile로 변환한 메시지에서 지시를 꺼낸다. 이 profile은
// 지시 계층이 있을 때만 system 메시지 하나를 맨 앞에 두므로, 첫 메시지가 user이면 지시가 없는 것이다.
func singleSystemInstruction(messages []Message) string {
	if len(messages) > 0 && messages[0].Role == roleSystem {
		return messages[0].Content
	}

	return ""
}

func responsesInput(messages []Message) responses.ResponseInputParam {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, message := range messages {
		input = append(input, responses.ResponseInputItemParamOfMessage(message.Content, responsesRole(message.Role)))
	}

	return input
}

func responsesRole(role string) responses.EasyInputMessageRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case roleDeveloper:
		return responses.EasyInputMessageRoleDeveloper
	case roleSystem:
		return responses.EasyInputMessageRoleSystem
	case roleAssistant:
		return responses.EasyInputMessageRoleAssistant
	default:
		return responses.EasyInputMessageRoleUser
	}
}

func chatCompletionsSystemPrompt(systemPrompt string, schema map[string]any) (string, error) {
	schemaJSON, err := jsonv2.Marshal(schema)
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
		CacheWriteTokens:      int(resp.Usage.InputTokensDetails.CacheWriteTokens),
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
		CacheWriteTokens:      int(completion.Usage.PromptTokensDetails.CacheWriteTokens),
		ReasoningOutputTokens: int(completion.Usage.CompletionTokensDetails.ReasoningTokens),
	}
}
