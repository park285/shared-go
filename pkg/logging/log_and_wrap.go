package logging

import (
	"context"
	"fmt"
	"log/slog"
)

// LogAndWrapError는 에러를 구조화 로그로 남긴 뒤 op prefix로 감싸 반환합니다.
func LogAndWrapError(ctx context.Context, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error {
	if err == nil {
		return nil
	}

	errorAttrs := ErrorAttrs(err)
	mergedAttrs := make([]slog.Attr, 0, len(errorAttrs)+len(attrs))
	mergedAttrs = append(mergedAttrs, errorAttrs...)
	mergedAttrs = append(mergedAttrs, attrs...)

	Error(ctx, logger, op+".failed", op+": "+err.Error(), mergedAttrs...)
	return fmt.Errorf("%s: %w", op, err)
}
