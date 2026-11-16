package openaipreset

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
)

const (
	maxProviderResponseBytes int64 = 1 << 20
	maxOutputTokens          int64 = 16 << 10
)

// responseLimitedHTTPClient clones client so the caller's transport remains untouched while
// every provider response is bounded before the SDK can decode JSON or accumulate SSE events.
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
		return resp, err
	}

	if resp == nil || resp.Body == nil {
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
			_ = b.Close()

			return 0, httputil.ErrResponseBodyTooLarge
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

func safeResponseError(err error) error {
	if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		return httputil.ErrResponseBodyTooLarge
	}

	return openaidiag.SafeError(err)
}
