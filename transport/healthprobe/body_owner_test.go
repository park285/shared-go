package healthprobe

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type probeBody struct {
	reader   io.Reader
	closeErr error
	closes   int
}

func (b *probeBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *probeBody) Close() error {
	b.closes++

	return b.closeErr
}

type probeReadError struct {
	err error
}

func (r probeReadError) Read([]byte) (int, error) {
	return 0, r.err
}

func TestReadCappedBodyClosesOnce(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")

	for _, test := range []struct {
		name         string
		reader       io.Reader
		closeErr     error
		want         string
		wantErr      error
		wantCloseErr bool
	}{
		{name: "success", reader: strings.NewReader("ok"), want: "ok"},
		{name: "read error", reader: probeReadError{err: readErr}, wantErr: readErr},
		{name: "close error", reader: strings.NewReader("ok"), closeErr: closeErr, wantErr: closeErr, wantCloseErr: true},
		{name: "read and close errors", reader: probeReadError{err: readErr}, closeErr: closeErr, wantErr: readErr, wantCloseErr: true},
		{name: "too large", reader: strings.NewReader("oversized"), wantErr: errBodyTooLarge},
		{name: "too large and close error", reader: strings.NewReader("oversized"), closeErr: closeErr, wantErr: errBodyTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := &probeBody{reader: test.reader, closeErr: test.closeErr}
			got, err := readCappedBody(body, 3)

			if !errors.Is(err, test.wantErr) {
				t.Fatalf("readCappedBody error = %v, want %v", err, test.wantErr)
			}

			if errors.Is(err, closeErr) != test.wantCloseErr {
				t.Fatalf("close error preserved = %t, want %t", errors.Is(err, closeErr), test.wantCloseErr)
			}

			if string(got) != test.want {
				t.Fatalf("readCappedBody bytes = %q, want %q", got, test.want)
			}

			if body.closes != 1 {
				t.Fatalf("body Close count = %d, want 1", body.closes)
			}
		})
	}
}

type probePanicReader struct{}

func (probePanicReader) Read([]byte) (int, error) {
	panic("read panic")
}

func TestReadCappedBodyPanicClosesOnce(t *testing.T) {
	t.Parallel()

	body := &probeBody{reader: probePanicReader{}}

	defer func() {
		if got := recover(); got != "read panic" {
			t.Errorf("read panic = %v, want original panic", got)
		}

		if body.closes != 1 {
			t.Errorf("body Close count = %d, want 1", body.closes)
		}
	}()

	if _, err := readCappedBody(body, 3); err != nil {
		t.Fatal(err)
	}
}

type probeRoundTripper struct{ body io.ReadCloser }

func (r probeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: r.body, Request: request}, nil
}

func TestFetchURLClosesBodyOnce(t *testing.T) {
	body := &probeBody{reader: strings.NewReader("ok")}
	previous := http.DefaultTransport

	http.DefaultTransport = probeRoundTripper{body: body}

	t.Cleanup(func() { http.DefaultTransport = previous })

	data, err := fetchURL(t.Context(), "http://127.0.0.1/health", nil, internalFetchOptions(tlsOverrides{}))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "ok" {
		t.Fatalf("fetchURL bytes = %q, want ok", data)
	}

	if body.closes != 1 {
		t.Fatalf("body Close count = %d, want 1", body.closes)
	}
}
