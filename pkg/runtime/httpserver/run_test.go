package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingServer struct {
	listenErr   error
	shutdownErr error
	listenDone  chan struct{}
	stop        chan struct{}
	stopOnce    sync.Once
	shutdownCtx context.Context
}

func newBlockingServer(listenErr, shutdownErr error) *blockingServer {
	return &blockingServer{
		listenErr:   listenErr,
		shutdownErr: shutdownErr,
		listenDone:  make(chan struct{}),
		stop:        make(chan struct{}),
	}
}

func (s *blockingServer) ListenAndServe() error {
	close(s.listenDone)
	<-s.stop
	return s.listenErr
}

func (s *blockingServer) Shutdown(ctx context.Context) error {
	s.shutdownCtx = ctx
	if s.shutdownErr == nil {
		s.stopOnce.Do(func() { close(s.stop) })
	}
	return s.shutdownErr
}

func TestRunStopsServerWhenContextEnds(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, server, time.Second)
	}()

	waitForBlockingListen(t, server)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
	if server.shutdownCtx == nil {
		t.Fatal("Shutdown() was not called")
	}
	if server.shutdownCtx.Err() != context.Canceled {
		t.Fatalf("Shutdown context error = %v, want canceled after Run returns", server.shutdownCtx.Err())
	}
}

func TestRunReturnsListenError(t *testing.T) {
	wantErr := errors.New("listen failed")
	server := newBlockingServer(wantErr, nil)
	server.stopOnce.Do(func() { close(server.stop) })

	err := Run(context.Background(), server, time.Second)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "http server listen failed") {
		t.Fatalf("Run() error = %q, want listen context", err)
	}
	if server.shutdownCtx != nil {
		t.Fatal("Shutdown() called after server stopped itself")
	}
}

func TestRunReturnsShutdownError(t *testing.T) {
	wantErr := errors.New("shutdown failed")
	server := newBlockingServer(http.ErrServerClosed, wantErr)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(ctx, server, time.Second)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "http server shutdown failed") {
		t.Fatalf("Run() error = %q, want shutdown context", err)
	}
}

func TestRunReturnsWhenShutdownDoesNotStopListener(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, server, 20*time.Millisecond)
	}()

	waitForBlockingListen(t, server)
	server.stopOnce.Do(func() {})
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() hung after shutdown timeout")
	}
}

func TestRunUsesDefaultShutdownDeadlineWhenTimeoutIsNotPositive(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startedAt := time.Now()
	if err := Run(ctx, server, 0); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	deadline, ok := server.shutdownCtx.Deadline()
	if !ok {
		t.Fatal("Shutdown context has no deadline for non-positive timeout")
	}
	if budget := deadline.Sub(startedAt); budget <= 0 || budget > DefaultShutdownTimeout+time.Second {
		t.Fatalf("Shutdown budget = %v, want bounded by DefaultShutdownTimeout", budget)
	}
}

func waitForBlockingListen(t *testing.T, server *blockingServer) {
	t.Helper()
	select {
	case <-server.listenDone:
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe() was not called")
	}
}
