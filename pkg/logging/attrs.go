package logging

import (
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

	coded, retryable := probeErrorInterfaces(err)
	if coded != nil && coded.Code() != "" {
		attrs = append(attrs, slog.String("error_code", coded.Code()))
	}
	if retryable != nil {
		attrs = append(attrs, slog.Bool("retryable", retryable.Retryable()))
	}

	return attrs
}

type codedError interface{ Code() string }

type retryableError interface{ Retryable() bool }

// probeErrorInterfaces는 errors.As 두 번(각각 full chain walk) 대신 unwrap chain을
// 한 번만 순회하며 두 인터페이스를 동시에 탐지한다. Unwrap() error와 Unwrap() []error
// 양쪽을 처리해 errors.As와 동일한 분기 의미를 유지한다.
func probeErrorInterfaces(err error) (codedError, retryableError) {
	var p errorInterfaceProbe
	p.walk(err)
	return p.coded, p.retryable
}

type errorInterfaceProbe struct {
	coded     codedError
	retryable retryableError
}

func (p *errorInterfaceProbe) done() bool {
	return p.coded != nil && p.retryable != nil
}

func (p *errorInterfaceProbe) inspect(e error) {
	if p.coded == nil {
		if c, ok := e.(codedError); ok {
			p.coded = c
		}
	}
	if p.retryable == nil {
		if r, ok := e.(retryableError); ok {
			p.retryable = r
		}
	}
}

func (p *errorInterfaceProbe) walk(e error) {
	for e != nil {
		p.inspect(e)
		if p.done() {
			return
		}

		switch x := e.(type) { //nolint:errorlint // unwrap 트리 직접 순회가 이 함수의 목적 (errors.As 대체)
		case interface{ Unwrap() error }:
			e = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				p.walk(sub)
				if p.done() {
					return
				}
			}
			return
		default:
			return
		}
	}
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
