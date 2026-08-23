package openaipreset

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/v2/pkg/httputil"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
	"github.com/park285/shared-go/v2/pkg/logging"
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
	promptCacheKeyPrefix         string
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

	requestOpts := []option.RequestOption{
		option.WithAPIKey(strings.TrimSpace(apiKey)),
		option.WithMaxRetries(sharedllm.ResolveOpenAIMaxRetries(cfg.maxRetries)),
	}
	if normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalizedBaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(normalizedBaseURL))
	}
	requestOpts = append(requestOpts, option.WithHTTPClient(httpClient))

	generator, err := sharedllm.NewOpenAICompatibleJSONGenerator(sharedllm.OpenAICompatibleConfig{
		BaseURL:                      baseURL,
		APIKey:                       apiKey,
		HTTPClient:                   httpClient,
		AllowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
		MaxRetries:                   cfg.maxRetries,
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
		promptCacheKeyPrefix:         cfg.promptCacheKeyPrefix,
	}, nil
}

func (c *Client) promptCacheKeyFor(task string) string {
	if c.promptCacheKeyPrefix == "" {
		return ""
	}
	return c.promptCacheKeyPrefix + strings.TrimSpace(task)
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
		CacheKey: c.promptCacheKeyFor(task),
	})
	if err != nil {
		return "", err
	}
	attrs := promptSummaryAttrs(model, joinedPromptLen(prompts.Invariant, prompts.Developer, prompts.User))
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

func (c *Client) generate(
	ctx context.Context,
	taskName, systemPrompt, invariantPrompt, developerPrompt, userPrompt string,
	schema map[string]any,
) (sharedllm.JSONResponse, error) {
	attrs := promptSummaryAttrs(c.model, layeredPromptLen(systemPrompt, invariantPrompt, developerPrompt, userPrompt))
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
			CacheKey:        c.promptCacheKeyFor(taskName),
		}, providerLabel, c.usageReporter)
	})
}

func decodeJSONInto(task, text string, out any) error {
	if err := jsonv2.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("decode %s json failed: %w", strings.TrimSpace(task), &redactedCauseError{cause: err})
	}
	return nil
}

// JSON decode error 메시지는 실패 지점 주변 원문을 담을 수 있으므로 렌더링하면 provider
// 출력이 샌다(TestGenerateJSONIntoDecodeErrorOmitsProviderOutput). 원문은 Unwrap으로만
// 전달해 errors.Is/As 대상 클래스를 보존하고, 메시지에는 타입만 남긴다.
type redactedCauseError struct{ cause error }

func (e *redactedCauseError) Error() string {
	return sharedllm.RedactDiagnostic(openaidiag.ErrorClass(e.cause), sharedllm.DefaultDiagnosticLimit)
}

func (e *redactedCauseError) Unwrap() error { return e.cause }

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

func promptSummaryAttrs(model string, promptLen int) []slog.Attr {
	return []slog.Attr{
		slog.String("provider", providerLabel),
		slog.String("model", model),
		slog.Int("prompt_len", promptLen),
	}
}

func layeredPromptLen(systemPrompt, invariantPrompt, developerPrompt, userPrompt string) int {
	hasInvariantPrompt := strings.TrimSpace(invariantPrompt) != ""
	hasDeveloperPrompt := strings.TrimSpace(developerPrompt) != ""
	switch {
	case hasInvariantPrompt && hasDeveloperPrompt:
		return joinedPromptLen(invariantPrompt, developerPrompt, userPrompt)
	case hasInvariantPrompt:
		return joinedPromptLen(invariantPrompt, userPrompt)
	case hasDeveloperPrompt:
		return joinedPromptLen(developerPrompt, userPrompt)
	default:
		return joinedPromptLen(systemPrompt, userPrompt)
	}
}

// "\n"으로 이은 계층의 TrimSpace 길이를 연결 없이 센다. 구분자가 ASCII 공백이라
// 계층 경계를 rune이 가로지르지 않으므로 계층별 trim 결과를 그대로 합산할 수 있다.
func joinedPromptLen(parts ...string) int {
	total := 0
	for i, part := range parts {
		if i > 0 {
			total++
		}
		total += len(part)
	}

	lead := 0
	for i, part := range parts {
		if i > 0 {
			lead++
		}
		trimmed := strings.TrimLeftFunc(part, unicode.IsSpace)
		lead += len(part) - len(trimmed)
		if trimmed != "" {
			break
		}
	}
	if lead >= total {
		return 0
	}

	trail := 0
	for i, part := range slices.Backward(parts) {
		if i < len(parts)-1 {
			trail++
		}
		trimmed := strings.TrimRightFunc(part, unicode.IsSpace)
		trail += len(part) - len(trimmed)
		if trimmed != "" {
			break
		}
	}

	return total - lead - trail
}
