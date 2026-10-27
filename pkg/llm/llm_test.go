package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingGenerator struct {
	called   bool
	deadline time.Time
	req      JSONRequest
	resp     JSONResponse
	err      error
}

func (g *recordingGenerator) GenerateJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	g.called = true
	g.req = req

	if deadline, ok := ctx.Deadline(); ok {
		g.deadline = deadline
	}

	return g.resp, g.err
}

type recordingUsageReporter struct {
	called   bool
	provider string
	model    string
	usage    Usage
}

func (r *recordingUsageReporter) RecordUsage(_ context.Context, provider, model string, usage Usage) {
	r.called = true
	r.provider = provider
	r.model = model
	r.usage = usage
}

func validJSONRequest() JSONRequest {
	return JSONRequest{
		TaskName:        "membernews_summary",
		DeveloperPrompt: roleDeveloper,
		UserPrompt:      roleUser,
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           testGptTest,
	}
}

func TestRunJSONRejectsNilContext(t *testing.T) {
	generator := &recordingGenerator{}

	//lint:ignore SA1012 거부 동작을 검증하기 위해 의도적으로 nil을 전달한다.
	_, err := RunJSON(nil, generator, validJSONRequest(), "openai", NoopUsageReporter{}) //nolint:staticcheck // 거부 동작 검증용 nil이다.
	if err == nil {
		t.Fatal("RunJSON(nil context) error = nil, want error")
	}

	if generator.called {
		t.Fatal("RunJSON called generator with nil context")
	}
}

func TestRunJSONValidatesRequiredRequestFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(JSONRequest) JSONRequest
		want string
	}{
		{
			name: "empty model",
			edit: func(req JSONRequest) JSONRequest {
				req.Model = " "
				return req
			},
			want: "model",
		},
		{
			name: "empty schema name",
			edit: func(req JSONRequest) JSONRequest {
				req.SchemaName = ""
				return req
			},
			want: "schema name",
		},
		{
			name: "nil schema",
			edit: func(req JSONRequest) JSONRequest {
				req.Schema = nil
				return req
			},
			want: "schema",
		},
		{
			name: "empty schema",
			edit: func(req JSONRequest) JSONRequest {
				req.Schema = map[string]any{}
				return req
			},
			want: "schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := &recordingGenerator{}

			_, err := RunJSON(t.Context(), generator, tt.edit(validJSONRequest()), "openai", nil)
			if err == nil {
				t.Fatal("RunJSON invalid request error = nil, want error")
			}

			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RunJSON invalid request error = %q, want substring %q", err, tt.want)
			}

			if generator.called {
				t.Fatal("RunJSON called generator for invalid request")
			}
		})
	}
}

func TestRunJSONPropagatesContextDeadlineAndRecordsUsage(t *testing.T) {
	deadline := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)

	defer cancel()

	generator := &recordingGenerator{
		resp: JSONResponse{
			Text:  `{"ok":true}`,
			Model: "gpt-returned",
			Usage: Usage{
				InputTokens:           12,
				OutputTokens:          5,
				TotalTokens:           17,
				CachedInputTokens:     3,
				ReasoningOutputTokens: 2,
			},
		},
	}
	reporter := &recordingUsageReporter{}

	resp, err := RunJSON(ctx, generator, validJSONRequest(), "openai", reporter)
	if err != nil {
		t.Fatalf("RunJSON error = %v, want nil", err)
	}

	if !generator.called {
		t.Fatal("RunJSON did not call generator")
	}

	if !generator.deadline.Equal(deadline) {
		t.Fatalf("generator deadline = %v, want %v", generator.deadline, deadline)
	}

	if !reporter.called {
		t.Fatal("RunJSON did not record usage")
	}

	if reporter.provider != "openai" || reporter.model != "gpt-returned" {
		t.Fatalf("usage reporter provider/model = %q/%q, want openai/gpt-returned", reporter.provider, reporter.model)
	}

	if reporter.usage != resp.Usage {
		t.Fatalf("usage reporter usage = %+v, want %+v", reporter.usage, resp.Usage)
	}
}

func TestRunJSONSkipsUsageOnGeneratorError(t *testing.T) {
	generatorErr := errors.New("provider failed")
	generator := &recordingGenerator{err: generatorErr}
	reporter := &recordingUsageReporter{}

	_, err := RunJSON(t.Context(), generator, validJSONRequest(), "openai", reporter)
	if !errors.Is(err, generatorErr) {
		t.Fatalf("RunJSON error = %v, want %v", err, generatorErr)
	}

	if reporter.called {
		t.Fatal("RunJSON recorded usage after generator error")
	}
}

func TestNoopUsageReporterDoesNothing(t *testing.T) {
	NoopUsageReporter{}.RecordUsage(t.Context(), "openai", testGptTest, Usage{TotalTokens: 1})
}
