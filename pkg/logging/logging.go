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

	"github.com/park285/shared-go/pkg/logging/archive"
)

type Config struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
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

func NewLogger() *slog.Logger {
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.RFC3339,
		AddSource:  true,
		NoColor:    shouldDisableColor(os.Stdout),
	}))
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

	if err := archive.EnsureLogDirPerm(logDir); err != nil {
		return nil, fmt.Errorf("prepare log dir failed: %w", err)
	}

	logPath := filepath.Join(logDir, fileName)
	if err := archive.EnsureLogFilePerm(logPath); err != nil {
		return nil, fmt.Errorf("prepare log file failed: %w", err)
	}

	logArchiver := archive.NewCompressedLogArchiver(logPath, config.MaxBackups, config.MaxAgeDays, config.Compress)
	logFile := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   config.Compress,
	}

	w := io.MultiWriter(os.Stdout, &archive.AwareWriter{Inner: logFile, Archiver: logArchiver})

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
		slog.String("archive_dir", filepath.Join(logDir, archive.DirName)),
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
