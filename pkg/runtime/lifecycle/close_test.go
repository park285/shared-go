package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type closeCtxKey struct{}

func TestRunCloseSteps_RunsInSliceOrder(t *testing.T) {
	t.Parallel()

	var order []string
	steps := []CloseStep{
		{Name: "a", Close: func(context.Context) error { order = append(order, "a"); return nil }},
		{Name: "b", Close: func(context.Context) error { order = append(order, "b"); return nil }},
		{Name: "c", Close: func(context.Context) error { order = append(order, "c"); return nil }},
	}

	if err := RunCloseSteps(context.Background(), nil, steps); err != nil {
		t.Fatalf("RunCloseSteps() error = %v, want nil", err)
	}

	if got := strings.Join(order, ","); got != "a,b,c" {
		t.Fatalf("close order = %q, want %q", got, "a,b,c")
	}
}

func TestRunCloseSteps_ContinuesAfterErrorAndAggregates(t *testing.T) {
	t.Parallel()

	errB := errors.New("b failed")
	errC := errors.New("c failed")

	var order []string
	steps := []CloseStep{
		{Name: "a", Close: func(context.Context) error { order = append(order, "a"); return nil }},
		{Name: "b", Close: func(context.Context) error { order = append(order, "b"); return errB }},
		{Name: "c", Close: func(context.Context) error { order = append(order, "c"); return errC }},
	}

	err := RunCloseSteps(context.Background(), nil, steps)
	if err == nil {
		t.Fatal("RunCloseSteps() error = nil, want aggregated error")
	}

	if got := strings.Join(order, ","); got != "a,b,c" {
		t.Fatalf("close order = %q, want every step to run", got)
	}

	if !errors.Is(err, errB) {
		t.Fatalf("aggregated error = %v, want to wrap errB", err)
	}

	if !errors.Is(err, errC) {
		t.Fatalf("aggregated error = %v, want to wrap errC", err)
	}

	if !strings.Contains(err.Error(), "close b:") || !strings.Contains(err.Error(), "close c:") {
		t.Fatalf("aggregated error = %q, want step names", err.Error())
	}
}

func TestRunCloseSteps_PassesContextToStep(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), closeCtxKey{}, "value")

	var got string
	steps := []CloseStep{
		{Name: "a", Close: func(c context.Context) error {
			if v, ok := c.Value(closeCtxKey{}).(string); ok {
				got = v
			}

			return nil
		}},
	}

	if err := RunCloseSteps(ctx, nil, steps); err != nil {
		t.Fatalf("RunCloseSteps() error = %v, want nil", err)
	}

	if got != "value" {
		t.Fatalf("step context value = %q, want %q", got, "value")
	}
}

func TestRunCloseSteps_NilLoggerSafeOnErrorPath(t *testing.T) {
	t.Parallel()

	steps := []CloseStep{
		{Name: "a", Close: func(context.Context) error { return errors.New("boom") }},
	}

	if err := RunCloseSteps(context.Background(), nil, steps); err == nil {
		t.Fatal("RunCloseSteps() error = nil, want error")
	}
}

func TestRunCloseSteps_EmptyStepsNoOp(t *testing.T) {
	t.Parallel()

	if err := RunCloseSteps(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunCloseSteps(nil) error = %v, want nil", err)
	}

	if err := RunCloseSteps(context.Background(), nil, []CloseStep{}); err != nil {
		t.Fatalf("RunCloseSteps(empty) error = %v, want nil", err)
	}
}

func TestRunCloseSteps_SkipsNilClose(t *testing.T) {
	t.Parallel()

	var ran bool
	steps := []CloseStep{
		{Name: "nil-step"},
		{Name: "b", Close: func(context.Context) error { ran = true; return nil }},
	}

	if err := RunCloseSteps(context.Background(), nil, steps); err != nil {
		t.Fatalf("RunCloseSteps() error = %v, want nil", err)
	}

	if !ran {
		t.Fatal("step with non-nil Close did not run")
	}
}

func TestRunCloseSteps_EmptyNameFallback(t *testing.T) {
	t.Parallel()

	steps := []CloseStep{
		{Close: func(context.Context) error { return errors.New("boom") }},
	}

	err := RunCloseSteps(context.Background(), nil, steps)
	if err == nil || !strings.Contains(err.Error(), "step[0]") {
		t.Fatalf("RunCloseSteps() error = %v, want step[0] fallback name", err)
	}
}

func TestRunCloseSteps_LogsFailuresThroughLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	steps := []CloseStep{
		{Name: "database", Close: func(context.Context) error { return errors.New("closed") }},
	}

	_ = RunCloseSteps(context.Background(), logger, steps)

	out := buf.String()
	if !strings.Contains(out, "close step failed") {
		t.Fatalf("log output = %q, want failure message", out)
	}

	if !strings.Contains(out, "database") {
		t.Fatalf("log output = %q, want step name", out)
	}
}

func TestRunCloseSteps_RunsAllStepsWhenContextAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var order []string
	steps := []CloseStep{
		{Name: "a", Close: func(context.Context) error { order = append(order, "a"); return nil }},
		{Name: "b", Close: func(context.Context) error { order = append(order, "b"); return nil }},
	}

	if err := RunCloseSteps(ctx, nil, steps); err != nil {
		t.Fatalf("RunCloseSteps() error = %v, want nil", err)
	}

	if got := strings.Join(order, ","); got != "a,b" {
		t.Fatalf("close order = %q, want %q (cancelled ctx must not skip steps)", got, "a,b")
	}
}
