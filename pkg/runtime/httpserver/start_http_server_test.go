package httpserver

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out, err := b.buf.Write(p)
	if err != nil {
		return out, fmt.Errorf("write: %w", err)
	}

	return out, nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestStartServerWithPrefix_CustomErrorText(t *testing.T) {
	t.Parallel()

	wantText := "custom prefix"
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	StartServerWithPrefix(server, wantText, nil, errCh)

	select {
	case err := <-errCh:
		if !strings.Contains(err.Error(), wantText) {
			t.Fatalf("error = %q, want prefix %q", err, wantText)
		}

		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want wrapped %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestStartServerWithPrefix_LogsListenErrorWithErrChNonNil(t *testing.T) {
	t.Parallel()

	wantText := "bind failed"
	wantErr := errors.New("listen failed")
	server := newFakeServer(wantErr, nil)
	errCh := make(chan error, 1)

	var logBuf syncBuffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	StartServerWithPrefix(server, wantText, logger, errCh)

	select {
	case err := <-errCh:
		if !errors.Is(err, wantErr) {
			t.Fatalf("errCh error = %v, want wrapped %v", err, wantErr)
		}

		if !strings.Contains(err.Error(), wantText) {
			t.Fatalf("errCh error = %q, want prefix %q", err, wantText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for errCh")
	}

	deadline := time.After(2 * time.Second)

	for {
		if logged := logBuf.String(); strings.Contains(logged, wantText) {
			break
		}

		select {
		case <-deadline:
			t.Fatalf("listen error not logged with errCh non-nil; log = %q", logBuf.String())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
