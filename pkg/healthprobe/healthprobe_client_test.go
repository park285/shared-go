package healthprobe

import (
	"net/url"
	"testing"
)

func TestNewClientHTTPKeepsSharedTransportWhenPrivateAllowed(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("http://internal.test/health")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	client, closeFn, err := newClient(parsed, internalFetchOptions(tlsOverrides{}))
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	if client.Transport != nil {
		t.Fatalf("newClient() transport = %T, want shared default transport", client.Transport)
	}

	closeFn()
}
