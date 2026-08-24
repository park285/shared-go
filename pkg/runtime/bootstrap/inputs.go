package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
)

func NormalizeRuntimeBuildInputs(ctx context.Context, appConfig any, logger *slog.Logger) (context.Context, error) {
	if reflectutil.IsNil(appConfig) {
		return nil, errors.New("config must not be nil")
	}

	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return ctx, nil
}
