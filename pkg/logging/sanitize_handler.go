package logging

import "log/slog"

type sanitizeHandler struct {
	inner         slog.Handler
	inMaskedGroup bool
	privacy       privacyKeys
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}
