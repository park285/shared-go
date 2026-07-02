package openaipreset

import (
	"net/http"
	"testing"
)

func TestDefaultHTTPClientAllowsSlowFirstHeader(t *testing.T) {
	client := defaultHTTPClient()
	if client.Timeout != defaultRequestTimeout {
		t.Fatalf("Timeout = %v, want %v", client.Timeout, defaultRequestTimeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != defaultRequestTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, defaultRequestTimeout)
	}
}
