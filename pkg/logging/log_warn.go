package logging

import (
	"context"
	"log/slog"
)

// LogWarnWithErrorAttrs는 반환하지 않는 background failure를 WARN 로그와 표준 error attr로 남깁니다.
func LogWarnWithErrorAttrs(ctx context.Context, logger *slog.Logger, event, message string, err error, attrs ...slog.Attr) {
	if err == nil {
		return
	}

	errorAttrs := ErrorAttrs(err)
	mergedAttrs := make([]slog.Attr, 0, len(errorAttrs)+len(attrs))
	mergedAttrs = append(mergedAttrs, errorAttrs...)
	mergedAttrs = append(mergedAttrs, attrs...)

	Warn(ctx, logger, event, message, mergedAttrs...)
}
