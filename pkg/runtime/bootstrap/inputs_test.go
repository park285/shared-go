package bootstrap

import (
	"context"
	"log/slog"
	"testing"
)

func TestNormalizeRuntimeBuildInputs_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRuntimeBuildInputs(t.Context(), nil, slog.New(slog.DiscardHandler))
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

	_, err := NormalizeRuntimeBuildInputs(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for typed nil config")
	}

	if err.Error() != "config must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "config must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_NilLogger(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRuntimeBuildInputs(t.Context(), &struct{}{}, nil)
	if err == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() expected error for nil logger")
	}

	if err.Error() != "logger must not be nil" {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %q, want %q", err.Error(), "logger must not be nil")
	}
}

func TestNormalizeRuntimeBuildInputs_NilCtx(t *testing.T) {
	t.Parallel()

	var nilCtx context.Context

	ctx, err := NormalizeRuntimeBuildInputs(nilCtx, &struct{}{}, slog.New(slog.DiscardHandler))
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

	ctx, err := NormalizeRuntimeBuildInputs(inputCtx, &struct{}{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}

	if ctx != inputCtx {
		t.Fatal("NormalizeRuntimeBuildInputs() returned different context")
	}
}
