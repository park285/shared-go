package logging

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	asyncStdoutQueueDepth       = 256
	asyncStdoutMaxLineBytes     = 64 << 10
	asyncDropWriterCloseTimeout = 2 * time.Second
)

type asyncDropWriter struct {
	target    io.Writer
	queue     chan queuedLine
	done      chan struct{}
	stopped   chan struct{}
	mu        sync.Mutex
	closed    bool
	dropped   atomic.Uint64
	truncated atomic.Uint64
}

type queuedLine struct {
	data      []byte
	truncated bool
}

func newAsyncDropWriter(target io.Writer, depth int) *asyncDropWriter {
	if depth <= 0 {
		depth = asyncStdoutQueueDepth
	}

	w := &asyncDropWriter{
		target:  target,
		queue:   make(chan queuedLine, depth),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	go w.run()

	return w
}

func (w *asyncDropWriter) run() {
	defer close(w.stopped)

	for {
		select {
		case line := <-w.queue:
			w.forward(line)
		case <-w.done:
			w.drain()
			w.writeLossSummary()

			return
		}
	}
}

func (w *asyncDropWriter) drain() {
	for {
		select {
		case line := <-w.queue:
			w.forward(line)
		default:
			return
		}
	}
}

func (w *asyncDropWriter) forward(line queuedLine) {
	if n, err := w.target.Write(line.data); err != nil || n != len(line.data) {
		w.recordDrop()

		return
	}

	if line.truncated {
		w.truncated.Add(1)
		stdoutTruncated.Add(1)
	}
}

func (w *asyncDropWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed || len(w.queue) == cap(w.queue) {
		w.recordDrop()

		return len(p), nil
	}

	w.queue <- w.boundedCopy(p)

	return len(p), nil
}

func (w *asyncDropWriter) recordDrop() {
	w.dropped.Add(1)
	stdoutDropped.Add(1)
}

func (w *asyncDropWriter) boundedCopy(p []byte) queuedLine {
	if len(p) <= asyncStdoutMaxLineBytes {
		return queuedLine{data: bytes.Clone(p)}
	}

	data := []byte(`{"stdout_truncated":true,"stdout_original_bytes":`)

	data = strconv.AppendInt(data, int64(len(p)), 10)

	var fields struct {
		Time      jsontext.Value `json:"time"`
		Level     jsontext.Value `json:"level"`
		Message   jsontext.Value `json:"msg"`
		Event     jsontext.Value `json:"event"`
		TraceID   jsontext.Value `json:"trace_id"`
		SpanID    jsontext.Value `json:"span_id"`
		RequestID jsontext.Value `json:"request_id"`
	}

	if err := jsonv2.Unmarshal(p, &fields, jsontext.AllowDuplicateNames(true)); err != nil { //nolint:forbidigo // slog JSONHandler는 같은 key를 중복 출력할 수 있어, 큰 stdout 줄 요약은 중복 이름을 허용해야 필드를 잃지 않는다.
		data = append(data, ",\"stdout_invalid_json\":true}\n"...)

		return queuedLine{data: data, truncated: true}
	}

	for _, field := range [...]struct {
		key string
		raw jsontext.Value
	}{
		{"time", fields.Time},
		{"level", fields.Level},
		{"msg", fields.Message},
		{"event", fields.Event},
		{"trace_id", fields.TraceID},
		{"span_id", fields.SpanID},
		{"request_id", fields.RequestID},
	} {
		var value string

		if len(field.raw) == 0 || jsonv2.Unmarshal(field.raw, &value) != nil {
			continue
		}

		if len(value) > 1024 {
			end := 1024
			for !utf8.RuneStart(value[end]) {
				end--
			}

			value = value[:end]
		}

		data = append(data, ',', '"')
		data = append(data, field.key...)
		data = append(data, '"', ':')

		var quoteErr error

		data, quoteErr = jsontext.AppendQuote(data, value)
		if quoteErr != nil {
			data = append(data, `,"stdout_invalid_utf8":true`...)
		}
	}

	data = append(data, '}', '\n')

	return queuedLine{data: data, truncated: true}
}

func (w *asyncDropWriter) Close() error {
	w.mu.Lock()

	if !w.closed {
		w.closed = true
		close(w.done)
	}

	w.mu.Unlock()

	select {
	case <-w.stopped:
	case <-time.After(asyncDropWriterCloseTimeout):
	}

	return nil
}

func (w *asyncDropWriter) writeLossSummary() {
	dropped, truncated := w.dropped.Load(), w.truncated.Load()
	if dropped == 0 && truncated == 0 {
		return
	}

	record := slog.NewRecord(time.Now(), slog.LevelWarn, "async stdout writer lost lines", 0)
	record.AddAttrs(
		slog.Uint64("dropped", dropped),
		slog.Uint64("truncated", truncated),
	)

	//nolint:errcheck // best-effort 종료 진단, 기록 실패는 무시한다.
	_ = newFormatHandler(record.Level, w.target, nil).Handle(context.Background(), record)
}

func (w *asyncDropWriter) droppedCount() uint64 {
	return w.dropped.Load()
}

func (w *asyncDropWriter) truncatedCount() uint64 {
	return w.truncated.Load()
}

type multiCloser []io.Closer

func (c multiCloser) Close() error {
	var closeErr error

	for _, closer := range c {
		if closer == nil {
			continue
		}

		closeErr = errors.Join(closeErr, closer.Close())
	}

	if closeErr != nil {
		return fmt.Errorf("logging: close log lanes: %w", closeErr)
	}

	return nil
}
