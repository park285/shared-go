package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

func Run(ctx context.Context, server Server, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return normalizeListenError(err, "http server listen failed")
	case <-ctx.Done():
		shutdownCtx, cancel := shutdownContext(ctx, shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http server shutdown failed: %w", err)
		}
		select {
		case err := <-errCh:
			return normalizeListenError(err, "http server stopped with error")
		case <-shutdownCtx.Done():
			return fmt.Errorf("http server stop wait: %w", shutdownCtx.Err())
		}
	}
}

func shutdownContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	parent := context.WithoutCancel(ctx)
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func normalizeListenError(err error, text string) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s: %w", text, err)
}
