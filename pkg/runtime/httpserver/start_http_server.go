package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

func StartHTTPServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}
	StartServerWithPrefix(server, "HTTP server error", logger, errCh)
}

func ShutdownHTTPServer(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}
	return Shutdown(ctx, server, "HTTP server shutdown failed")
}

func StartServerWithPrefix(server Server, errorText string, logger *slog.Logger, errCh chan<- error) {
	Start(listenErrorPrefixServer{
		Server:    server,
		errorText: errorText,
		logger:    logger,
		errCh:     errCh,
	}, nil, errCh)
}

type listenErrorPrefixServer struct {
	Server
	errorText string
	logger    *slog.Logger
	errCh     chan<- error
}

func (s listenErrorPrefixServer) ListenAndServe() error {
	err := s.Server.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return err
	}

	if s.errCh == nil && s.logger != nil {
		s.logger.Error(s.errorText, slog.Any("error", err))
	}

	return fmt.Errorf("%s: %w", s.errorText, err)
}
