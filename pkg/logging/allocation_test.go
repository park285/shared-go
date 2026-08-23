//go:build !race

package logging

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestLoggingAllocationCeilings(t *testing.T) {
	logger := slog.New(newFormatHandler(slog.LevelInfo, io.Discard))
	ctx := WithRequestID(WithRuntime(context.Background(), "bot"), "req-1")

	tests := []struct {
		name      string
		maxAllocs float64
		call      func()
	}{
		{
			name:      "log common path",
			maxAllocs: 4,
			call: func() {
				Log(ctx, logger, slog.LevelInfo, "request.completed", "request completed",
					slog.String("method", "GET"), slog.Int("status", 200))
			},
		},
		{
			name:      "log and wrap error",
			maxAllocs: 11,
			call: func() {
				_ = LogAndWrapError(ctx, logger, "op", context.DeadlineExceeded, slog.String("a", "b"))
			},
		},
		{
			name:      "log warn with error attrs",
			maxAllocs: 12,
			call: func() {
				LogWarnWithErrorAttrs(ctx, logger, "event", "message", context.DeadlineExceeded, slog.String("a", "b"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := testing.AllocsPerRun(200, test.call); got > test.maxAllocs {
				t.Fatalf("%s allocs = %v, want <= %v", test.name, got, test.maxAllocs)
			}
		})
	}
}
