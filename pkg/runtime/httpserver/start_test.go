package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeServer struct {
	listenErr   error
	shutdownErr error
	listenDone  chan struct{}
}

func newFakeServer(listenErr, shutdownErr error) *fakeServer {
	return &fakeServer{
		listenErr:   listenErr,
		shutdownErr: shutdownErr,
		listenDone:  make(chan struct{}),
	}
}

func (s *fakeServer) ListenAndServe() error {
	close(s.listenDone)
	return s.listenErr
}

func (s *fakeServer) Shutdown(context.Context) error {
	return s.shutdownErr
}

func TestStart_NormalStartup(t *testing.T) {
	server := newFakeServer(http.ErrServerClosed, nil)
	errCh := make(chan error, 1)

	Start(server, slog.New(slog.NewTextHandler(io.Discard, nil)), errCh)
	waitForListen(t, server)

	select {
	case err := <-errCh:
		t.Fatalf("errCh received %v, want empty", err)
	default:
	}
}

func TestStart_ListenError(t *testing.T) {
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	Start(server, slog.New(slog.NewTextHandler(io.Discard, nil)), errCh)

	select {
	case err := <-errCh:
		if !errors.Is(err, wantErr) {
			t.Fatalf("errCh error = %v, want wrapped %v", err, wantErr)
		}
		if !strings.Contains(err.Error(), "http server listen") {
			t.Fatalf("errCh error = %q, want listen context", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Start() did not send listen error")
	}
}

func TestStart_NilLogger(t *testing.T) {
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	Start(server, nil, errCh)

	select {
	case err := <-errCh:
		if !errors.Is(err, wantErr) {
			t.Fatalf("errCh error = %v, want wrapped %v", err, wantErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Start() did not send listen error")
	}
}

func TestStart_NilErrCh(t *testing.T) {
	server := newFakeServer(errors.New("listen failed"), nil)

	Start(server, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	waitForListen(t, server)
}

func waitForListen(t *testing.T, server *fakeServer) {
	t.Helper()

	select {
	case <-server.listenDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ListenAndServe() was not called")
	}
}
