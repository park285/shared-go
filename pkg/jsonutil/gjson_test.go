package jsonutil

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/park285/shared-go/pkg/httputil"
)

func TestReadAllLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		maxBytes  int64
		want      string
		wantError error
	}{
		{
			name:     "body within limit",
			input:    `{"ok":true}`,
			maxBytes: 100,
			want:     `{"ok":true}`,
		},
		{
			name:     "body exactly at limit",
			input:    "12345",
			maxBytes: 5,
			want:     "12345",
		},
		{
			name:      "body exceeds limit by one",
			input:     "123456",
			maxBytes:  5,
			wantError: ErrBodyTooLarge,
		},
		{
			name:      "body far exceeds limit",
			input:     strings.Repeat("x", 1024),
			maxBytes:  10,
			wantError: ErrBodyTooLarge,
		},
		{
			name:      "zero maxBytes is rejected",
			input:     "hello",
			maxBytes:  0,
			wantError: httputil.ErrInvalidBodyLimit,
		},
		{
			name:      "negative maxBytes is rejected",
			input:     "world",
			maxBytes:  -1,
			wantError: httputil.ErrInvalidBodyLimit,
		},
		{
			name:     "empty body within limit",
			input:    "",
			maxBytes: 10,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := strings.NewReader(tt.input)
			got, err := ReadAllLimit(r, tt.maxBytes)

			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("ReadAllLimit() error = %v, want %v", err, tt.wantError)
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadAllLimit() unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("ReadAllLimit() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestReadAllLimit_ReaderError(t *testing.T) {
	t.Parallel()
	_, err := ReadAllLimit(errReader{}, 100)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadAllLimit() error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

type endlessReader struct {
	reads int
}

func (r *endlessReader) Read(p []byte) (int, error) {
	r.reads++
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestReadAllLimit_NonPositiveLimitNeverReadsUnbounded(t *testing.T) {
	t.Parallel()

	for _, maxBytes := range []int64{0, -1, math.MinInt64} {
		r := &endlessReader{}
		got, err := ReadAllLimit(r, maxBytes)
		if !errors.Is(err, httputil.ErrInvalidBodyLimit) {
			t.Fatalf("ReadAllLimit(_, %d) error = %v, want ErrInvalidBodyLimit", maxBytes, err)
		}
		if got != nil {
			t.Fatalf("ReadAllLimit(_, %d) = %q, want nil", maxBytes, got)
		}
		if r.reads != 0 {
			t.Fatalf("ReadAllLimit(_, %d) read %d times, want 0", maxBytes, r.reads)
		}
	}
}

func TestReadAllLimit_TooLargeMatchesHTTPUtilSentinel(t *testing.T) {
	t.Parallel()

	_, err := ReadAllLimit(strings.NewReader("123456"), 5)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("ReadAllLimit() error = %v, want ErrBodyTooLarge", err)
	}
	if !errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("ReadAllLimit() error = %v, want httputil.ErrResponseBodyTooLarge", err)
	}
}

func TestReadAllLimit_LargeBody(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("a"), 4096)
	got, err := ReadAllLimit(bytes.NewReader(body), 8192)
	if err != nil {
		t.Fatalf("ReadAllLimit() unexpected error: %v", err)
	}
	if len(got) != 4096 {
		t.Errorf("ReadAllLimit() len = %d, want 4096", len(got))
	}
}
