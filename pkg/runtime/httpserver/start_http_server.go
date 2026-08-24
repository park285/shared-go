package httpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

func StartServerWithPrefix(server Server, errorText string, logger *slog.Logger, errCh chan<- error) {
	Start(listenErrorPrefixServer{
		Server:    server,
		errorText: errorText,
	}, logger, errCh)
}

type listenErrorPrefixServer struct {
	Server

	errorText string
}

func (s listenErrorPrefixServer) ListenAndServe() error {
	err := s.Server.ListenAndServe()

	switch {
	case err == nil:
		return nil
	case errors.Is(err, http.ErrServerClosed):
		return http.ErrServerClosed
	default:
		return fmt.Errorf("%s: %w", s.errorText, err)
	}
}
