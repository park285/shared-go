package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// DefaultShutdownTimeout 은 shutdownTimeout 이 0 이하일 때 적용하는 보수적 기본 종료 예산입니다.
const DefaultShutdownTimeout = 30 * time.Second

const maxForceCloseReserve = time.Second

type forceCloser interface {
	Close() error
}

// Run 은 ctx 취소 시 server 를 종료합니다. shutdownTimeout 이 0 이하이면 무기한 대기 대신
// DefaultShutdownTimeout 을 적용해 process 종료가 멈추지 않도록 합니다.
func Run(ctx context.Context, server Server, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return normalizeListenError(err, "http server listen failed")
	case <-ctx.Done():
		return shutdownAndWait(ctx, server, errCh, shutdownTimeout)
	}
}

func shutdownAndWait(
	parent context.Context,
	server Server,
	errCh <-chan error,
	shutdownTimeout time.Duration,
) error {
	hardCtx, hardCancel := shutdownContext(parent, shutdownTimeout)
	defer hardCancel()
	gracefulCtx, gracefulCancel := gracefulShutdownContext(hardCtx)
	defer gracefulCancel()

	if err := server.Shutdown(gracefulCtx); err != nil {
		cause := fmt.Errorf("http server shutdown failed: %w", err)
		return forceCloseAndWait(hardCtx, server, errCh, cause)
	}

	select {
	case err := <-errCh:
		return normalizeListenError(err, "http server stopped with error")
	case <-gracefulCtx.Done():
		cause := fmt.Errorf("http server stop wait: %w", gracefulCtx.Err())
		return forceCloseAndWait(hardCtx, server, errCh, cause)
	}
}

func gracefulShutdownContext(hardCtx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := hardCtx.Deadline()
	if !ok {
		return context.WithCancel(hardCtx)
	}
	remaining := max(time.Until(deadline), 0)
	reserve := min(remaining/5, maxForceCloseReserve)
	return context.WithDeadline(hardCtx, deadline.Add(-reserve))
}

func forceCloseAndWait(
	ctx context.Context,
	server Server,
	errCh <-chan error,
	cause error,
) error {
	var closeCh <-chan error
	closeDone := true
	if closer, ok := server.(forceCloser); ok {
		resultCh := make(chan error, 1)
		closeCh = resultCh
		closeDone = false
		// Close 에는 context 경계가 없으므로, 이 호출이 소유하는 단일 goroutine과
		// 버퍼링한 결과 채널로 hard deadline을 기다리는 경로를 분리합니다. Close가
		// 반환하지 않아도 Run은 deadline에 반환하고, 이후 결과 전달도 막히지 않습니다.
		go func() {
			resultCh <- closer.Close()
		}()
	}

	var closeErr, stopErr error
	stopErr, stopDone := readServerStop(errCh)
	for !closeDone || !stopDone {
		select {
		case err := <-closeCh:
			closeDone = true
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				closeErr = fmt.Errorf("http server force close failed: %w", err)
			}
		case err := <-errCh:
			stopDone = true
			stopErr = normalizeListenError(err, "http server force close stopped with error")
		case <-ctx.Done():
			waitErr := fmt.Errorf("http server force close wait: %w", ctx.Err())
			return errors.Join(cause, closeErr, stopErr, waitErr)
		}
	}
	return errors.Join(cause, closeErr, stopErr)
}

func readServerStop(errCh <-chan error) (error, bool) {
	select {
	case err := <-errCh:
		return normalizeListenError(err, "http server force close stopped with error"), true
	default:
		return nil, false
	}
}

func shutdownContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	parent := context.WithoutCancel(ctx)
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func normalizeListenError(err error, text string) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s: %w", text, err)
}
