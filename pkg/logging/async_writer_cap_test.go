package logging

import (
	"bytes"
	"testing"
)

func TestSG05AsyncWriterCapsPerLineBytes_38e7cbe7(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := newAsyncDropWriter(&buf, 64)
	writer.maxLineBytes = 32

	oversize := bytes.Repeat([]byte("A"), 4096)
	n, err := writer.Write(oversize)
	if err != nil {
		t.Fatalf("Write(oversize) error = %v, want nil", err)
	}
	if n != len(oversize) {
		t.Fatalf("Write(oversize) n = %d, want %d (io.Writer contract reports full length)", n, len(oversize))
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := buf.Len(); got > writer.maxLineBytes {
		t.Fatalf("forwarded bytes = %d, want <= cap %d (line must be truncated before queuing)", got, writer.maxLineBytes)
	}
	if writer.truncatedCount() != 1 {
		t.Fatalf("truncatedCount() = %d, want 1", writer.truncatedCount())
	}
}

func TestSG05AsyncWriterBoundsQueuedMemory_38e7cbe7(t *testing.T) {
	t.Parallel()

	target := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	const depth = 8
	writer := newAsyncDropWriter(target, depth)
	writer.maxLineBytes = 16
	t.Cleanup(func() { close(target.release) })

	huge := bytes.Repeat([]byte("Z"), 1<<20)
	if writer.maxLineBytes >= len(huge) {
		t.Fatalf("test setup: cap %d must be smaller than line %d", writer.maxLineBytes, len(huge))
	}
	for range 100 {
		if _, err := writer.Write(huge); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if writer.droppedCount() == 0 {
		t.Fatal("droppedCount() = 0, want > 0 (bounded queue must drop excess under stall)")
	}
	if writer.truncatedCount() == 0 {
		t.Fatal("truncatedCount() = 0, want > 0 (oversize lines must be truncated)")
	}
}
