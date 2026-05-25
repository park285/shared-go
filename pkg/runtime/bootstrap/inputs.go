package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
)

func NormalizeRuntimeBuildInputs(ctx context.Context, appConfig any, logger *slog.Logger) (context.Context, error) {
	if isNilValue(appConfig) {
		return nil, errors.New("config must not be nil")
	}
	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return ctx, nil
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}
