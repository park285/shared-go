package logging

import (
	"context"
	"log/slog"
	"strings"
)

type contextKey uint8

const (
	requestIDContextKey contextKey = iota
	jobIDContextKey
	runtimeContextKey
	componentContextKey
)

type contextValues struct {
	runtime   string
	component string
	requestID string
	jobID     string
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return withString(ctx, requestIDContextKey, requestID)
}

func requestIDFromContext(ctx context.Context) string {
	return stringFromContext(ctx, requestIDContextKey)
}

func WithJobID(ctx context.Context, jobID string) context.Context {
	return withString(ctx, jobIDContextKey, jobID)
}

func jobIDFromContext(ctx context.Context) string {
	return stringFromContext(ctx, jobIDContextKey)
}

func WithRuntime(ctx context.Context, runtime string) context.Context {
	return withString(ctx, runtimeContextKey, runtime)
}

func runtimeFromContext(ctx context.Context) string {
	return stringFromContext(ctx, runtimeContextKey)
}

func WithComponent(ctx context.Context, component string) context.Context {
	return withString(ctx, componentContextKey, component)
}

func componentFromContext(ctx context.Context) string {
	return stringFromContext(ctx, componentContextKey)
}

func ContextAttrs(ctx context.Context) []slog.Attr {
	values := contextValuesFrom(ctx)
	count := values.count()

	if count == 0 {
		return nil
	}

	return values.appendTo(make([]slog.Attr, 0, count))
}

func contextValuesFrom(ctx context.Context) contextValues {
	if ctx == nil {
		return contextValues{}
	}

	return contextValues{
		runtime:   runtimeFromContext(ctx),
		component: componentFromContext(ctx),
		requestID: requestIDFromContext(ctx),
		jobID:     jobIDFromContext(ctx),
	}
}

func (v contextValues) count() int {
	count := 0

	if v.runtime != "" {
		count++
	}

	if v.component != "" {
		count++
	}

	if v.requestID != "" {
		count++
	}

	if v.jobID != "" {
		count++
	}

	return count
}

func (v contextValues) appendTo(attrs []slog.Attr) []slog.Attr {
	if v.runtime != "" {
		attrs = append(attrs, Runtime(v.runtime))
	}

	if v.component != "" {
		attrs = append(attrs, componentAttr(v.component))
	}

	if v.requestID != "" {
		attrs = append(attrs, RequestID(v.requestID))
	}

	if v.jobID != "" {
		attrs = append(attrs, jobIDAttr(v.jobID))
	}

	return attrs
}

func (v contextValues) addToRecord(record *slog.Record) {
	if v.runtime != "" {
		record.AddAttrs(Runtime(v.runtime))
	}

	if v.component != "" {
		record.AddAttrs(componentAttr(v.component))
	}

	if v.requestID != "" {
		record.AddAttrs(RequestID(v.requestID))
	}

	if v.jobID != "" {
		record.AddAttrs(jobIDAttr(v.jobID))
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

func withString(ctx context.Context, key contextKey, value string) context.Context {
	ctx = contextOrBackground(ctx)

	value = strings.TrimSpace(value)
	if value == "" {
		return ctx
	}

	return context.WithValue(ctx, key, value)
}

func stringFromContext(ctx context.Context, key contextKey) string {
	if ctx == nil {
		return ""
	}

	value, ok := ctx.Value(key).(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}
