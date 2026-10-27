package bootstrap

import (
	"cmp"
	"context"
	"errors"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
)

// NormalizeRuntimeBuildInputs는 BuildRuntime 훅이 받은 입력을 정규화합니다. 설정 값 appConfig가
// nil(typed nil 포함)이면 오류를 반환하고, nil ctx는 context.Background(), nil logger는
// slog.Default()로 바꿔 돌려줍니다.
func NormalizeRuntimeBuildInputs(ctx context.Context, appConfig any, logger *slog.Logger) (context.Context, *slog.Logger, error) {
	if reflectutil.IsNil(appConfig) {
		return nil, nil, errors.New("bootstrap: config must not be nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return ctx, cmp.Or(logger, slog.Default()), nil
}
