package openaipreset

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3/responses"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
	"github.com/park285/shared-go/v2/pkg/logging"
)

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
		return "", fmt.Errorf("completion params: %w", err)
	}

	attrs := promptSummaryAttrs(model, joinedPromptLen(prompts.Invariant, prompts.Developer, prompts.User))

	out, err := runRequest(ctx, c.logger, attrs, func() (string, error) {
		resp, respErr := c.openai.Responses.New(ctx, params)
		if respErr != nil {
			return "", fmt.Errorf("openai responses API: %w", openaidiag.SafeError(respErr))
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
	if err != nil {
		return out, fmt.Errorf("run request: %w", err)
	}

	return out, nil
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

func (c *Client) generate(
	ctx context.Context,
	taskName, systemPrompt, invariantPrompt, developerPrompt, userPrompt string,
	schema map[string]any,
) (sharedllm.JSONResponse, error) {
	attrs := promptSummaryAttrs(c.model, layeredPromptLen(systemPrompt, invariantPrompt, developerPrompt, userPrompt))

	out, err := runRequest(ctx, c.logger, attrs, func() (sharedllm.JSONResponse, error) {
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
	if err != nil {
		return out, fmt.Errorf("run request: %w", err)
	}

	return out, nil
}

func decodeJSONAs[T any](task, text string) (T, error) {
	var out T

	if err := jsonv2.Unmarshal([]byte(text), &out); err != nil {
		return out, fmt.Errorf("decode %s json failed: %w", strings.TrimSpace(task), &redactedCauseError{cause: err})
	}

	return out, nil
}

// JSON decode error 메시지는 실패 지점 주변 원문을 담을 수 있으므로 렌더링하면 provider
// 출력이 샌다(TestGenerateJSONAsDecodeErrorOmitsProviderOutput). 원문은 Unwrap으로만
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

		return zero, fmt.Errorf("run: %w", err)
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
