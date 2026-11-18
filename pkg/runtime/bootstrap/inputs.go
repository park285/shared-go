package bootstrap

import (
	"cmp"
	"context"
	"errors"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
)

func NormalizeRuntimeBuildInputs(ctx context.Context, appConfig any, logger *slog.Logger) (context.Context, *slog.Logger, error) {
	if reflectutil.IsNil(appConfig) {
		return nil, nil, errors.New("bootstrap: config must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return ctx, cmp.Or(logger, slog.Default()), nil
}
