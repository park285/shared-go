package health

import (
	"strings"
	"testing"
)

func TestInit_SetsVersionAndStartTime(t *testing.T) {
	Init("v2.0.0")

	got := GetVersion()
	if got == "" {
		t.Fatal("GetVersion() returned empty after Init")
	}
}

func TestInit_IdempotentViaSyncOnce(t *testing.T) {
	Init("first-call-wins")

	first := GetVersion()

	Init("should-be-ignored")

	second := GetVersion()

	if first != second {
		t.Fatalf("Init() should be idempotent via sync.Once: first=%q second=%q", first, second)
	}
}

func TestGetUptime_ReturnsNonEmpty(t *testing.T) {
	Init("test")

	got := GetUptime()
	if got == "" {
		t.Fatal("GetUptime() returned empty string")
	}

	if !strings.Contains(got, "s") {
		t.Fatalf("GetUptime() = %q, expected to contain duration suffix", got)
	}
}

func TestGet_FullResponse(t *testing.T) {
	Init("test")

	resp := Get()
	if resp.Status != "ok" {
		t.Fatalf("Get().Status = %q, want %q", resp.Status, "ok")
	}

	if resp.Goroutines <= 0 {
		t.Fatalf("Get().Goroutines = %d, want > 0", resp.Goroutines)
	}

	if resp.Uptime == "" {
		t.Fatal("Get().Uptime is empty")
	}

	if resp.Version == "" {
		t.Fatal("Get().Version is empty")
	}
}
