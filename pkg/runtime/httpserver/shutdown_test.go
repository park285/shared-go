package httpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestShutdown_Success(t *testing.T) {
	server := newFakeServer(nil, nil)

	if err := Shutdown(context.Background(), server, "shutdown failed"); err != nil {
		t.Fatalf("Shutdown() error = %v, want nil", err)
	}
}

func TestShutdown_Error(t *testing.T) {
	wantErr := errors.New("close failed")
	server := newFakeServer(nil, wantErr)

	err := Shutdown(context.Background(), server, "shutdown failed")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "shutdown failed: close failed") {
		t.Fatalf("Shutdown() error = %q, want prefixed context", err)
	}
}
