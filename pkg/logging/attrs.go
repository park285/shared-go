package logging

import (
	"errors"
	"log/slog"
	"reflect"
	"time"
)

func Event(event string) slog.Attr {
	return slog.String("event", event)
}

func Runtime(runtime string) slog.Attr {
	return slog.String("runtime", runtime)
}

func Component(component string) slog.Attr {
	return slog.String("component", component)
}

func Operation(name string) slog.Attr {
	return slog.String("operation", name)
}

func RequestID(id string) slog.Attr {
	return slog.String("request_id", id)
}

func JobID(id string) slog.Attr {
	return slog.String("job_id", id)
}

func DurationMS(d time.Duration) slog.Attr {
	return slog.Int64("duration_ms", d.Milliseconds())
}

func SinceMS(start time.Time) slog.Attr {
	return DurationMS(time.Since(start))
}

func ErrorAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}

	attrs := []slog.Attr{
		slog.String("error_type", errorType(err)),
		slog.String("error_message", err.Error()),
	}

	var coded interface{ Code() string }
	if errors.As(err, &coded) && coded.Code() != "" {
		attrs = append(attrs, slog.String("error_code", coded.Code()))
	}

	var retryable interface{ Retryable() bool }
	if errors.As(err, &retryable) {
		attrs = append(attrs, slog.Bool("retryable", retryable.Retryable()))
	}

	return attrs
}

func errorType(err error) string {
	if err == nil {
		return ""
	}

	t := reflect.TypeOf(err)
	if t == nil {
		return "error"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" {
		return t.String()
	}
	return t.Name()
}
