package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	logFilePerm         os.FileMode = 0o640
	logDirPerm          os.FileMode = 0o750
	compressSuffix                  = ".gz"
	backupTimeFormat                = "2006-01-02T15-04-05.000"
	archiveDirName                  = "archive"
	archiveScanInterval             = 5 * time.Second
)

type Config struct {
	Level      string // 로그 레벨 (debug, info, warn, error)
	Dir        string // 로그 파일 디렉터리
	MaxSizeMB  int    // 단일 파일 최대 크기 (MB)
	MaxBackups int    // 보관할 백업 파일 수
	MaxAgeDays int    // 백업 파일 보관 일수
	Compress   bool   // 백업 파일 압축 여부
}

func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func LogError(logger *slog.Logger, event string, err error) {
	if logger == nil || err == nil {
		return
	}
	logger.Warn(event, "err", err)
}

func LogInfo(logger *slog.Logger, event string, fields ...any) {
	if logger == nil {
		return
	}
	logger.Info(event, fields...)
}

func NewLogger() *slog.Logger {
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    shouldDisableColor(os.Stdout),
	}))
}

func NewLoggerWithLevel(level string) *slog.Logger {
	config := Config{Level: level}
	logger, err := EnableFileLogging(config, "")
	if err != nil || logger == nil {
		return NewLogger()
	}
	return logger
}

func NewTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func NewTestLoggerWithOutput(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

func EnableFileLogging(config Config, fileName string) (*slog.Logger, error) {
	return EnableFileLoggingWithOTel(config, fileName, false)
}

func EnableFileLoggingWithLevel(config Config, fileName, level string) (*slog.Logger, error) {
	config.Level = level
	return EnableFileLogging(config, fileName)
}

func EnableFileLoggingWithOTel(config Config, fileName string, enableOTel bool) (*slog.Logger, error) {
	level := ParseLevel(config.Level)
	logDir := strings.TrimSpace(config.Dir)
	if logDir == "" {
		logger := slog.New(newConsoleHandler(level, os.Stdout, enableOTel))
		slog.SetDefault(logger)
		return logger, nil
	}
	if config.MaxSizeMB <= 0 || config.MaxBackups <= 0 || config.MaxAgeDays <= 0 {
		return nil, fmt.Errorf("invalid log config: size=%d backups=%d age_days=%d", config.MaxSizeMB, config.MaxBackups, config.MaxAgeDays)
	}

	if err := ensureLogDirPerm(logDir); err != nil {
		return nil, fmt.Errorf("prepare log dir failed: %w", err)
	}

	logPath := filepath.Join(logDir, fileName)
	if err := ensureLogFilePerm(logPath); err != nil {
		return nil, fmt.Errorf("prepare log file failed: %w", err)
	}

	logArchiver := newCompressedLogArchiver(logPath, config.MaxBackups, config.MaxAgeDays, config.Compress)
	logFile := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   config.Compress,
	}

	w := io.MultiWriter(os.Stdout, &archiveAwareWriter{inner: logFile, archiver: logArchiver})

	var handler slog.Handler
	handler = tint.NewHandler(w, &tint.Options{
		Level:      level,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    true,
	})
	handler = NewSanitizeHandler(handler)
	if enableOTel {
		handler = NewOTelHandler(handler)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	logger.Info("file_logging_enabled",
		slog.String("path", logFile.Filename),
		slog.String("archive_dir", filepath.Join(logDir, archiveDirName)),
		slog.Bool("otel_correlation", enableOTel),
	)
	logArchiver.Trigger()
	return logger, nil
}

func newConsoleHandler(level slog.Level, w io.Writer, enableOTel bool) slog.Handler {
	var handler slog.Handler
	handler = tint.NewHandler(w, &tint.Options{
		Level:      level,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    shouldDisableColor(w),
	})
	handler = NewSanitizeHandler(handler)
	if enableOTel {
		handler = NewOTelHandler(handler)
	}
	return handler
}

func shouldDisableColor(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return true
	}

	if os.Getenv("NO_COLOR") != "" {
		return true
	}

	fd := file.Fd()
	return !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd)
}

type OTelHandler struct {
	inner slog.Handler
}

func NewOTelHandler(inner slog.Handler) *OTelHandler {
	return &OTelHandler{inner: inner}
}

func (h *OTelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *OTelHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		spanCtx := span.SpanContext()
		record.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, record)
}

func (h *OTelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &OTelHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *OTelHandler) WithGroup(name string) slog.Handler {
	return &OTelHandler{inner: h.inner.WithGroup(name)}
}
