package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

func Shutdown(ctx context.Context, server Server, errorText string) error {
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s: %w", errorText, err)
	}

	return nil
}
