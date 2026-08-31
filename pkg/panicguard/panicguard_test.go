package panicguard

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRunRecoversPanicAndLogsStableEvent(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	Run(logger, BackgroundTask, "worker-loop", func() { panic("boom") })

	logged := output.String()

	for _, want := range []string{
		"background goroutine panic recovered",
		"guard=worker-loop",
		"panic=boom",
		"stack=",
		"panicguard.Run",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("panic log = %q, want marker %q", logged, want)
		}
	}
}

func TestRunPreservesGoroutineLogContract(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	Run(logger, Goroutine, "image-reader", func() { panic("boom") })

	logged := output.String()

	for _, want := range []string{
		"msg=goroutine_panic_recovered",
		"goroutine=image-reader",
		"panic=boom",
		"stack=",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("panic log = %q, want marker %q", logged, want)
		}
	}

	if strings.Contains(logged, "guard=") {
		t.Fatalf("panic log = %q, must not contain background guard key", logged)
	}
}

func TestRunDoesNotLogWithoutPanic(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	ran := false

	Run(logger, BackgroundTask, "worker-loop", func() { ran = true })

	if !ran || output.Len() != 0 {
		t.Fatalf("Run() ran = %t, log = %q, want true and empty", ran, output.String())
	}
}

func TestRunENormalReturnAndError(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	if err := RunE(logger, BackgroundTask, "ok", func() error { return nil }); err != nil {
		t.Fatalf("RunE(nil) error = %v, want nil", err)
	}

	wantErr := errors.New("failed")
	if err := RunE(logger, BackgroundTask, "failed", func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("RunE(error) error = %v, want wrapped sentinel", err)
	}

	if output.Len() != 0 {
		t.Fatalf("RunE normal log = %q, want empty", output.String())
	}
}

func TestRunEConvertsErrorAndNonErrorPanics(t *testing.T) {
	t.Parallel()

	panickedErr := errors.New("panicked error")
	err := RunE(nil, BackgroundTask, "error-panic", func() error { panic(panickedErr) })

	if !errors.Is(err, panickedErr) {
		t.Fatalf("RunE(error panic) error = %v, want wrapped sentinel", err)
	}

	err = RunE(nil, Goroutine, "string-panic", func() error { panic("kaboom") })
	if err == nil || !strings.Contains(err.Error(), "string-panic: recovered panic: kaboom") {
		t.Fatalf("RunE(string panic) error = %v, want named recovered panic", err)
	}
}

func TestRunNilLoggerIsSafe(t *testing.T) {
	t.Parallel()

	Run(nil, BackgroundTask, "nil-logger", func() { panic("boom") })
}

func TestRunRejectsUnknownLogContractBeforeCallingFunction(t *testing.T) {
	t.Parallel()

	ran := false

	defer func() {
		if recover() == nil {
			t.Fatal("Run() panic = nil, want invalid contract panic")
		}

		if ran {
			t.Fatal("Run() called function with invalid log contract")
		}
	}()

	Run(nil, LogContract(255), "invalid", func() { ran = true })
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelError}))

	return logger, &output
}
