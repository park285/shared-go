package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

type testConfig struct {
	Logging sharedlogging.Config
	Port    int
}

type testRuntime struct {
	runCalls   int
	closeCalls int
}

func (r *testRuntime) Run() {
	r.runCalls++
}

func (r *testRuntime) Close() {
	r.closeCalls++
}

func TestRun_ReturnsExitCodeOneWhenLoadConfigFails(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("load boom")
	var stderr bytes.Buffer
	initCalled := false
	loggerCalled := false
	buildCalled := false

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(string) { initCalled = true },
		LoadConfig:             func() (*testConfig, error) { return nil, loadErr },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			loggerCalled = true
			return slog.New(slog.DiscardHandler), nil
		},
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			buildCalled = true
			return &testRuntime{}, nil
		},
		Stderr: &stderr,
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if !initCalled {
		t.Fatal("Initialize() was not called")
	}
	if loggerCalled {
		t.Fatal("NewLogger() should not be called on config load failure")
	}
	if buildCalled {
		t.Fatal("BuildRuntime() should not be called on config load failure")
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to load test config: load boom") {
		t.Fatalf("stderr = %q, want load failure message", got)
	}
}

func TestRun_ReturnsExitCodeOneWhenLoggerInitFails(t *testing.T) {
	t.Parallel()

	loggerErr := errors.New("logger boom")
	var stderr bytes.Buffer
	buildCalled := false

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(string) {},
		LoadConfig:             func() (*testConfig, error) { return &testConfig{}, nil },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return nil, loggerErr
		},
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			buildCalled = true
			return &testRuntime{}, nil
		},
		Stderr: &stderr,
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if buildCalled {
		t.Fatal("BuildRuntime() should not be called on logger init failure")
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to initialize logger: logger boom") {
		t.Fatalf("stderr = %q, want logger init failure message", got)
	}
}

func TestRun_BuildsRunsAndClosesRuntime(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{}
	var initializedVersion string
	var builtConfig *testConfig
	var buildCtxDeadline time.Time
	buildStartedAt := time.Now()

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(version string) { initializedVersion = version },
		LoadConfig:             func() (*testConfig, error) { return &testConfig{Port: 30001}, nil },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		StartupMessage: "Test runtime starting...",
		StartupFields: func(config *testConfig) []any {
			return []any{slog.Int("port", config.Port)}
		},
		BuildTimeout: 25 * time.Millisecond,
		BuildRuntime: func(ctx context.Context, config *testConfig, logger *slog.Logger) (*testRuntime, error) {
			if logger == nil {
				t.Fatal("BuildRuntime() logger = nil")
			}
			builtConfig = config
			var ok bool
			buildCtxDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("BuildRuntime() context missing deadline")
			}
			return runtime, nil
		},
		BuildErrorMessage: "Failed to build test runtime",
	})

	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0", exitCode)
	}
	if initializedVersion != "test-version" {
		t.Fatalf("Initialize() version = %q, want %q", initializedVersion, "test-version")
	}
	if builtConfig == nil || builtConfig.Port != 30001 {
		t.Fatalf("BuildRuntime() config = %#v, want port 30001", builtConfig)
	}
	buildTimeout := buildCtxDeadline.Sub(buildStartedAt)
	if buildTimeout <= 0 || buildTimeout > time.Second {
		t.Fatalf("BuildRuntime() timeout window = %v, want positive bounded timeout", buildTimeout)
	}
	if runtime.runCalls != 1 {
		t.Fatalf("runtime.Run() calls = %d, want 1", runtime.runCalls)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("runtime.Close() calls = %d, want 1", runtime.closeCalls)
	}
}

func TestRun_ReturnsExitCodeOneWhenBuildRuntimeFails(t *testing.T) {
	t.Parallel()

	buildErr := errors.New("build boom")

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:    "v1",
		Initialize: func(string) {},
		LoadConfig: func() (*testConfig, error) { return &testConfig{}, nil },
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		BuildTimeout: 50 * time.Millisecond,
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			return nil, buildErr
		},
		BuildErrorMessage: "Failed to build test runtime",
		Stderr:            &bytes.Buffer{},
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
}

func TestRun_BuildRuntimeFailUsesDefaultErrorMessage(t *testing.T) {
	t.Parallel()

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:    "v1",
		Initialize: func(string) {},
		LoadConfig: func() (*testConfig, error) { return &testConfig{}, nil },
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		BuildTimeout: 50 * time.Millisecond,
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			return nil, errors.New("fail")
		},
		Stderr: &bytes.Buffer{},
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
}

