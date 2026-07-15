package logging

import "log/slog"

type sanitizeHandler struct {
	inner slog.Handler
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}
