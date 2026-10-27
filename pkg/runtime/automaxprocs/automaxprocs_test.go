package automaxprocs

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestAutomaxprocsDecision(t *testing.T) {
	tests := []struct {
		name       string
		disable    string
		force      string
		wantRun    bool
		wantReason automaxprocsDecisionReason
	}{
		{name: "native default", wantRun: false, wantReason: automaxprocsDecisionNativeRuntime},
		{name: "forced", force: "1", wantRun: true, wantReason: automaxprocsDecisionForced},
		{name: "disabled overrides forced", disable: "1", force: "1", wantRun: false, wantReason: automaxprocsDecisionDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(disableEnv, tt.disable)
			t.Setenv(forceEnv, tt.force)

			got := decideAutomaxprocs()
			if got.run != tt.wantRun || got.reason != tt.wantReason {
				t.Fatalf("decideAutomaxprocs() = {run:%t reason:%v}, want {run:%t reason:%v}", got.run, got.reason, tt.wantRun, tt.wantReason)
			}
		})
	}
}

func TestInit_LogsDecisionToLogger(t *testing.T) {
	tests := []struct {
		name    string
		disable string
		want    string
	}{
		{name: "native default", want: "automaxprocs skipped"},
		{name: "disabled", disable: "1", want: "automaxprocs disabled by env"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(disableEnv, tt.disable)
			t.Setenv(forceEnv, "")

			var logs bytes.Buffer

			Init(slog.New(slog.NewTextHandler(&logs, nil)))

			if !strings.Contains(logs.String(), tt.want) {
				t.Fatalf("Init() logs = %q, want %q", logs.String(), tt.want)
			}
		})
	}
}

// 전역 slog default를 바꾸므로 병렬로 돌리지 않는다.
func TestInit_NilLoggerUsesDefault(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv(forceEnv, "")

	var logs bytes.Buffer

	testsupport.UseDefaultLogger(t, slog.NewTextHandler(&logs, nil))

	Init(nil)

	if !strings.Contains(logs.String(), "automaxprocs disabled by env") {
		t.Fatalf("default logger output = %q, want disabled decision", logs.String())
	}
}

func TestInit_Forced(t *testing.T) {
	t.Setenv(disableEnv, "")
	t.Setenv(forceEnv, "1")
	Init(slog.New(slog.DiscardHandler))
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "YES", want: true},
		{value: "y", want: true},
		{value: "", want: false},
		{value: "0", want: false},
		{value: "false", want: false},
	}

	for _, tt := range tests {
		if got := isTruthy(tt.value); got != tt.want {
			t.Fatalf("isTruthy(%q) = %t, want %t", tt.value, got, tt.want)
		}
	}
}
