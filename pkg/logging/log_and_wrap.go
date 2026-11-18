package logging

import (
	"context"
	"fmt"
	"log/slog"
)

func LogAndWrapError(ctx context.Context, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error {
	if err == nil {
		return nil
	}

	logWith(ctx, logger, slog.LevelError, op+".failed", op+": "+err.Error(), callerSkipViaHelper, ErrorAttrs(err), attrs)

	return fmt.Errorf("%s: %w", op, err)
}
