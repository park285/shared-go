package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestNewTestLogger(t *testing.T) {
	logger := NewTestLogger()
	if logger == nil {
		t.Fatal("NewTestLogger returned nil")
	}

	logger.Info("test message", "key", "value")
	logger.Error("error message", "error", "test error")
}

func TestNewTestLoggerWithOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTestLoggerWithOutput(&buf)
	if logger == nil {
		t.Fatal("NewTestLoggerWithOutput returned nil")
	}

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected log output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected log output to contain 'key=value', got: %s", output)
	}
}

func TestNewTestLoggerDiscardsOutput(t *testing.T) {
	logger := NewTestLogger()

	logger.Info("this should be discarded")
	logger.Error("this should also be discarded")
}

func TestOTelHandler_WithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	handler := NewOTelHandler(baseHandler)

	logger := slog.New(handler)
	logger.Info("test message")

	output := buf.String()
	// trace_id/span_id가 없어야 함 (span 없는 context)
	if strings.Contains(output, "trace_id") {
		t.Errorf("expected no trace_id without span, got: %s", output)
	}
}

func TestOTelHandler_Enabled(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewOTelHandler(baseHandler)

	// Info 레벨은 비활성화되어야 함
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info level to be disabled")
	}

	// Warn 레벨은 활성화되어야 함
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected Warn level to be enabled")
	}
}

func TestOTelHandler_WithAttrs(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, nil)
	handler := NewOTelHandler(baseHandler)

	newHandler := handler.WithAttrs([]slog.Attr{slog.String("key", "value")})
	if newHandler == nil {
		t.Fatal("WithAttrs returned nil")
	}
	_, ok := newHandler.(*OTelHandler)
	if !ok {
		t.Error("WithAttrs did not return OTelHandler")
	}
}

func TestOTelHandler_WithGroup(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, nil)
	handler := NewOTelHandler(baseHandler)

	newHandler := handler.WithGroup("testgroup")
	if newHandler == nil {
		t.Fatal("WithGroup returned nil")
	}
	_, ok := newHandler.(*OTelHandler)
	if !ok {
		t.Error("WithGroup did not return OTelHandler")
	}
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty dir returns nil",
			config:  Config{Dir: "", MaxSizeMB: 10, MaxBackups: 5, MaxAgeDays: 7},
			wantErr: false,
		},
		{
			name:    "invalid size",
			config:  Config{Dir: "/tmp", MaxSizeMB: 0, MaxBackups: 5, MaxAgeDays: 7},
			wantErr: true,
		},
		{
			name:    "invalid backups",
			config:  Config{Dir: "/tmp", MaxSizeMB: 10, MaxBackups: 0, MaxAgeDays: 7},
			wantErr: true,
		},
		{
			name:    "invalid age",
			config:  Config{Dir: "/tmp", MaxSizeMB: 10, MaxBackups: 5, MaxAgeDays: 0},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EnableFileLogging(tt.config, "test.log")
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnableFileLogging_UsesRestrictedFileAndDirectoryPerms(t *testing.T) {
	logDir := t.TempDir()
	serviceLogPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(serviceLogPath, []byte("preexisting\n"), 0o600); err != nil {
		t.Fatalf("write preexisting log failed: %v", err)
	}

	config := Config{
		Level:      "info",
		Dir:        logDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 7,
	}

	if _, err := EnableFileLogging(config, "service.log"); err != nil {
		t.Fatalf("EnableFileLogging failed: %v", err)
	}

	fileInfo, err := os.Stat(serviceLogPath)
	if err != nil {
		t.Fatalf("stat %s failed: %v", serviceLogPath, err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o640 {
		t.Fatalf("unexpected perm for %s: got %o want %o", serviceLogPath, got, 0o640)
	}

	dirInfo, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("stat %s failed: %v", logDir, err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o750 {
		t.Fatalf("unexpected perm for %s: got %o want %o", logDir, got, 0o750)
	}
}

func TestNewLoggerWithLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{name: "debug level", level: "debug"},
		{name: "info level", level: "info"},
		{name: "invalid level falls back", level: "invalid_level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLoggerWithLevel(tt.level)
			if logger == nil {
				t.Fatalf("NewLoggerWithLevel(%q) returned nil", tt.level)
			}
		})
	}
}

func TestEnableFileLoggingWithLevel(t *testing.T) {
	logDir := t.TempDir()
	config := Config{
		Dir:        logDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 7,
		Compress:   false,
	}

	logger, err := EnableFileLoggingWithLevel(config, "with-level.log", "warn")
	if err != nil {
		t.Fatalf("EnableFileLoggingWithLevel failed: %v", err)
	}
	if logger == nil {
		t.Fatal("EnableFileLoggingWithLevel returned nil")
	}
}

func TestLogError_LogsWarnWithErrField(t *testing.T) {
	t.Parallel()

	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	testErr := errors.New("something broke")

	LogError(logger, "op.failed", testErr)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	rec := handler.records[0]
	if rec.level != slog.LevelWarn {
		t.Fatalf("level = %v, want %v", rec.level, slog.LevelWarn)
	}
	errVal, ok := rec.attrs["err"]
	if !ok {
		t.Fatal("record missing \"err\" attr")
	}
	if errVal.String() != testErr.Error() {
		t.Fatalf("err attr = %q, want %q", errVal.String(), testErr.Error())
	}
}

func TestLogError_NilLoggerNoops(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogError panicked with nil logger: %v", r)
		}
	}()

	LogError(nil, "op.failed", errors.New("err"))
}

func TestLogError_NilErrorNoops(t *testing.T) {
	t.Parallel()

	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogError(logger, "op.failed", nil)

	if len(handler.records) != 0 {
		t.Fatalf("got %d records, want 0", len(handler.records))
	}
}

func TestLogInfo_LogsInfoWithFields(t *testing.T) {
	t.Parallel()

	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogInfo(logger, "sync.done", "count", 42, "source", "yt")

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	rec := handler.records[0]
	if rec.level != slog.LevelInfo {
		t.Fatalf("level = %v, want %v", rec.level, slog.LevelInfo)
	}
	countVal, ok := rec.attrs["count"]
	if !ok {
		t.Fatal("record missing \"count\" attr")
	}
	if countVal.String() != "42" {
		t.Fatalf("count attr = %q, want \"42\"", countVal.String())
	}
	sourceVal, ok := rec.attrs["source"]
	if !ok {
		t.Fatal("record missing \"source\" attr")
	}
	if sourceVal.String() != "yt" {
		t.Fatalf("source attr = %q, want \"yt\"", sourceVal.String())
	}
}

func TestLogInfo_NilLoggerNoops(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogInfo panicked with nil logger: %v", r)
		}
	}()

	LogInfo(nil, "sync.done", "key", "value")
}
