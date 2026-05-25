package httputil

import (
	"testing"
	"time"
)

func TestTimeoutPresets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		preset  TimeoutPreset
		want    time.Duration
		wantPos bool
	}{
		{"FetchTimeout", FetchTimeout, 10 * time.Second, true},
		{"LongPollTimeout", LongPollTimeout, 30 * time.Second, true},
		{"ScraperTimeout", ScraperTimeout, 15 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.preset.Duration()
			if got != tt.want {
				t.Fatalf("%s.Duration() = %s, want %s", tt.name, got, tt.want)
			}
			if tt.wantPos && got <= 0 {
				t.Fatalf("%s.Duration() must be positive", tt.name)
			}
		})
	}
}

func TestNewClientWithPreset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		preset TimeoutPreset
	}{
		{"FetchTimeout", FetchTimeout},
		{"LongPollTimeout", LongPollTimeout},
		{"ScraperTimeout", ScraperTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClientWithPreset(tt.preset)
			if client == nil {
				t.Fatalf("NewClientWithPreset(%s) returned nil", tt.name)
			}
			if client.Timeout != tt.preset.Duration() {
				t.Fatalf("client.Timeout = %s, want %s", client.Timeout, tt.preset.Duration())
			}
		})
	}
}

func TestNewExternalAPIClientWithPreset(t *testing.T) {
	t.Parallel()

	client := NewExternalAPIClientWithPreset(FetchTimeout)
	if client == nil {
		t.Fatal("NewExternalAPIClientWithPreset(FetchTimeout) returned nil")
	}
	if client.Timeout != FetchTimeout.Duration() {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, FetchTimeout.Duration())
	}
	if client.Transport == nil {
		t.Fatal("client.Transport is nil, want profiled transport")
	}
}

func TestNewInternalServiceClientWithPreset(t *testing.T) {
	t.Parallel()

	client := NewInternalServiceClientWithPreset(FetchTimeout)
	if client == nil {
		t.Fatal("NewInternalServiceClientWithPreset(FetchTimeout) returned nil")
	}
	if client.Timeout != FetchTimeout.Duration() {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, FetchTimeout.Duration())
	}
	if client.Transport == nil {
		t.Fatal("client.Transport is nil, want profiled transport")
	}
}
