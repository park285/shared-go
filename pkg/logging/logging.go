package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/park285/shared-go/v2/pkg/logging/internal/archive"
)

type Config struct {
	Level string
	// Format이 빈 값이면 json이다. json 외 값은 EnableFileLogging* 계열이 error로 거부한다.
	Format     string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

func parseLevel(level string) slog.Level {
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
	return slog.New(newFormatHandler(slog.LevelInfo, os.Stdout))
}

func NewTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Options는 파일 로깅의 선택 동작을 제어한다.
type Options struct {
	// AsyncStdout이 true면 stdout 사본 lane을 drop-on-full 비동기 writer로 감싸
	// stdout fd 정체가 로깅 전체를 블로킹하지 않게 한다. 파일 lane은 동기 기록을 유지한다.
	// 파일 lane이 없는 콘솔 전용 구성(Dir 빈 값)에서는 유일한 기록처를 잃을 수 있어 적용하지 않는다.
	AsyncStdout bool
	// OTel이 true면 span context의 trace_id/span_id를 로그 attr로 상관시킨다.
	OTel bool
}

func EnableFileLogging(config Config, fileName string) (*slog.Logger, error) {
	return EnableFileLoggingWithOTel(config, fileName, false)
}

func EnableFileLoggingWithLevel(config Config, fileName, level string) (*slog.Logger, error) {
	config.Level = level
	return EnableFileLogging(config, fileName)
}

func EnableFileLoggingWithOTel(config Config, fileName string, enableOTel bool) (*slog.Logger, error) {
	logger, _, err := EnableFileLoggingWithOptions(config, fileName, Options{OTel: enableOTel})
	return logger, err
}

// EnableFileLoggingWithOptions는 io.Closer를 함께 반환한다. Closer는 비동기 stdout lane의
// 잔여 드레인과 lumberjack 파일 핸들 정리를 담당하며, 콘솔 전용 구성에서는 nil이다.
//
// lumberjack은 첫 로테이션에서 millRun 고루틴을 띄우지만 Close()는 파일 핸들만 닫고 그
// 고루틴을 회수하지 않는다(라이브러리 한계). 프로세스당 1회만 호출하고 재초기화는 피하라.
func EnableFileLoggingWithOptions(config Config, fileName string, opts Options) (*slog.Logger, io.Closer, error) {
	return enableFileLoggingWithStdout(os.Stdout, config, fileName, opts)
}

func enableFileLoggingWithStdout(stdout io.Writer, config Config, fileName string, opts Options) (*slog.Logger, io.Closer, error) {
	level := parseLevel(config.Level)
	if err := parseLogFormat(config.Format); err != nil {
		return nil, nil, err
	}
	logDir := strings.TrimSpace(config.Dir)
	if logDir == "" {
		logger := slog.New(newConsoleHandler(level, stdout, opts.OTel))
		return logger, nil, nil
	}
	if config.MaxSizeMB <= 0 || config.MaxBackups <= 0 || config.MaxAgeDays <= 0 {
		return nil, nil, fmt.Errorf("invalid log config: size=%d backups=%d age_days=%d", config.MaxSizeMB, config.MaxBackups, config.MaxAgeDays)
	}

	if err := archive.EnsureLogDirPerm(logDir); err != nil {
		return nil, nil, fmt.Errorf("prepare log dir failed: %w", err)
	}

	logPath := filepath.Join(logDir, fileName)
	if err := archive.EnsureLogFilePerm(logPath); err != nil {
		return nil, nil, fmt.Errorf("prepare log file failed: %w", err)
	}

	logArchiver := archive.NewCompressedLogArchiver(logPath, config.MaxBackups, config.MaxAgeDays, config.Compress)
	logFile := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   config.Compress,
	}

	stdoutLane := stdout
	closers := make(multiCloser, 0, 4)
	if opts.AsyncStdout {
		asyncStdout := newAsyncDropWriter(stdout, asyncStdoutQueueDepth)
		stdoutLane = asyncStdout
		closers = append(closers, asyncStdout)
	}

	fileLane := &archive.AwareWriter{Inner: logFile, Archiver: logArchiver}
	// 요약은 stdout lane 종료 뒤 파일에 남아야 하므로 logFile Close보다 앞서 실행된다.
	stdoutGuard := &bestEffortWriter{target: stdoutLane, summary: fileLane}
	closers = append(closers, stdoutGuard, logFile)
	if logArchiver != nil {
		closers = append(closers, logArchiver)
	}

	w := io.MultiWriter(stdoutGuard, fileLane)

	handler := newFormatHandler(level, w)
	if opts.OTel {
		handler = newOTelHandler(handler)
	}

	logger := slog.New(handler)
	logger.Info("file_logging_enabled",
		slog.String("path", logFile.Filename),
		slog.String("archive_dir", filepath.Join(logDir, archive.DirName)),
		slog.Bool("otel_correlation", opts.OTel),
		slog.Bool("async_stdout", opts.AsyncStdout),
	)
	logArchiver.Trigger()
	return logger, closers, nil
}

// io.MultiWriter는 첫 writer가 실패하면 나머지를 건너뛴다. stdout이 EPIPE·ENOSPC로 죽었을 때
// 그 뒤의 파일 lane까지 함께 멈추면 내구 기록이 사라지므로, stdout 사본의 실패는 삼키고
// 유실 건수만 세어 Close에서 파일 lane으로 요약한다. 파일 lane은 감싸지 않아 실패가 전파된다.
type bestEffortWriter struct {
	target   io.Writer
	summary  io.Writer
	dropped  atomic.Uint64
	reported sync.Once
}

func (w *bestEffortWriter) Write(p []byte) (int, error) {
	if _, err := w.target.Write(p); err != nil {
		w.dropped.Add(1)
	}

	return len(p), nil
}

func (w *bestEffortWriter) Close() error {
	w.reported.Do(w.writeLossSummary)

	return nil
}

// 요약을 stdout으로 되돌리면 이미 실패한 lane에 다시 쓰는 셈이라 파일 lane에만 남긴다.
func (w *bestEffortWriter) writeLossSummary() {
	dropped := w.dropped.Load()
	if dropped == 0 || w.summary == nil {
		return
	}

	record := slog.NewRecord(time.Now(), slog.LevelWarn, "stdout lane write failed", 0)
	record.AddAttrs(slog.Uint64("dropped", dropped))
	if err := newFormatHandler(record.Level, w.summary).Handle(context.Background(), record); err != nil {
		return
	}
}

func newConsoleHandler(level slog.Level, w io.Writer, enableOTel bool) slog.Handler {
	handler := newFormatHandler(level, w)
	if enableOTel {
		handler = newOTelHandler(handler)
	}
	return handler
}

type otelHandler struct {
	inner slog.Handler
}

func newOTelHandler(inner slog.Handler) *otelHandler {
	return &otelHandler{inner: inner}
}

func (h *otelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *otelHandler) Handle(ctx context.Context, record slog.Record) error {
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

func (h *otelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &otelHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *otelHandler) WithGroup(name string) slog.Handler {
	return &otelHandler{inner: h.inner.WithGroup(name)}
}
