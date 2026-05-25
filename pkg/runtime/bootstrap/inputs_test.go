package bootstrap

import (
	"context"
	"log/slog"
	"testing"
)

func TestNormalizeRuntimeBuildInputs_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRuntimeBuildInputs(context.Background(), nil, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for nil config")
	}
	if err.Error() != "config must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "config must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_TypedNilConfig(t *testing.T) {
	t.Parallel()

	type Config struct{}
	var cfg *Config

	_, err := NormalizeRuntimeBuildInputs(context.Background(), cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for typed nil config")
	}
	if err.Error() != "config must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "config must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_NilLogger(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRuntimeBuildInputs(context.Background(), &struct{}{}, nil)
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for nil logger")
	}
	if err.Error() != "logger must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "logger must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_NilCtx(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 nil ctx는 의도적 — fallback 동작 검증
	ctx, err := NormalizeRuntimeBuildInputs(nil, &struct{}{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}
	if ctx == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() returned nil context for nil input")
	}
}

func TestNormalizeRuntimeBuildInputs_ValidInputs(t *testing.T) {
	t.Parallel()

	inputCtx := context.Background()
	ctx, err := NormalizeRuntimeBuildInputs(inputCtx, &struct{}{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}
	if ctx != inputCtx {
		t.Fatal("NormalizeRuntimeBuildInputs() returned different context")
	}
}

func TestIsNilValue(t *testing.T) {
	t.Parallel()

	if !isNilValue(nil) {
		t.Fatal("isNilValue(nil) = false")
	}

	type S struct{}
	var p *S
	if !isNilValue(p) {
		t.Fatal("isNilValue(typed nil pointer) = false")
	}

	if isNilValue(&S{}) {
		t.Fatal("isNilValue(non-nil pointer) = true")
	}

	if isNilValue(42) {
		t.Fatal("isNilValue(int) = true")
	}

	if isNilValue("hello") {
		t.Fatal("isNilValue(string) = true")
	}
}
