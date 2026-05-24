package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Server 는 HTTP server lifecycle 추상화입니다.
type Server interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// Start 는 server.ListenAndServe 를 background goroutine 에서 실행합니다.
func Start(server Server, logger *slog.Logger, errCh chan<- error) {
	go func() {
		handleListenError(server.ListenAndServe(), logger, errCh)
	}()
}

func handleListenError(err error, logger *slog.Logger, errCh chan<- error) {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}
	if logger != nil {
		logger.Error("http server listen error", "err", err)
	}
	if errCh != nil {
		errCh <- fmt.Errorf("http server listen: %w", err)
	}
}
