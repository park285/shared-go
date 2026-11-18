package httpserver

import (
	"net/http"
	"time"
)

type ServerOptions struct {
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

func NewServer(addr string, handler http.Handler, opts ServerOptions) *http.Server {
	if handler == nil {
		handler = http.NewServeMux()
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	if opts.IdleTimeout > 0 {
		server.IdleTimeout = opts.IdleTimeout
	}

	if opts.MaxHeaderBytes > 0 {
		server.MaxHeaderBytes = opts.MaxHeaderBytes
	}

	return server
}
