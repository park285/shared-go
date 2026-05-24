package httpserver

import (
	"context"
	"fmt"
)

// Shutdown 은 server.Shutdown(ctx) 결과를 errorText prefix 와 함께 wrap 합니다.
func Shutdown(ctx context.Context, server Server, errorText string) error {
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("%s: %w", errorText, err)
	}
	return nil
}
