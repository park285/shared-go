package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
)

const DefaultShutdownTimeout = lifecycle.DefaultShutdownTimeout

const maxForceCloseReserve = time.Second

type forceCloser interface {
	Close() error
}

func Run(ctx context.Context, server Server, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)

	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if listenErr := normalizeListenError(err, "http server listen failed"); listenErr != nil {
			return fmt.Errorf("httpserver: normalize listen error: %w", listenErr)
		}

		return nil
	case <-ctx.Done():
		if err := shutdownAndWait(ctx, server, errCh, shutdownTimeout); err != nil {
			return fmt.Errorf("httpserver: shutdown and wait: %w", err)
		}

		return nil
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
		if err := forceCloseAndWait(hardCtx, server, errCh, cause); err != nil {
			return fmt.Errorf("force close and wait: %w", err)
		}

		return nil
	}

	select {
	case err := <-errCh:
		if stopErr := normalizeListenError(err, "http server stopped with error"); stopErr != nil {
			return fmt.Errorf("normalize listen error: %w", stopErr)
		}

		return nil
	case <-gracefulCtx.Done():
		cause := fmt.Errorf("http server stop wait: %w", gracefulCtx.Err())
		if err := forceCloseAndWait(hardCtx, server, errCh, cause); err != nil {
			return fmt.Errorf("force close and wait: %w", err)
		}

		return nil
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

		go func() {
			resultCh <- closer.Close()
		}()
	}

	var closeErr, stopErr error

	stopDone, stopErr := readServerStop(errCh)

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
			if err := errors.Join(cause, closeErr, stopErr, waitErr); err != nil {
				return fmt.Errorf("force close and wait: %w", err)
			}

			return nil
		}
	}

	if err := errors.Join(cause, closeErr, stopErr); err != nil {
		return fmt.Errorf("force close and wait: %w", err)
	}

	return nil
}

func readServerStop(errCh <-chan error) (bool, error) {
	select {
	case err := <-errCh:
		if forceCloseErr := normalizeListenError(err, "http server force close stopped with error"); forceCloseErr != nil {
			return true, fmt.Errorf("normalize listen error: %w", forceCloseErr)
		}

		return true, nil
	default:
		return false, nil
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
