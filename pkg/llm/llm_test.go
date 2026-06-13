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
		TaskName:     "membernews_summary",
		SystemPrompt: "system",
		UserPrompt:   "user",
		SchemaName:   "summary",
		Schema:       map[string]any{"type": "object"},
		Model:        "gpt-test",
	}
}

func TestRunJSONRejectsNilContext(t *testing.T) {
	generator := &recordingGenerator{}

	_, err := RunJSON(nil, generator, validJSONRequest(), "openai", NoopUsageReporter{})
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

			_, err := RunJSON(context.Background(), generator, tt.edit(validJSONRequest()), "openai", nil)
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
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
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

	_, err := RunJSON(context.Background(), generator, validJSONRequest(), "openai", reporter)
	if !errors.Is(err, generatorErr) {
		t.Fatalf("RunJSON error = %v, want %v", err, generatorErr)
	}
	if reporter.called {
		t.Fatal("RunJSON recorded usage after generator error")
	}
}

func TestRedactDiagnosticMasksSecretsAndBoundsOutput(t *testing.T) {
	raw := "OPENAI_API_KEY=sk-live-openai-secret" +
		" CODEX_ACCESS_TOKEN=codex-live-secret" +
		" CODEX_API_KEY=codex-api-secret" +
		" Authorization: Bearer bearer-live-secret " +
		strings.Repeat("x", 180)

	got := RedactDiagnostic(raw, 180)
	for _, secret := range []string{
		"sk-live-openai-secret",
		"codex-live-secret",
		"codex-api-secret",
		"bearer-live-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("RedactDiagnostic leaked %q in %q", secret, got)
		}
	}
	if strings.Count(got, "***REDACTED***") < 4 {
		t.Fatalf("RedactDiagnostic redaction count = %d, want at least 4 in %q", strings.Count(got, "***REDACTED***"), got)
	}
	if len(got) > 180+len(truncatedMarker) {
		t.Fatalf("RedactDiagnostic length = %d, want bounded by %d", len(got), 180+len(truncatedMarker))
	}
	if !strings.HasSuffix(got, truncatedMarker) {
		t.Fatalf("RedactDiagnostic suffix = %q, want %q", got, truncatedMarker)
	}
}

func TestNoopUsageReporterDoesNothing(t *testing.T) {
	NoopUsageReporter{}.RecordUsage(context.Background(), "openai", "gpt-test", Usage{TotalTokens: 1})
}
