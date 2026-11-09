package httpserver

import (
	"net/http"
	"testing"
	"time"
)

func TestNewServerAppliesServerOptions(t *testing.T) {
	const (
		addr              = "127.0.0.1:40258"
		readHeaderTimeout = 11 * time.Second
		idleTimeout       = 17 * time.Second
		maxHeaderBytes    = 8192
	)

	server := NewServer(addr, nil, ServerOptions{
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	})

	if server.Addr != addr {
		t.Fatalf("server address = %q, want %q", server.Addr, addr)
	}

	if server.Handler == nil {
		t.Fatal("server handler is nil")
	}

	if server.ReadHeaderTimeout != readHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, readHeaderTimeout)
	}

	if server.ReadTimeout != 0 {
		t.Fatalf("ReadTimeout = %s, want 0 because body budget starts at handler ingress", server.ReadTimeout)
	}

	if server.IdleTimeout != idleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, idleTimeout)
	}

	if server.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxHeaderBytes)
	}
}

func TestNewServerPreservesDisabledReadTimeouts(t *testing.T) {
	server := NewServer(":0", http.NotFoundHandler(), ServerOptions{})

	if server.ReadHeaderTimeout != 0 || server.ReadTimeout != 0 {
		t.Fatalf("disabled read timeouts = header %s body %s, want 0/0", server.ReadHeaderTimeout, server.ReadTimeout)
	}
}