func TestRun_SkipsStartupMessageWhenEmpty(t *testing.T) {
	t.Parallel()

	rt := &testRuntime{}
	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:    "v1",
		Initialize: func(string) {},
		LoadConfig: func() (*testConfig, error) { return &testConfig{}, nil },
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		BuildTimeout: 50 * time.Millisecond,
		BuildRuntime: func(_ context.Context, _ *testConfig, _ *slog.Logger) (*testRuntime, error) {
			return rt, nil
		},
		Stderr: &bytes.Buffer{},
	})

	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0", exitCode)
	}
	if rt.runCalls != 1 {
		t.Fatalf("runtime.Run() calls = %d, want 1", rt.runCalls)
	}
}

func TestRun_DefaultLoadConfigErrorMessage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:    "v1",
		Initialize: func(string) {},
		LoadConfig: func() (*testConfig, error) { return nil, errors.New("oops") },
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		BuildTimeout: 50 * time.Millisecond,
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			return &testRuntime{}, nil
		},
		Stderr: &stderr,
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to load config: oops") {
		t.Fatalf("stderr = %q, want default config error message", got)
	}
}

func TestRun_NilInitializeUsesDefault(t *testing.T) {
	t.Parallel()

	rt := &testRuntime{}
	exitCode := Run(Options[*testConfig, *testRuntime]{
		LoadConfig: func() (*testConfig, error) { return &testConfig{}, nil },
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		BuildTimeout: 50 * time.Millisecond,
		BuildRuntime: func(_ context.Context, _ *testConfig, _ *slog.Logger) (*testRuntime, error) {
			return rt, nil
		},
		Stderr: &bytes.Buffer{},
	})

	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0", exitCode)
	}
}

func TestNewLogger_DefaultPathWithLoggerConfig(t *testing.T) {
	t.Parallel()

	cfg := &testConfig{}
	logger, err := newLogger(Options[*testConfig, *testRuntime]{
		LoggerConfig: func(c *testConfig) sharedlogging.Config {
			return sharedlogging.Config{}
		},
		LoggerLevel: func(c *testConfig) string {
			return "debug"
		},
		LoggerFileName: "test.log",
	}, cfg)

	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}
	if logger == nil {
		t.Fatal("newLogger() returned nil logger")
	}
}

func TestNewLogger_DefaultPathAllNil(t *testing.T) {
	t.Parallel()

	cfg := &testConfig{}
	logger, err := newLogger(Options[*testConfig, *testRuntime]{
		LoggerFileName: "test.log",
	}, cfg)

	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}
	if logger == nil {
		t.Fatal("newLogger() returned nil logger")
	}
}

func TestStartupFields_NilLoggerLevelAndNilStartupFields(t *testing.T) {
	t.Parallel()

	cfg := &testConfig{}
	fields := startupFields(Options[*testConfig, *testRuntime]{
		Version: "v1",
	}, cfg)

	if len(fields) != 1 {
		t.Fatalf("startupFields() len = %d, want 1", len(fields))
	}
}

func TestStartupFields_WithLoggerLevelNoStartupFields(t *testing.T) {
	t.Parallel()

	cfg := &testConfig{}
	fields := startupFields(Options[*testConfig, *testRuntime]{
		Version:     "v1",
		LoggerLevel: func(c *testConfig) string { return "info" },
	}, cfg)

	if len(fields) != 2 {
		t.Fatalf("startupFields() len = %d, want 2", len(fields))
	}
}

func TestFallback_ReturnsValueWhenNonEmpty(t *testing.T) {
	t.Parallel()

	got := fallback("custom", "default")
	if got != "custom" {
		t.Fatalf("fallback() = %q, want %q", got, "custom")
	}
}

func TestFallback_ReturnsDefaultWhenEmpty(t *testing.T) {
	t.Parallel()

	got := fallback("", "default")
	if got != "default" {
		t.Fatalf("fallback() = %q, want %q", got, "default")
	}
}

func TestRuntimeStderr_ReturnsProvidedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	got := runtimeStderr(&buf)
	if got != &buf {
		t.Fatal("runtimeStderr() did not return provided writer")
	}
}

func TestRuntimeStderr_ReturnsOsStderrWhenNil(t *testing.T) {
	t.Parallel()

	got := runtimeStderr(nil)
	if got == nil {
		t.Fatal("runtimeStderr(nil) returned nil")
	}
}

func TestPrintBootstrapError_AlwaysReturnsOne(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	code := printBootstrapError(&buf, "test error", errors.New("boom"))
	if code != 1 {
		t.Fatalf("printBootstrapError() = %d, want 1", code)
	}
	if got := buf.String(); got != "test error: boom\n" {
		t.Fatalf("printBootstrapError() output = %q, want %q", got, "test error: boom\n")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrintBootstrapError_ReturnsOneOnWriteFailure(t *testing.T) {
	t.Parallel()

	code := printBootstrapError(failWriter{}, "msg", errors.New("err"))
	if code != 1 {
		t.Fatalf("printBootstrapError() = %d, want 1", code)
	}
}
