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

// log는 level gate 뒤 Record를 직접 구성해 전달한다. Logger.LogAttrs의 두 번째 Enabled
// 호출과 임시 attr 병합 slice를 피하고, Record의 inline attr 저장소를 그대로 활용한다.
//
// runtime.Callers의 skip 3은 이 함수 바로 위의 exported logging wrapper 호출자를 가리킨다.
// 따라서 일반적인 Debug/Info/Warn/Error/Log 사용에서는 실제 애플리케이션 호출 위치가 남는다.
func log(ctx context.Context, logger *slog.Logger, level slog.Level, event, message string, attrs ...slog.Attr) {
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
	runtime.Callers(3, pcs[:])

	record := slog.NewRecord(time.Now(), level, logMessage(event, message), pcs[0])
	if strings.TrimSpace(event) != "" {
		record.AddAttrs(Event(event))
	}
	contextValuesFrom(ctx).addToRecord(&record)
	record.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, record)
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
