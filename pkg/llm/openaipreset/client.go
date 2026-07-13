package openaipreset

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/pkg/httputil"
	sharedjson "github.com/park285/shared-go/pkg/json"
	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/llm/internal/openaidiag"
	"github.com/park285/shared-go/pkg/logging"
)

const (
	providerLabel         = "openai"
	defaultSchemaName     = "response"
	defaultRequestTimeout = 120 * time.Second
)

var errClientNil = errors.New("openaipreset: client is nil")

// ErrResponsesJSONRequired는 strict Responses JSON contract를 fallback 없이 충족할 수 없을 때 반환된다.
var ErrResponsesJSONRequired = errors.New("openaipreset: responses JSON transport required")

type Client struct {
	generator                    sharedllm.JSONGenerator
	openai                       openai.Client
	model                        string
	schemaName                   string
	temperature                  *float64
	reasoningEffort              string
	webSearch                    bool
	chatCompletions              bool
	allowChatCompletionsFallback bool
	usageReporter                sharedllm.UsageReporter
	logger                       *slog.Logger
}

type PromptLayers struct {
	Invariant string
	Developer string
	User      string
}

func New(baseURL, apiKey, model string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("openaipreset: model is empty")
	}

	cfg := &config{schemaName: defaultSchemaName}
	for _, opt := range opts {
		opt(cfg)
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	requestOpts := []option.RequestOption{option.WithAPIKey(strings.TrimSpace(apiKey))}
	if normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalizedBaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(normalizedBaseURL))
	}
	requestOpts = append(requestOpts, option.WithHTTPClient(httpClient))

	generator, err := sharedllm.NewOpenAICompatibleJSONGenerator(sharedllm.OpenAICompatibleConfig{
		BaseURL:                      baseURL,
		APIKey:                       apiKey,
		HTTPClient:                   httpClient,
		AllowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
	})
	if err != nil {
		return nil, err
	}

	reporter := cfg.usageReporter
	if reporter == nil {
		reporter = sharedllm.NoopUsageReporter{}
	}

	return &Client{
		generator:                    generator,
		openai:                       openai.NewClient(requestOpts...),
		model:                        strings.TrimSpace(model),
		schemaName:                   cfg.schemaName,
		temperature:                  cfg.temperature,
		reasoningEffort:              cfg.reasoningEffort,
		webSearch:                    cfg.webSearch,
		chatCompletions:              cfg.chatCompletions,
		allowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
		usageReporter:                reporter,
		logger:                       cfg.logger,
	}, nil
}

// reasoning·web-search 모델은 첫 응답 헤더까지 수십 초가 걸려 externalAPI 프로파일의 15s로는 끊긴다.
func defaultHTTPClient() *http.Client {
	return httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:               defaultRequestTimeout,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: defaultRequestTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          128,
		MaxConnsPerHost:       32,
		MaxIdleConnsPerHost:   16,
	})
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *Client) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c == nil {
		return "", errClientNil
	}
	resp, err := c.generate(ctx, c.schemaName, systemPrompt, "", "", userPrompt, schema)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (c *Client) GenerateJSONInto(
	ctx context.Context,
	task string,
	prompts PromptLayers,
	schema map[string]any,
	out any,
) error {
	if c == nil {
		return errClientNil
	}
	if isNilOutputTarget(out) {
		return errors.New("openaipreset: output target is nil")
	}
	resp, err := c.generate(ctx, task, "", prompts.Invariant, prompts.Developer, prompts.User, schema)
	if err != nil {
		return err
	}
	return decodeJSONInto(task, resp.Text, out)
}

// GenerateLayeredResponsesJSON returns every Responses output_text fragment
// before object extraction or destination decoding. It is intentionally strict:
// callers using this boundary can validate the complete provider surface first.
func (c *Client) GenerateLayeredResponsesJSON(ctx context.Context, task string, prompts PromptLayers, schema map[string]any) (string, error) {
	if c == nil {
		return "", errClientNil
	}
	if c.chatCompletions || c.allowChatCompletionsFallback {
		return "", ErrResponsesJSONRequired
	}
	profile := sharedllm.InstructionProfileOpenAI
	params, model, err := c.completionParams(CompletionRequest{
		Messages: []Message{{Role: "system", Content: prompts.Invariant}, {Role: "developer", Content: prompts.Developer}, {Role: "user", Content: prompts.User}},
		Model:    c.model, ResponseFormat: &ResponseFormat{Name: sharedllm.ResponsesSchemaName(task), Schema: schema, Strict: true}, InstructionProfile: &profile,
	})
	if err != nil {
		return "", err
	}
	attrs := promptSummaryAttrs(model, prompts.Invariant+"\n"+prompts.Developer+"\n"+prompts.User)
	return runRequest(ctx, c.logger, attrs, func() (string, error) {
		resp, err := c.openai.Responses.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("openai responses API: %w", openaidiag.SafeError(err))
		}
		responseModel := strings.TrimSpace(resp.Model)
		if responseModel == "" {
			responseModel = model
		}
		c.usageReporter.RecordUsage(ctx, providerLabel, responseModel, sharedllm.Usage{
			InputTokens:           int(resp.Usage.InputTokens),
			OutputTokens:          int(resp.Usage.OutputTokens),
			TotalTokens:           int(resp.Usage.TotalTokens),
			CachedInputTokens:     int(resp.Usage.InputTokensDetails.CachedTokens),
			ReasoningOutputTokens: int(resp.Usage.OutputTokensDetails.ReasoningTokens),
		})
		text := completeResponsesOutputText(resp)
		if strings.TrimSpace(text) == "" {
			return "", sharedllm.ErrOpenAIEmptyOutput
		}
		return text, nil
	})
}

