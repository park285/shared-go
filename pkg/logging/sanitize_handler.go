package logging

import "log/slog"

type sanitizeHandler struct {
	inner         slog.Handler
	inMaskedGroup bool
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}
