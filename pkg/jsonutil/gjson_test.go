package jsonutil

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
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
			name:     "zero maxBytes falls back to ReadAll",
			input:    "hello",
			maxBytes: 0,
			want:     "hello",
		},
		{
			name:     "negative maxBytes falls back to ReadAll",
			input:    "world",
			maxBytes: -1,
			want:     "world",
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

func TestReadAllLimit_ReaderError_NoLimit(t *testing.T) {
	t.Parallel()
	_, err := ReadAllLimit(errReader{}, 0)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadAllLimit() error = %v, want %v", err, io.ErrUnexpectedEOF)
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
