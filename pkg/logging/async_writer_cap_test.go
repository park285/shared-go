package logging

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	stdoutTestEventKey     = "event"
	stdoutTestRequestIDKey = "request_id"
)

func oversizedJSON(t *testing.T) []byte {
	t.Helper()

	data, err := jsonv2.Marshal(map[string]string{
		"time":                 "2026-10-13T10:00:00Z",
		"level":                "ERROR",
		"msg":                  "diagnostic failed",
		stdoutTestEventKey:     "diagnostic_failure",
		"trace_id":             "0123456789abcdef0123456789abcdef",
		"span_id":              "0123456789abcdef",
		stdoutTestRequestIDKey: "request-42",
		"diagnostic":           strings.Repeat("한\\\"\n", 20000),
	})
	if err != nil {
		t.Fatal(err)
	}

	return append(data, '\n')
}

func decodeLogLine(t *testing.T, line string) map[string]any {
	t.Helper()

	var record map[string]any

	if err := jsonv2.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("invalid JSON record (%d bytes): %v", len(line), err)
	}

	return record
}

func TestAsyncDropWriterJSONByteBoundary(t *testing.T) {
	t.Parallel()

	for _, size := range []int{asyncStdoutMaxLineBytes - 1, asyncStdoutMaxLineBytes, asyncStdoutMaxLineBytes + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			const envelope = "{\"msg\":\"\"}\n"

			payload := []byte("{\"msg\":\"" + strings.Repeat("x", size-len(envelope)) + "\"}\n")

			var output bytes.Buffer

			writer := newAsyncDropWriter(&output, 4)

			if n, err := writer.Write(payload); n != size || err != nil {
				t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, size)
			}

			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			line, _, _ := strings.Cut(output.String(), "\n")
			if len(line)+1 > asyncStdoutMaxLineBytes {
				t.Fatalf("stdout record exceeds cap: %d", len(line)+1)
			}

			record := decodeLogLine(t, line)
			if size <= asyncStdoutMaxLineBytes {
				if output.String() != string(payload) || writer.truncatedCount() != 0 {
					t.Fatal("in-bound record changed or counted as truncated")
				}
			} else if record["stdout_truncated"] != true || writer.truncatedCount() != 1 {
				t.Fatal("oversized record missing truncation metadata/count")
			}
		})
	}
}

func TestAsyncDropWriterTruncationKeepsJSONCorrelationAndNextRecord(t *testing.T) {
	t.Parallel()

	payload := oversizedJSON(t)
	original := bytes.Clone(payload)

	var output bytes.Buffer

	writer := newAsyncDropWriter(&output, 4)

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(payload, original) {
		t.Fatal("stdout truncation mutated original file record")
	}

	clear(payload) // Write 반환 뒤에는 쓰기 작업이 큐에 넣은 사본을 소유한다.

	if _, err := writer.Write([]byte("{\"msg\":\"survivor\"}\n")); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("records = %d, want truncated record, survivor and summary", len(lines))
	}

	first := decodeLogLine(t, lines[0])
	full := decodeLogLine(t, string(original))

	for _, key := range []string{"time", "level", "msg", stdoutTestEventKey, "trace_id", "span_id", stdoutTestRequestIDKey} {
		if first[key] != full[key] {
			t.Errorf("correlation %s = %v, want %v", key, first[key], full[key])
		}
	}

	if first["stdout_truncated"] != true || first["stdout_original_bytes"] != float64(len(original)) {
		t.Fatalf("truncation metadata = %v", first)
	}

	if _, ok := first["diagnostic"]; ok {
		t.Fatal("oversized arbitrary field retained")
	}

	if decodeLogLine(t, lines[1])["msg"] != "survivor" {
		t.Fatal("truncation damaged next JSON record")
	}

	if decodeLogLine(t, lines[2])["truncated"] != float64(1) {
		t.Fatal("shutdown summary omitted truncation")
	}
}

func TestAsyncDropWriterBoundsEveryRetainedField(t *testing.T) {
	t.Parallel()

	for _, value := range []string{strings.Repeat("\x00", 20000), strings.Repeat("한😀\\\"\n", 10000)} {
		fields := make(map[string]string)

		for _, key := range []string{"time", "level", "msg", stdoutTestEventKey, "trace_id", "span_id", stdoutTestRequestIDKey, "extra"} {
			fields[key] = value
		}

		payload, err := jsonv2.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}

		writer := &asyncDropWriter{}
		line := writer.boundedCopy(payload)

		if len(line.data) > asyncStdoutMaxLineBytes || !utf8.Valid(line.data) || !line.truncated {
			t.Fatalf("invalid bounded result: bytes=%d truncated=%v", len(line.data), line.truncated)
		}

		record := decodeLogLine(t, string(line.data))

		for key := range fields {
			if key == "extra" {
				continue
			}

			got, ok := record[key].(string)
			if !ok || len(got) > 1024 || !strings.HasPrefix(value, got) {
				t.Errorf("%s is not a bounded original string", key)
			}
		}
	}
}

