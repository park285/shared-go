package openaipreset

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

const (
	maxProviderResponseBytes int64 = 1 << 20
	maxOutputTokens          int64 = 16 << 10
)

func responseLimitedHTTPClient(client *http.Client, maxBytes int64) *http.Client {
	cloned := *client

	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	cloned.Transport = responseLimitRoundTripper{base: base, maxBytes: maxBytes}

	return &cloned
}

type responseLimitRoundTripper struct {
	base     http.RoundTripper
	maxBytes int64
}

func (t responseLimitRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.Body == nil {
		return resp, nil
	}

	if resp.ContentLength > t.maxBytes {
		_ = resp.Body.Close()

		return nil, httputil.ErrResponseBodyTooLarge
	}

	resp.Body = &limitedResponseBody{body: resp.Body, remaining: t.maxBytes}

	return resp, nil
}

func (t responseLimitRoundTripper) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type limitedResponseBody struct {
	body      io.ReadCloser
	remaining int64
	closeOnce sync.Once
	closeErr  error
}

func (b *limitedResponseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if b.remaining == 0 {
		var probe [1]byte

		n, err := b.body.Read(probe[:])
		if n > 0 {
			return 0, errors.Join(httputil.ErrResponseBodyTooLarge, b.Close())
		}

		return 0, err
	}

	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}

	n, err := b.body.Read(p)

	b.remaining -= int64(n)

	return n, err
}

func (b *limitedResponseBody) Close() error {
	b.closeOnce.Do(func() {
		b.closeErr = b.body.Close()
	})

	return b.closeErr
}
