package sse

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
)

var (
	ErrEventTooLarge = errors.New("sse: event exceeds limit")
	errInvalidLimits = errors.New("sse: limits must be positive")
)

type Limits struct {
	MaxEventBytes int
	MaxTypeBytes  int
	MaxDataLines  int
}

type Event struct {
	Type string
	Data string
}

func Events(r io.Reader, limits Limits) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if limits.MaxEventBytes <= 0 || limits.MaxTypeBytes <= 0 || limits.MaxDataLines <= 0 {
			yield(Event{}, errInvalidLimits)

			return
		}

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, min(64<<10, limits.MaxEventBytes)), limits.MaxEventBytes)

		pending := eventBuffer{limits: limits}

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if event, ok := pending.take(); ok && !yield(event, nil) {
					return
				}

				continue
			}

			if err := pending.add(line); err != nil {
				yield(Event{}, err)

				return
			}
		}

		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				err = fmt.Errorf("sse: line exceeds %d bytes: %w", limits.MaxEventBytes, ErrEventTooLarge)
			} else {
				err = fmt.Errorf("sse: read stream: %w", err)
			}

			yield(Event{}, err)

			return
		}

		if event, ok := pending.take(); ok {
			yield(event, nil)
		}
	}
}

type eventBuffer struct {
	limits    Limits
	eventType string
	data      strings.Builder
	dataLines int
}

func (b *eventBuffer) add(line string) error {
	if strings.HasPrefix(line, ":") {
		return nil
	}

	field, value, _ := strings.Cut(line, ":")

	value = strings.TrimPrefix(value, " ")

	switch field {
	case "event":
		if len(value) > b.limits.MaxTypeBytes {
			return fmt.Errorf("sse: event type exceeds %d bytes: %w", b.limits.MaxTypeBytes, ErrEventTooLarge)
		}

		b.eventType = value
	case "data":
		if b.dataLines >= b.limits.MaxDataLines {
			return fmt.Errorf("sse: event exceeds %d data lines: %w", b.limits.MaxDataLines, ErrEventTooLarge)
		}

		separator := min(b.dataLines, 1)
		if separator+len(value) > b.limits.MaxEventBytes-b.data.Len() {
			return fmt.Errorf("sse: event data exceeds %d bytes: %w", b.limits.MaxEventBytes, ErrEventTooLarge)
		}

		if separator == 1 {
			b.data.WriteByte('\n')
		}

		b.data.WriteString(value)

		b.dataLines++
	}

	return nil
}

func (b *eventBuffer) take() (Event, bool) {
	defer func() {
		b.eventType = ""
		b.data.Reset()

		b.dataLines = 0
	}()

	if b.dataLines == 0 {
		return Event{}, false
	}

	return Event{Type: b.eventType, Data: b.data.String()}, true
}