func completeResponsesOutputText(resp *responses.Response) string {
	if resp == nil {
		return ""
	}
	var out strings.Builder
	for itemIndex := range resp.Output {
		item := &resp.Output[itemIndex]
		if item.Type != "message" {
			continue
		}
		for contentIndex := range item.Content {
			content := &item.Content[contentIndex]
			if content.Type == "output_text" {
				out.WriteString(content.Text)
			}
		}
	}
	return out.String()
}

func isNilOutputTarget(out any) bool {
	if out == nil {
		return true
	}
	value := reflect.ValueOf(out)
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Interface:
		return value.IsNil()
	default:
		return false
	}
}

func (c *Client) RunInto(ctx context.Context, task, prompt string, schema map[string]any, out any) error {
	if c == nil {
		return errClientNil
	}
	if isNilOutputTarget(out) {
		return errors.New("openaipreset: output target is nil")
	}
	resp, err := c.generate(ctx, task, "", "", "", prompt, schema)
	if err != nil {
		return err
	}
	return decodeJSONInto(task, resp.Text, out)
}

func (c *Client) generate(
	ctx context.Context,
	taskName, systemPrompt, invariantPrompt, developerPrompt, userPrompt string,
	schema map[string]any,
) (sharedllm.JSONResponse, error) {
	prompt := systemPrompt + "\n" + userPrompt
	hasInvariantPrompt := strings.TrimSpace(invariantPrompt) != ""
	hasDeveloperPrompt := strings.TrimSpace(developerPrompt) != ""
	if hasInvariantPrompt || hasDeveloperPrompt {
		promptLayers := make([]string, 0, 3)
		if hasInvariantPrompt {
			promptLayers = append(promptLayers, invariantPrompt)
		}
		if hasDeveloperPrompt {
			promptLayers = append(promptLayers, developerPrompt)
		}
		prompt = strings.Join(append(promptLayers, userPrompt), "\n")
	}
	attrs := promptSummaryAttrs(c.model, strings.TrimSpace(prompt))
	return runRequest(ctx, c.logger, attrs, func() (sharedllm.JSONResponse, error) {
		return sharedllm.RunJSON(ctx, c.generator, sharedllm.JSONRequest{
			TaskName:        taskName,
			SystemPrompt:    systemPrompt,
			UserPrompt:      userPrompt,
			InvariantPrompt: invariantPrompt,
			DeveloperPrompt: developerPrompt,
			SchemaName:      taskName,
			Schema:          schema,
			Model:           c.model,
			Temperature:     c.temperature,
			ReasoningEffort: c.reasoningEffort,
			WebSearch:       c.webSearch,
			ChatCompletions: c.chatCompletions,
		}, providerLabel, c.usageReporter)
	})
}

func decodeJSONInto(task, text string, out any) error {
	if err := sharedjson.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("decode %s json failed", strings.TrimSpace(task))
	}
	return nil
}

func runRequest[T any](ctx context.Context, logger *slog.Logger, attrs []slog.Attr, run func() (T, error)) (T, error) {
	logging.Info(ctx, logger, "llm.request.started", "llm request started", attrs...)
	started := time.Now()
	resp, err := run()
	elapsed := logging.SinceMS(started)
	if err != nil {
		logging.Error(ctx, logger, "llm.request.failed", "llm request failed", append(attrs, elapsed)...)
		var zero T
		return zero, err
	}
	logging.Info(ctx, logger, "llm.request.succeeded", "llm request succeeded", append(attrs, elapsed)...)
	return resp, nil
}

func promptSummaryAttrs(model, prompt string) []slog.Attr {
	prompt = strings.TrimSpace(prompt)
	attrs := []slog.Attr{
		slog.String("provider", providerLabel),
		slog.String("model", model),
		slog.Int("prompt_len", len(prompt)),
	}
	return attrs
}
