package sse_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/park285/shared-go/v2/pkg/sse"
)

var testLimits = sse.Limits{MaxEventBytes: 64, MaxTypeBytes: 16, MaxDataLines: 4}

func collect(t *testing.T, r io.Reader, limits sse.Limits) ([]sse.Event, error) {
	t.Helper()

	var events []sse.Event

	for event, err := range sse.Events(r, limits) {
		if err != nil {
			return events, err
		}

		events = append(events, event)
	}

	return events, nil
}

func TestEventsFramesNamedAndUnnamedEvents(t *testing.T) {
	t.Parallel()

	stream := ": keep-alive\r\n" +
		"event: message_start\r\n" +
		"data: {\"a\":1}\r\n" +
		"id: 7\r\n" +
		"retry: 10\r\n" +
		"\r\n" +
		"event: only-type\n" +
		"\n" +
		"data:first\n" +
		"data:  second\n" +
		"data\n" +
		"\n" +
		"data: trailing"

	events, err := collect(t, strings.NewReader(stream), testLimits)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}

	want := []sse.Event{
		{Type: "message_start", Data: `{"a":1}`},
		{Data: "first\n second\n"},
		{Data: "trailing"},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}

	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %#v, want %#v", i, events[i], want[i])
		}
	}
}

func TestEventsStopsWhenConsumerBreaks(t *testing.T) {
	t.Parallel()

	reads := 0

	for event, err := range sse.Events(strings.NewReader("data: 1\n\ndata: 2\n\n"), testLimits) {
		if err != nil {
			t.Fatalf("Events() error = %v", err)
		}

		reads++

		if event.Data != "1" {
			t.Fatalf("first event = %q, want 1", event.Data)
		}

		break
	}

	if reads != 1 {
		t.Fatalf("reads = %d, want 1", reads)
	}
}

func TestEventsRejectsOversizedEvents(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"type":       "event: " + strings.Repeat("x", testLimits.MaxTypeBytes+1) + "\n",
		"data bytes": "data: " + strings.Repeat("x", 40) + "\ndata: " + strings.Repeat("x", 24) + "\n",
		"data lines": strings.Repeat("data: x\n", testLimits.MaxDataLines+1),
		"line":       "data: " + strings.Repeat("x", testLimits.MaxEventBytes) + "\n",
	}

	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := collect(t, strings.NewReader(stream), testLimits)
			if !errors.Is(err, sse.ErrEventTooLarge) {
				t.Fatalf("Events() error = %v, want ErrEventTooLarge", err)
			}

			if !strings.HasPrefix(err.Error(), "sse: ") {
				t.Fatalf("error %q lacks sse prefix", err)
			}
		})
	}
}

func TestEventsAcceptsDataAtByteLimit(t *testing.T) {
	t.Parallel()

	limits := sse.Limits{MaxEventBytes: 128, MaxTypeBytes: 16, MaxDataLines: 4}
	stream := "data: " + strings.Repeat("x", 63) + "\ndata: " + strings.Repeat("y", 64) + "\n\n"

	events, err := collect(t, strings.NewReader(stream), limits)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}

	if len(events) != 1 || len(events[0].Data) != 128 {
		t.Fatalf("events = %d, data bytes = %d, want one event of 128 bytes", len(events), len(events[0].Data))
	}
}

func TestEventsReportsReadErrorAfterDeliveredEvents(t *testing.T) {
	t.Parallel()

	failure := errors.New("connection reset")
	reader := io.MultiReader(strings.NewReader("data: 1\n\n"), iotest.ErrReader(failure))

	events, err := collect(t, reader, testLimits)
	if !errors.Is(err, failure) {
		t.Fatalf("Events() error = %v, want wrapped read error", err)
	}

	if len(events) != 1 || events[0].Data != "1" {
		t.Fatalf("events = %#v, want the event before the failure", events)
	}
}

func TestEventsRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	for _, limits := range []sse.Limits{{}, {MaxEventBytes: 1, MaxTypeBytes: 1}, {MaxEventBytes: -1, MaxTypeBytes: 1, MaxDataLines: 1}} {
		events, err := collect(t, strings.NewReader("data: x\n\n"), limits)
		if err == nil || errors.Is(err, sse.ErrEventTooLarge) || len(events) != 0 || !strings.HasPrefix(err.Error(), "sse: ") {
			t.Fatalf("Events(%+v) = (%v, %v), want invalid limits error", limits, events, err)
		}
	}
}
