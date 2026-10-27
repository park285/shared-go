package bootstrap

import (
	"context"
	"log/slog"
	"testing"
)

func TestNormalizeRuntimeBuildInputs_NilConfig(t *testing.T) {
	t.Parallel()

	_, _, err := NormalizeRuntimeBuildInputs(t.Context(), nil, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for nil config")
	}

	if err.Error() != "bootstrap: config must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "bootstrap: config must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_TypedNilConfig(t *testing.T) {
	t.Parallel()

	type Config struct{}

	var cfg *Config

	_, _, err := NormalizeRuntimeBuildInputs(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for typed nil config")
	}

	if err.Error() != "bootstrap: config must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "bootstrap: config must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_NilLoggerUsesDefault(t *testing.T) {
	t.Parallel()

	_, logger, err := NormalizeRuntimeBuildInputs(t.Context(), &struct{}{}, nil)
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}

	if logger != slog.Default() {
		t.Fatal("NormalizeRuntimeBuildInputs() logger is not slog.Default() for nil input")
	}
}

func TestNormalizeRuntimeBuildInputs_NilCtx(t *testing.T) {
	t.Parallel()

	var nilCtx context.Context

	ctx, _, err := NormalizeRuntimeBuildInputs(nilCtx, &struct{}{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}

	if ctx == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() returned nil context for nil input")
	}
}

func TestNormalizeRuntimeBuildInputs_ValidInputs(t *testing.T) {
	t.Parallel()

	inputCtx := t.Context()
	inputLogger := slog.New(slog.DiscardHandler)

	ctx, logger, err := NormalizeRuntimeBuildInputs(inputCtx, &struct{}{}, inputLogger)
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}

	if ctx != inputCtx {
		t.Fatal("NormalizeRuntimeBuildInputs() returned different context")
	}

	if logger != inputLogger {
		t.Fatal("NormalizeRuntimeBuildInputs() returned different logger")
	}
}
