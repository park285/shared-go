package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

type Server interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

func StartServerWithPrefix(server Server, errorText string, logger *slog.Logger, errCh chan<- error) {
	if logger == nil {
		logger = slog.Default()
	}

	go func() {
		err := server.ListenAndServe()
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}

		err = fmt.Errorf("%s: %w", errorText, err)

		logger.Error("http server listen error", "err", err)

		if errCh != nil {
			errCh <- fmt.Errorf("httpserver: listen: %w", err)
		}
	}()
}
