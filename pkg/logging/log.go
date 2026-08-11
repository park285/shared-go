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
	// runtime.Callers → logWith → log → exported wrapper → 실제 호출자
	callerSkipViaWrapper = 4
	// runtime.Callers → logWith → 호출한 helper → 실제 호출자
	callerSkipViaHelper = 3
)

func log(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, attrs ...slog.Attr) {
	logWith(ctx, logger, level, event, message, callerSkipViaWrapper, attrs, nil)
}

// logWith는 level gate 뒤 Record를 직접 구성해 전달한다. Logger.LogAttrs의 두 번째 Enabled
// 호출과 임시 attr 병합 slice를 피하고, Record의 inline attr 저장소를 그대로 활용한다.
// primary와 secondary를 따로 받는 것도 같은 이유다. 호출자가 두 attr 묶음을 미리 합치면
// 그 병합 slice가 record마다 할당된다.
func logWith(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, skip int, primary, secondary []slog.Attr) {
	if logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !logger.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(skip, pcs[:])

	record := slog.NewRecord(time.Now(), level, logMessage(event, message), pcs[0])
	if strings.TrimSpace(event) != "" {
		record.AddAttrs(Event(event))
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
