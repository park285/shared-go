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

// StartServerWithPrefix 는 server.ListenAndServe 를 background goroutine 에서 실행합니다.
// 정상 종료인 http.ErrServerClosed 는 무시합니다. 그 밖의 listen 오류는 errorText 를 붙여
// logger(nil 이면 slog.Default())에 기록하고, errCh 가 있으면 "httpserver: listen: <errorText>: <원인>" 으로
// 한 번 보냅니다.
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
