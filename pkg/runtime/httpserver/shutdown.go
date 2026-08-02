package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Shutdown 은 server.Shutdown(ctx) 결과를 errorText prefix 와 함께 wrap 합니다.
// 이미 종료된 server 가 돌려주는 http.ErrServerClosed 는 정상 종료로 흡수해 nil 을 반환합니다.
func Shutdown(ctx context.Context, server Server, errorText string) error {
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: %w", errorText, err)
	}
	return nil
}
