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
