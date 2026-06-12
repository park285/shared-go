package httputil

import (
	"testing"
	"time"
)

func BenchmarkNewProfiledClient(b *testing.B) {
	profile := TransportProfile{
		Timeout:               30 * time.Second,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          128,
		MaxConnsPerHost:       32,
		MaxIdleConnsPerHost:   16,
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = NewProfiledClient(profile)
	}
}
