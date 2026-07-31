package logging

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestSG05AsyncWriterCapsPerLineBytes_38e7cbe7(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := newAsyncDropWriter(&buf, formatText, 64)
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

	// Close가 손실 요약 라인을 덧붙이므로 버퍼 전체가 아니라 전달된 첫 라인으로 상한을 잰다.
	forwarded, _, found := strings.Cut(buf.String(), "\n")
	if !found {
		t.Fatalf("forwarded payload has no record separator: %q", buf.String())
	}
	if got := len(forwarded) + 1; got > writer.maxLineBytes {
		t.Fatalf("forwarded bytes = %d, want <= cap %d (line must be truncated before queuing)", got, writer.maxLineBytes)
	}
	if writer.truncatedCount() != 1 {
		t.Fatalf("truncatedCount() = %d, want 1", writer.truncatedCount())
	}
}

// 절단된 조각이 다음 record와 한 줄로 이어붙으면 JSON lane에서 2건이 함께 깨진다.
func TestAsyncDropWriterTruncationKeepsRecordBoundary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := newAsyncDropWriter(&buf, formatText, 64)
	writer.maxLineBytes = 32

	if _, err := writer.Write(append(bytes.Repeat([]byte("A"), 4096), '\n')); err != nil {
		t.Fatalf("Write(oversize) error = %v", err)
	}
	if _, err := writer.Write([]byte("survivor\n")); err != nil {
		t.Fatalf("Write(survivor) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("truncated record swallowed the next one: %q", buf.String())
	}
	if got := lines[0]; got != strings.Repeat("A", writer.maxLineBytes-1) {
		t.Fatalf("truncated line = %q, want %d A's followed by a newline", got, writer.maxLineBytes-1)
	}
	if lines[1] != "survivor" {
		t.Fatalf("line after truncation = %q, want %q", lines[1], "survivor")
	}
}

// 절단은 조용히 유실되면 안 되고 종료 요약에 관측 가능해야 한다.
func TestAsyncDropWriterCloseReportsTruncationWithoutDrops(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := newAsyncDropWriter(&buf, formatText, 64)
	writer.maxLineBytes = 32

	if _, err := writer.Write(bytes.Repeat([]byte("A"), 4096)); err != nil {
		t.Fatalf("Write(oversize) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if writer.droppedCount() != 0 {
		t.Fatalf("droppedCount() = %d, want 0 (queue never overflowed)", writer.droppedCount())
	}
	if !strings.Contains(buf.String(), "truncated=1") {
		t.Fatalf("close summary must report truncation, got: %q", buf.String())
	}
}

func TestAsyncDropWriterTruncationCountsOnlyDeliveredLines(t *testing.T) {
	t.Parallel()

	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	releaseTarget := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(releaseTarget)

	writer := newAsyncDropWriter(target, formatText, 1)
	writer.maxLineBytes = 16

	const writes = 20
	oversize := bytes.Repeat([]byte("A"), 1024)

	if _, err := writer.Write(oversize); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	<-target.started
	for range writes - 1 {
		if _, err := writer.Write(oversize); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if writer.droppedCount() == 0 {
		t.Fatal("droppedCount() = 0, want > 0 (depth-1 queue must overflow while target stalls)")
	}

	releaseTarget()
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := writer.droppedCount() + writer.truncatedCount(); got != writes {
		t.Fatalf("dropped(%d) + truncated(%d) = %d, want %d (a write is dropped or truncated, never both)",
			writer.droppedCount(), writer.truncatedCount(), got, writes)
	}

	reached := strings.Count(target.String(), strings.Repeat("A", writer.maxLineBytes-1))
	if uint64(reached) != writer.truncatedCount() {
		t.Fatalf("truncatedCount() = %d but %d truncated lines reached the target", writer.truncatedCount(), reached)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("target write failed")
}

func TestAsyncDropWriterTruncationExcludesLinesTargetRejected(t *testing.T) {
	t.Parallel()

	writer := newAsyncDropWriter(failingWriter{}, formatText, 64)
	writer.maxLineBytes = 16

	const writes = 5
	for range writes {
		if _, err := writer.Write(bytes.Repeat([]byte("A"), 1024)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := writer.truncatedCount(); got != 0 {
		t.Fatalf("truncatedCount() = %d, want 0 (no truncated line reached the target)", got)
	}
	if got := writer.droppedCount(); got != writes {
		t.Fatalf("droppedCount() = %d, want %d", got, writes)
	}
}

// '한'은 3바이트라 cap 17의 절단 경계(16)가 rune을 쪼갠다. cap 16이면 경계가 rune에 맞아떨어져
// 절단 로직을 되돌려도 통과한다.
func TestAsyncDropWriterTruncationKeepsRuneBoundary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := newAsyncDropWriter(&buf, formatText, 64)
	writer.maxLineBytes = 17

	if _, err := writer.Write([]byte(strings.Repeat("한", 64))); err != nil {
		t.Fatalf("Write(oversize) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	line, _, found := strings.Cut(buf.String(), "\n")
	if !found {
		t.Fatalf("forwarded payload has no record separator: %q", buf.String())
	}
	if !utf8.ValidString(line) {
		t.Fatalf("truncated line is not valid UTF-8: %q", line)
	}
	if want := strings.Repeat("한", 5); line != want {
		t.Fatalf("truncated line = %q, want %q", line, want)
	}
}

func TestSG05AsyncWriterBoundsQueuedMemory_38e7cbe7(t *testing.T) {
	t.Parallel()

	target := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	const depth = 8
	writer := newAsyncDropWriter(target, formatText, depth)
	writer.maxLineBytes = 16
	releaseTarget := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(releaseTarget)

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

	releaseTarget()
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if writer.truncatedCount() == 0 {
		t.Fatal("truncatedCount() = 0, want > 0 (oversize lines must be truncated)")
	}
}
