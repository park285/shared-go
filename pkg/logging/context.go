package logging

import (
	"context"
	"log/slog"
	"strings"
)

type contextKey string

const (
	requestIDContextKey contextKey = "hololive.logging.request_id"
	jobIDContextKey     contextKey = "hololive.logging.job_id"
	runtimeContextKey   contextKey = "hololive.logging.runtime"
	componentContextKey contextKey = "hololive.logging.component"
)

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return withString(ctx, requestIDContextKey, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	return stringFromContext(ctx, requestIDContextKey)
}

func WithJobID(ctx context.Context, jobID string) context.Context {
	return withString(ctx, jobIDContextKey, jobID)
}

func JobIDFromContext(ctx context.Context) string {
	return stringFromContext(ctx, jobIDContextKey)
}

func WithRuntime(ctx context.Context, runtime string) context.Context {
	return withString(ctx, runtimeContextKey, runtime)
}

func RuntimeFromContext(ctx context.Context) string {
	return stringFromContext(ctx, runtimeContextKey)
}

func WithComponent(ctx context.Context, component string) context.Context {
	return withString(ctx, componentContextKey, component)
}

func ComponentFromContext(ctx context.Context) string {
	return stringFromContext(ctx, componentContextKey)
}

func ContextAttrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}

	attrs := make([]slog.Attr, 0, 4)
	if value := RuntimeFromContext(ctx); value != "" {
		attrs = append(attrs, Runtime(value))
	}
	if value := ComponentFromContext(ctx); value != "" {
		attrs = append(attrs, Component(value))
	}
	if value := RequestIDFromContext(ctx); value != "" {
		attrs = append(attrs, RequestID(value))
	}
	if value := JobIDFromContext(ctx); value != "" {
		attrs = append(attrs, JobID(value))
	}

	return attrs
}

func withString(ctx context.Context, key contextKey, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
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
	value, _ := ctx.Value(key).(string)
	return strings.TrimSpace(value)
}
