package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingServer struct {
	listenErr     error
	shutdownErr   error
	closeErr      error
	listenDone    chan struct{}
	listenStopped chan struct{}
	stop          chan struct{}
	stopOnce      sync.Once
	//nolint:containedctx // Shutdown이 받은 ctx의 취소·데드라인을 나중에 검증하려고 보관한다.
	shutdownCtx    context.Context
	shutdownStops  bool
	closeCalled    atomic.Bool
	closeStarted   chan struct{}
	closeStartOnce sync.Once
	closeRelease   chan struct{}
}

func newBlockingServer(listenErr, shutdownErr error) *blockingServer {
	return &blockingServer{
		listenErr:     listenErr,
		shutdownErr:   shutdownErr,
		listenDone:    make(chan struct{}),
		listenStopped: make(chan struct{}),
		stop:          make(chan struct{}),
		shutdownStops: true,
	}
}

func (s *blockingServer) ListenAndServe() error {
	close(s.listenDone)
	<-s.stop
	close(s.listenStopped)

	return s.listenErr
}

func (s *blockingServer) Shutdown(ctx context.Context) error {
	s.shutdownCtx = ctx //nolint:fatcontext // 검증용으로 호출 시점 ctx를 그대로 담는다.
	if s.shutdownErr == nil && s.shutdownStops {
		s.stopOnce.Do(func() { close(s.stop) })
	}

	return s.shutdownErr
}

func (s *blockingServer) Close() error {
	s.closeCalled.Store(true)

	if s.closeStarted != nil {
		s.closeStartOnce.Do(func() { close(s.closeStarted) })
	}

	if s.closeRelease != nil {
		<-s.closeRelease
	}

	s.stopOnce.Do(func() { close(s.stop) })

	return s.closeErr
}

func TestRunStopsServerWhenContextEnds(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)
	ctx, cancel := context.WithCancel(t.Context())
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

	if server.closeCalled.Load() {
		t.Fatal("Close() called after graceful shutdown completed")
	}
}

func TestRunReturnsListenError(t *testing.T) {
	wantErr := errors.New("listen failed")
	server := newBlockingServer(wantErr, nil)
	server.stopOnce.Do(func() { close(server.stop) })

	err := Run(t.Context(), server, time.Second)
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
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Run(ctx, server, time.Second)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}

	if !strings.Contains(err.Error(), "http server shutdown failed") {
		t.Fatalf("Run() error = %q, want shutdown context", err)
	}

	if !server.closeCalled.Load() {
		t.Fatal("Close() was not called after shutdown failure")
	}
}

func TestRunReturnsWhenShutdownDoesNotStopListener(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)

	server.shutdownStops = false

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, server, 100*time.Millisecond)
	}()

	waitForBlockingListen(t, server)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() hung after shutdown timeout")
	}

	if !server.closeCalled.Load() {
		t.Fatal("Close() was not called after graceful stop timeout")
	}
}

func TestRunReturnsByHardDeadlineWhenForceCloseBlocks(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)

	server.shutdownStops = false
	server.closeStarted = make(chan struct{})
	server.closeRelease = make(chan struct{})

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, server, 100*time.Millisecond)
	}()

	waitForBlockingListen(t, server)
	cancel()

	select {
	case <-server.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("Close() was not called")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() exceeded hard shutdown deadline while Close() blocked")
	}

	close(server.closeRelease)

	select {
	case <-server.listenStopped:
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe() did not stop after releasing Close()")
	}
}

func TestRunJoinsForceCloseError(t *testing.T) {
	wantShutdownErr := errors.New("shutdown failed")
	wantCloseErr := errors.New("close failed")
	server := newBlockingServer(http.ErrServerClosed, wantShutdownErr)

	server.closeErr = wantCloseErr

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Run(ctx, server, time.Second)
	if !errors.Is(err, wantShutdownErr) {
		t.Fatalf("Run() error = %v, want shutdown error %v", err, wantShutdownErr)
	}

	if !errors.Is(err, wantCloseErr) {
		t.Fatalf("Run() error = %v, want force-close error %v", err, wantCloseErr)
	}
}

func TestRunUsesDefaultShutdownDeadlineWhenTimeoutIsNotPositive(t *testing.T) {
	server := newBlockingServer(http.ErrServerClosed, nil)
	ctx, cancel := context.WithCancel(t.Context())
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
