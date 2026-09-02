package health

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"0은 0s", 0, "0s"},
		{"1h30m15s 정확히 일치", 1*time.Hour + 30*time.Minute + 15*time.Second, "1h30m15s"},
		{"500ms는 1초로 반올림", 500 * time.Millisecond, "1s"},
		{"5m는 5m0s", 5 * time.Minute, "5m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestGetVersion_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	got := GetVersion()
	if got == "" {
		t.Fatal("GetVersion() returned empty string")
	}
}

func TestGet_StatusOK(t *testing.T) {
	t.Parallel()

	resp := Get()
	if resp.Status != "ok" {
		t.Errorf("Get().Status = %q, want %q", resp.Status, "ok")
	}
}
