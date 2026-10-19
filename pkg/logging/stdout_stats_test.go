package logging

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotStdoutCountsOutcomesExactlyOnce(t *testing.T) {
	// 프로세스 전체 집계이므로 패키지의 병렬 테스트와 격리한다.
	before := SnapshotStdout()
	payload := oversizedJSON(t)

	var output bytes.Buffer

	writer := newAsyncDropWriter(&output, 4)

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for _, target := range []io.Writer{failingWriter{}, shortStdout{}} {
		async := newAsyncDropWriter(target, 4)

		var summary bytes.Buffer

		guard := &bestEffortWriter{target: async, summary: &summary}

		if _, err := guard.Write(payload); err != nil {
			t.Fatal(err)
		}

		if err := async.Close(); err != nil {
			t.Fatal(err)
		}

		if err := guard.Close(); err != nil {
			t.Fatal(err)
		}

		if err := guard.Close(); err != nil {
			t.Fatal(err)
		}

		record := decodeLogLine(t, summary.String())
		if record["dropped"] != float64(1) || record["truncated"] != float64(0) {
			t.Fatalf("async failure file summary = %v", record)
		}

		syncGuard := &bestEffortWriter{target: target}
		if n, err := syncGuard.Write(payload); n != len(payload) || err != nil {
			t.Fatalf("sync file guard Write = (%d, %v)", n, err)
		}
	}

	after := SnapshotStdout()
	if after.Dropped-before.Dropped != 4 || after.Truncated-before.Truncated != 1 {
		t.Fatalf("counter deltas = (%d, %d), want (4, 1)", after.Dropped-before.Dropped, after.Truncated-before.Truncated)
	}

	if again := SnapshotStdout(); again != after {
		t.Fatalf("snapshot reset or changed counters: %v -> %v", after, again)
	}
}

func TestAsyncDropWriterFullAndClosedOutcomes(t *testing.T) {
	before := SnapshotStdout()
	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(release)

	writer := newAsyncDropWriter(target, 1)
	payload := oversizedJSON(t)

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	<-target.started

	for range 3 {
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
	}

	pending := SnapshotStdout()
	if pending.Dropped-before.Dropped != 2 || pending.Truncated != before.Truncated {
		t.Fatalf("pending outcomes = %v, baseline = %v", pending, before)
	}

	release()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	if len(writer.queue) != 0 {
		t.Fatal("write after close stranded data in the queue")
	}

	after := SnapshotStdout()
	if after.Dropped-before.Dropped != 3 || after.Truncated-before.Truncated != 2 {
		t.Fatalf("final outcomes = %v, baseline = %v", after, before)
	}
}

func TestAsyncDropWriterOwnsQueuedRecordsAndPreservesOrder(t *testing.T) {
	t.Parallel()

	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(release)

	writer := newAsyncDropWriter(target, 2)
	payload := []byte("{\"sequence\":0}\n")

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	<-target.started

	payload[len(payload)-3] = '1'
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	payload[len(payload)-3] = '2'
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	clear(payload)
	release()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSuffix(target.String(), "\n"), "\n")
	if len(lines) != 3 || writer.droppedCount() != 0 {
		t.Fatalf("records=%d dropped=%d", len(lines), writer.droppedCount())
	}

	for sequence, line := range lines {
		if decodeLogLine(t, line)["sequence"] != float64(sequence) {
			t.Fatalf("queued record %d lost input ownership or ordering", sequence)
		}
	}
}

func TestAsyncDropWriterConcurrentAdmissionAndCloseAccountsForEveryWrite(t *testing.T) {
	t.Parallel()

	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(release)

	writer := newAsyncDropWriter(target, 8)
	payload := oversizedJSON(t)

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	<-target.started

	const writes = 32

	start := make(chan struct{})

	var producers sync.WaitGroup

	for range writes {
		producers.Go(func() {
			<-start

			if n, err := writer.Write(payload); n != len(payload) || err != nil {
				t.Errorf("Write = (%d, %v)", n, err)
			}
		})
	}

	closed := make(chan struct{})

	go func() {
		<-start

		if err := writer.Close(); err != nil {
			t.Errorf("Close = %v", err)
		}

		close(closed)
	}()

	close(start)
	producers.Wait()
	release()
	<-closed
	<-writer.stopped

	if got := writer.droppedCount() + writer.truncatedCount(); got != writes+1 {
		t.Fatalf("accounted outcomes = %d, want %d", got, writes+1)
	}

	if len(writer.queue) != 0 {
		t.Fatal("concurrent close stranded admitted records")
	}

	for line := range strings.SplitSeq(strings.TrimSpace(target.String()), "\n") {
		decodeLogLine(t, line)
	}
}

func TestEnableFileLoggingAsyncFailuresReachFileSummary(t *testing.T) {
	t.Parallel()

	for _, target := range []io.Writer{failingWriter{}, shortStdout{}} {
		dir := t.TempDir()
		config := Config{Level: testInfo, Dir: dir, MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}

		logger, closer, err := enableFileLoggingWithStdout(target, config, "failed.log", Options{AsyncStdout: true})
		if err != nil {
			t.Fatal(err)
		}

		logger.Info("diagnostic_failed", "diagnostic", strings.Repeat("detail ", 10000))

		if closeErr := closer.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}

		file, err := os.ReadFile(filepath.Join(dir, "failed.log")) //nolint:gosec // 테스트 소유 임시 파일이다.
		if err != nil {
			t.Fatal(err)
		}

		var summary map[string]any

		for line := range strings.SplitSeq(strings.TrimSpace(string(file)), "\n") {
			record := decodeLogLine(t, line)
			if record["msg"] == "stdout lane write failed" {
				summary = record
			}
		}

		if summary == nil || summary["dropped"] != float64(2) || summary["truncated"] != float64(0) {
			t.Fatalf("file summary while stdout fails = %v", summary)
		}
	}
}

func TestEnableFileLoggingBlockedCloseStillSummarizesKnownDrops(t *testing.T) {
	t.Parallel()

	target := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(release)

	dir := t.TempDir()
	config := Config{Level: testInfo, Dir: dir, MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}

	logger, closer, err := enableFileLoggingWithStdout(target, config, "blocked.log", Options{AsyncStdout: true})
	if err != nil {
		t.Fatal(err)
	}

	<-target.started

	for range asyncStdoutQueueDepth + 1 {
		logger.Info("queue_pressure")
	}

	closed := make(chan struct{})

	go func() {
		if closeErr := closer.Close(); closeErr != nil {
			t.Errorf("Close = %v", closeErr)
		}

		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(asyncDropWriterCloseTimeout + time.Second):
		t.Fatal("file-backed close exceeded bounded stdout wait")
	}

	file, err := os.ReadFile(filepath.Join(dir, "blocked.log")) //nolint:gosec // 테스트 소유 임시 파일이다.
	if err != nil {
		t.Fatal(err)
	}

	var summary map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(string(file)), "\n") {
		record := decodeLogLine(t, line)
		if record["msg"] == "stdout lane write failed" {
			summary = record
		}
	}

	if summary == nil || summary["dropped"] != float64(1) || summary["truncated"] != float64(0) {
		t.Fatalf("timed-out file summary = %v", summary)
	}

	release()

	lanes, ok := closer.(multiCloser)
	if !ok {
		t.Fatalf("unexpected logger closer %T", closer)
	}

	async, ok := lanes[0].(*asyncDropWriter)
	if !ok {
		t.Fatalf("unexpected stdout closer %T", lanes[0])
	}

	<-async.stopped
}
