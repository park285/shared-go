package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartHTTPServer_NilServer(t *testing.T) {
	t.Parallel()
	errCh := make(chan error, 1)

	StartHTTPServer(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), errCh)

	select {
	case err := <-errCh:
		t.Fatalf("errCh received %v for nil server", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStartHTTPServer_ListenError(t *testing.T) {
	t.Parallel()
	errCh := make(chan error, 1)

	StartHTTPServer(&http.Server{Addr: "invalid::addr"}, nil, errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP server error") {
			t.Fatalf("error = %q, want HTTP server error prefix", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}

func TestStartHTTPServer_ListenError_NilErrCh_LogsFallback(t *testing.T) {
	t.Parallel()
	logs := &countingLogHandler{}

	StartHTTPServer(&http.Server{Addr: "invalid::addr"}, slog.New(logs), nil)

	time.Sleep(200 * time.Millisecond)
	if got := logs.count.Load(); got == 0 {
		t.Fatal("expected logger fallback when errCh is nil, got 0 log entries")
	}
}

func TestStartHTTPServer_ListenError_WithErrCh_NoLog(t *testing.T) {
	t.Parallel()
	errCh := make(chan error, 1)
	logs := &countingLogHandler{}

	StartHTTPServer(&http.Server{Addr: "invalid::addr"}, slog.New(logs), errCh)

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if got := logs.count.Load(); got != 0 {
		t.Fatalf("log count = %d, want 0 when errCh handles error", got)
	}
}

func TestShutdownHTTPServer_NilServer(t *testing.T) {
	t.Parallel()
	if err := ShutdownHTTPServer(context.Background(), nil); err != nil {
		t.Fatalf("ShutdownHTTPServer(nil) = %v, want nil", err)
	}
}

func TestShutdownHTTPServer_ErrorPrefix(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(handlerStarted)
			<-releaseHandler
		}),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() {
		close(releaseHandler)
		_ = server.Close()
		<-serveErr
	}()

	go func() {
		resp, reqErr := http.Get("http://" + listener.Addr().String())
		if reqErr == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	shutdownErr := ShutdownHTTPServer(ctx, server)
	if shutdownErr == nil {
		t.Fatal("expected shutdown error, got nil")
	}
	if !errors.Is(shutdownErr, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", shutdownErr)
	}
	if !strings.Contains(shutdownErr.Error(), "HTTP server shutdown failed") {
		t.Fatalf("error = %q, want prefix", shutdownErr)
	}
}

func TestStartServerWithPrefix_CustomErrorText(t *testing.T) {
	t.Parallel()
	wantText := "custom prefix"
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	StartServerWithPrefix(server, wantText, nil, errCh)

	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), wantText) {
			t.Fatalf("error = %q, want prefix %q", err, wantText)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want wrapped %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

type countingLogHandler struct {
	count atomic.Int64
}

func (h *countingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingLogHandler) Handle(context.Context, slog.Record) error {
	h.count.Add(1)
	return nil
}
func (h *countingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingLogHandler) WithGroup(string) slog.Handler      { return h }
