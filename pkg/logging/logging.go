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

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/park285/shared-go/v2/pkg/logging/internal/archive"
)

type Config struct {
	Level      string
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
	return slog.New(newFormatHandler(slog.LevelInfo, os.Stdout, nil))
}

type Options struct {
	AsyncStdout bool
	PrivacyKeys []string
}

func EnableFileLoggingWithOptions(config Config, fileName string, opts Options) (*slog.Logger, io.Closer, error) {
	logger, closer, err := enableFileLoggingWithStdout(os.Stdout, config, fileName, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("logging: enable file logging with stdout: %w", err)
	}

	return logger, closer, nil
}

func enableFileLoggingWithStdout(stdout io.Writer, config Config, fileName string, opts Options) (*slog.Logger, io.Closer, error) {
	level := parseLevel(config.Level)
	if err := parseLogFormat(config.Format); err != nil {
		return nil, nil, fmt.Errorf("parse log format: %w", err)
	}

	privacy, err := newPrivacyKeys(opts.PrivacyKeys)
	if err != nil {
		return nil, nil, err
	}

	logDir := strings.TrimSpace(config.Dir)
	if logDir == "" {
		logger := slog.New(newFormatHandler(level, stdout, privacy))

		//nolint:nilnil // 콘솔 전용 구성에서 Closer가 nil인 것은 문서화된 반환 계약이다.
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
	logFile := newRotatingFile(config, logPath)

	w, closers := buildLogLanes(stdout, logFile, logArchiver, opts)

	logger := slog.New(newFormatHandler(level, w, privacy))
	logger.Info("file_logging_enabled",
		slog.String("path", logFile.Filename),
		slog.String("archive_dir", filepath.Join(logDir, archive.DirName)),
		slog.Bool("async_stdout", opts.AsyncStdout),
	)
	logArchiver.Trigger()

	return logger, closers, nil
}

func newRotatingFile(config Config, logPath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   config.Compress,
	}
}

func buildLogLanes(stdout io.Writer, logFile *lumberjack.Logger, logArchiver *archive.CompressedLogArchiver, opts Options) (io.Writer, multiCloser) {
	stdoutLane := stdout
	closers := make(multiCloser, 0, 4)

	if opts.AsyncStdout {
		asyncStdout := newAsyncDropWriter(stdout, asyncStdoutQueueDepth)

		stdoutLane = asyncStdout
		closers = append(closers, asyncStdout)
	}

	fileLane := &archive.AwareWriter{Inner: logFile, Archiver: logArchiver, Failures: &fileWriteFailed}
	stdoutGuard := &bestEffortWriter{target: stdoutLane, summary: fileLane}

	closers = append(closers, stdoutGuard, logFile)

	if logArchiver != nil {
		closers = append(closers, logArchiver)
	}

	return io.MultiWriter(stdoutGuard, fileLane), closers
}

type bestEffortWriter struct {
	target   io.Writer
	summary  io.Writer
	dropped  atomic.Uint64
	reported sync.Once
}

func (w *bestEffortWriter) Write(p []byte) (int, error) {
	if n, err := w.target.Write(p); err != nil || n != len(p) {
		w.dropped.Add(1)
		stdoutDropped.Add(1)
	}

	return len(p), nil
}

func (w *bestEffortWriter) Close() error {
	w.reported.Do(w.writeLossSummary)

	return nil
}

func (w *bestEffortWriter) writeLossSummary() {
	dropped := w.dropped.Load()

	var truncated uint64

	if async, ok := w.target.(*asyncDropWriter); ok {
		dropped += async.droppedCount()

		truncated = async.truncatedCount()
	}

	if (dropped == 0 && truncated == 0) || w.summary == nil {
		return
	}

	record := slog.NewRecord(time.Now(), slog.LevelWarn, "stdout lane write failed", 0)
	record.AddAttrs(slog.Uint64("dropped", dropped), slog.Uint64("truncated", truncated))

	if err := newFormatHandler(record.Level, w.summary, nil).Handle(context.Background(), record); err != nil {
		return
	}
}
