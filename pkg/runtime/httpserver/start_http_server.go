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
