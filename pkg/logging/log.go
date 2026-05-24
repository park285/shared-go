package logging

import (
	"context"
	"log/slog"
	"strings"
)

func Debug(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	Log(ctx, logger, slog.LevelDebug, event, message, attrs...)
}

func Info(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	Log(ctx, logger, slog.LevelInfo, event, message, attrs...)
}

func Warn(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	Log(ctx, logger, slog.LevelWarn, event, message, attrs...)
}

func Error(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	Log(ctx, logger, slog.LevelError, event, message, attrs...)
}

func Log(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, attrs ...slog.Attr) {
	if logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !logger.Enabled(ctx, level) {
		return
	}

	merged := make([]slog.Attr, 0, 1+len(ContextAttrs(ctx))+len(attrs))
	if strings.TrimSpace(event) != "" {
		merged = append(merged, Event(event))
	}
	merged = append(merged, ContextAttrs(ctx)...)
	merged = append(merged, attrs...)

	logger.LogAttrs(ctx, level, logMessage(event, message), merged...)
}

func logMessage(event, message string) string {
	message = strings.TrimSpace(message)
	if message != "" {
		return message
	}
	event = strings.TrimSpace(event)
	if event != "" {
		return event
	}
	return "log"
}
