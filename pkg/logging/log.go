package logging

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"time"
)

func Debug(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	log(ctx, logger, slog.LevelDebug, event, message, attrs...)
}

func Info(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	log(ctx, logger, slog.LevelInfo, event, message, attrs...)
}

func Warn(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	log(ctx, logger, slog.LevelWarn, event, message, attrs...)
}

func Error(ctx context.Context, logger *slog.Logger, event, message string, attrs ...slog.Attr) {
	log(ctx, logger, slog.LevelError, event, message, attrs...)
}

func Log(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, attrs ...slog.Attr) {
	log(ctx, logger, level, event, message, attrs...)
}

const (
	callerSkipViaWrapper = 4
	callerSkipViaHelper  = 3
)

func log(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, attrs ...slog.Attr) {
	logWith(ctx, logger, level, event, message, callerSkipViaWrapper, attrs, nil)
}

func logWith(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, skip int, primary, secondary []slog.Attr) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx = contextOrBackground(ctx)

	if !logger.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr

	runtime.Callers(skip, pcs[:])

	record := slog.NewRecord(time.Now(), level, logMessage(event, message), pcs[0])
	if strings.TrimSpace(event) != "" {
		record.AddAttrs(eventAttr(event))
	}

	contextValuesFrom(ctx).addToRecord(&record)
	record.AddAttrs(primary...)
	record.AddAttrs(secondary...)

	_ = logger.Handler().Handle(ctx, record) //nolint:errcheck // slog.Logger의 public logging API도 handler error를 반환하지 않는다
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
