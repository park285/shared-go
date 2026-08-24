package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNilContext         = errors.New("llm: context is nil")
	ErrNilJSONGenerator   = errors.New("llm: json generator is nil")
	ErrInvalidJSONRequest = errors.New("llm: invalid json request")
)

type JSONGenerator interface {
	GenerateJSON(ctx context.Context, req JSONRequest) (JSONResponse, error)
}

type Message struct {
	Role    string
	Content string
	// CacheBreakpoint는 이 메시지 끝을 GPT-5.6+ explicit prompt cache prefix의
	// 경계로 표시한다. 이전 모델은 해당 필드를 400으로 거부하므로 호출자가
	// 모델에 맞춰 설정해야 한다.
	CacheBreakpoint bool
}

type JSONRequest struct {
	TaskName        string
	SystemPrompt    string
	UserPrompt      string
	InvariantPrompt string
	DeveloperPrompt string
	SchemaName      string
	Schema          map[string]any
	Model           string
	Temperature     *float64
	ReasoningEffort string
	WebSearch       bool
	ChatCompletions bool
	CacheKey        string
}

type JSONResponse struct {
	Text         string
	Model        string
	Usage        Usage
	FallbackUsed bool
}

type Usage struct {
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CachedInputTokens     int
	CacheWriteTokens      int
	ReasoningOutputTokens int
}

type UsageReporter interface {
	RecordUsage(ctx context.Context, provider, model string, usage Usage)
}

type NoopUsageReporter struct{}

func (NoopUsageReporter) RecordUsage(context.Context, string, string, Usage) {}

func RunJSON(ctx context.Context, generator JSONGenerator, req JSONRequest, provider string, reporter UsageReporter) (JSONResponse, error) {
	if ctx == nil {
		return JSONResponse{}, ErrNilContext
	}

	if generator == nil {
		return JSONResponse{}, ErrNilJSONGenerator
	}

	if err := ValidateJSONRequest(req); err != nil {
		return JSONResponse{}, fmt.Errorf("validate JSON request: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return JSONResponse{}, err
	}

	resp, err := generator.GenerateJSON(ctx, req)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("generate JSON: %w", err)
	}

	if reporter != nil {
		model := strings.TrimSpace(resp.Model)
		if model == "" {
			model = strings.TrimSpace(req.Model)
		}

		reporter.RecordUsage(ctx, provider, model, resp.Usage)
	}

	return resp, nil
}

func ValidateJSONRequest(req JSONRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("%w: model is empty", ErrInvalidJSONRequest)
	}

	if strings.TrimSpace(req.SchemaName) == "" {
		return fmt.Errorf("%w: schema name is empty", ErrInvalidJSONRequest)
	}

	if len(req.Schema) == 0 {
		return fmt.Errorf("%w: schema is empty", ErrInvalidJSONRequest)
	}

	if hasPromptLayer(req.SystemPrompt) &&
		(hasPromptLayer(req.InvariantPrompt) || hasPromptLayer(req.DeveloperPrompt)) {
		return fmt.Errorf("%w: system prompt cannot be combined with invariant or developer prompt layers", ErrInvalidJSONRequest)
	}

	return nil
}

func hasPromptLayer(prompt string) bool {
	return strings.TrimSpace(prompt) != ""
}
