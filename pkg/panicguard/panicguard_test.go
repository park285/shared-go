package panicguard

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// testContract는 소비자가 정의하는 LogContract 값의 예다.
var testContract = LogContract{Message: "background goroutine panic recovered", NameKey: "guard"}

func TestRunRecoversPanicAndLogsStableEvent(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	Run(logger, testContract, "worker-loop", func() { panic("boom") })

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

func TestRunUsesCallerLogContract(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	Run(logger, LogContract{Message: "goroutine_panic_recovered", NameKey: "goroutine"}, "image-reader", func() { panic("boom") })

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
		t.Fatalf("panic log = %q, must not contain another contract key", logged)
	}
}

func TestRunDoesNotLogWithoutPanic(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	ran := false

	Run(logger, testContract, "worker-loop", func() { ran = true })

	if !ran || output.Len() != 0 {
		t.Fatalf("Run() ran = %t, log = %q, want true and empty", ran, output.String())
	}
}

func TestRunENormalReturnAndError(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	if err := RunE(logger, testContract, "ok", func() error { return nil }); err != nil {
		t.Fatalf("RunE(nil) error = %v, want nil", err)
	}

	wantErr := errors.New("failed")
	if err := RunE(logger, testContract, "failed", func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("RunE(error) error = %v, want wrapped sentinel", err)
	}

	if output.Len() != 0 {
		t.Fatalf("RunE normal log = %q, want empty", output.String())
	}
}

func TestRunEConvertsErrorAndNonErrorPanics(t *testing.T) {
	t.Parallel()

	panickedErr := errors.New("panicked error")
	err := RunE(nil, testContract, "error-panic", func() error { panic(panickedErr) })

	if !errors.Is(err, panickedErr) {
		t.Fatalf("RunE(error panic) error = %v, want wrapped sentinel", err)
	}

	err = RunE(nil, testContract, "string-panic", func() error { panic("kaboom") })
	if err == nil || err.Error() != "panicguard: string-panic: recovered panic: kaboom" {
		t.Fatalf("RunE(string panic) error = %v, want named recovered panic", err)
	}
}

func TestRunNilLoggerIsSafe(t *testing.T) {
	t.Parallel()

	Run(nil, testContract, "nil-logger", func() { panic("boom") })
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

	Run(nil, LogContract{Message: "missing name key"}, "invalid", func() { ran = true })
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelError}))

	return logger, &output
}

func TestRunUsesConsumerDefinedLogContract(t *testing.T) {
	t.Parallel()

	logger, output := testLogger()
	contract := LogContract{Message: "scheduler panic recovered", NameKey: "job"}

	Run(logger, contract, "nightly-digest", func() { panic("boom") })

	logged := output.String()

	for _, want := range []string{"msg=\"scheduler panic recovered\"", "job=nightly-digest", "panic=boom"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("panic log = %q, want marker %q", logged, want)
		}
	}
}

func TestRunEReturnsFunctionErrorUnwrapped(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("load snapshot: timeout")

	err := RunE(nil, testContract, "snapshot", func() error { return wantErr })
	if err != wantErr { //nolint:errorlint // 호출자 오류를 감싸지 않고 그대로 돌려주는 계약을 확인한다.
		t.Fatalf("RunE() error = %v, want the function error itself", err)
	}
}
