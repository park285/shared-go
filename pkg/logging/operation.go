package logging

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type OperationOptions struct {
	Name         string
	IDPrefix     string
	Runtime      string
	Component    string
	StartEvent   string
	SuccessEvent string
	FailureEvent string
	SkipStartLog bool
	// Level은 started/succeeded 로그 레벨이다. failed는 항상 Error로 기록한다.
	Level slog.Level
	Attrs []slog.Attr
}

func RunOperation(ctx context.Context, logger *slog.Logger, opts OperationOptions, fn func(context.Context) error) error {
	ctx = operationContext(ctx, opts)
	name := operationName(opts.Name)
	baseAttrs := operationAttrs(name, opts.Attrs)

	start := time.Now()
	if !opts.SkipStartLog {
		logWith(ctx, logger, opts.Level, eventOrDefault(opts.StartEvent, name+".started"), "operation started", callerSkipViaHelper, baseAttrs, nil)
	}

	err := fn(ctx)
	attrs := append([]slog.Attr{}, baseAttrs...)
	attrs = append(attrs, SinceMS(start))

	if err != nil {
		attrs = append(attrs, ErrorAttrs(err)...)
		logWith(ctx, logger, slog.LevelError, eventOrDefault(opts.FailureEvent, name+".failed"), "operation failed", callerSkipViaHelper, attrs, nil)
		return err
	}

	logWith(ctx, logger, opts.Level, eventOrDefault(opts.SuccessEvent, name+".succeeded"), "operation succeeded", callerSkipViaHelper, attrs, nil)
	return nil
}

func operationContext(ctx context.Context, opts OperationOptions) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = operationContextWithJobID(ctx, opts)
	return operationContextWithRuntime(ctx, opts)
}

func operationContextWithJobID(ctx context.Context, opts OperationOptions) context.Context {
	if jobIDFromContext(ctx) != "" {
		return ctx
	}
	prefix := strings.TrimSpace(opts.IDPrefix)
	if prefix == "" {
		prefix = operationName(opts.Name)
	}
	return WithJobID(ctx, newID(prefix))
}

func operationContextWithRuntime(ctx context.Context, opts OperationOptions) context.Context {
	if opts.Runtime != "" {
		ctx = WithRuntime(ctx, opts.Runtime)
	}
	if opts.Component != "" {
		ctx = WithComponent(ctx, opts.Component)
	}
	return ctx
}

func operationName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "operation"
	}
	return name
}

func operationAttrs(name string, attrs []slog.Attr) []slog.Attr {
	baseAttrs := make([]slog.Attr, 0, 1+len(attrs))
	baseAttrs = append(baseAttrs, Operation(name))
	return append(baseAttrs, attrs...)
}

func eventOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