func TestAsyncDropWriterMalformedOversizeDoesNotExposeRawPrefix(t *testing.T) {
	t.Parallel()

	writer := &asyncDropWriter{}
	line := writer.boundedCopy([]byte(strings.Repeat("private-invalid-json", 5000)))
	record := decodeLogLine(t, string(line.data))

	if record["stdout_invalid_json"] != true || record["stdout_truncated"] != true || bytes.Contains(line.data, []byte("private")) {
		t.Fatalf("malformed record was not safely identified: %s", line.data)
	}
}

func TestAsyncDropWriterFullQueueDoesNotAllocate(t *testing.T) {
	// AllocsPerRun이 GOMAXPROCS를 바꾸므로 병렬 실행하지 않는다.
	target := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(release)

	writer := newAsyncDropWriter(target, 1)
	payload := oversizedJSON(t)

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	<-target.started

	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}

	before := writer.droppedCount()
	allocations := testing.AllocsPerRun(100, func() {
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
	})

	if allocations != 0 {
		t.Fatalf("full queue allocated %v times per write", allocations)
	}

	if writer.droppedCount()-before != 101 {
		t.Fatalf("full queue drops = %d, want 101", writer.droppedCount()-before)
	}

	release()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if writer.truncatedCount() != 2 {
		t.Fatalf("delivered truncations = %d, want 2", writer.truncatedCount())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("target write failed")
}

type shortStdout struct{}

func (shortStdout) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestAsyncDropWriterRejectedTruncationsCountOnlyAsDrops(t *testing.T) {
	t.Parallel()

	for _, target := range []io.Writer{failingWriter{}, shortStdout{}} {
		writer := newAsyncDropWriter(target, 8)
		payload := oversizedJSON(t)

		for range 5 {
			if _, err := writer.Write(payload); err != nil {
				t.Fatal(err)
			}
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		if writer.droppedCount() != 5 || writer.truncatedCount() != 0 {
			t.Fatalf("outcomes = (%d, %d), want (5, 0)", writer.droppedCount(), writer.truncatedCount())
		}
	}
}

func findJSONLogRecord(t *testing.T, text, message string, maxLineBytes int) map[string]any {
	t.Helper()

	var found map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
		if maxLineBytes > 0 && len(line)+1 > maxLineBytes {
			t.Fatalf("stdout line exceeds byte cap: %d", len(line)+1)
		}

		record := decodeLogLine(t, line)
		if record["msg"] == message {
			found = record
		}
	}

	if found == nil {
		t.Fatalf("missing log record %q", message)
	}

	return found
}

func TestEnableFileLoggingOversizePreservesSanitizedFileRecord(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	dir := t.TempDir()
	config := Config{Level: testInfo, Dir: dir, MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}

	logger, closer, err := enableFileLoggingWithStdout(&stdout, config, "oversize.log", Options{AsyncStdout: true})
	if err != nil {
		t.Fatal(err)
	}

	const secret = "synthetic-credential-canary"

	diagnostic := strings.Repeat("diagnostic detail ", 5000)
	logger.Error("diagnostic_failed", eventAttr("diagnostic_failed"), requestIDAttr("request-42"),
		slog.String("trace_id", "0123456789abcdef0123456789abcdef"),
		slog.String("span_id", "0123456789abcdef"),
		slog.Any("error", errors.New("API_TOKEN="+secret+" "+diagnostic)))
	logger.Info("survivor")

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	file, err := os.ReadFile(filepath.Join(dir, "oversize.log")) //nolint:gosec // 테스트 소유 임시 파일이다.
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(file, []byte(secret)) || strings.Contains(stdout.String(), secret) {
		t.Fatal("sanitization was bypassed")
	}

	full := findJSONLogRecord(t, string(file), "diagnostic_failed", 0)
	capped := findJSONLogRecord(t, stdout.String(), "diagnostic_failed", asyncStdoutMaxLineBytes)
	summary := findJSONLogRecord(t, string(file), "stdout lane write failed", 0)
	fullError, ok := full["error"].(string)

	if !ok || !strings.Contains(fullError, diagnostic) {
		t.Fatal("file diagnostic was truncated")
	}

	if capped["stdout_truncated"] != true || capped[stdoutTestRequestIDKey] != "request-42" {
		t.Fatalf("stdout missing bounded diagnostic/correlation: %v", capped)
	}

	if summary == nil || summary["dropped"] != float64(0) || summary["truncated"] != float64(1) {
		t.Fatalf("file truncation summary = %v", summary)
	}
}
