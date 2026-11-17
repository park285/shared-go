package logging

import "log/slog"

type sanitizeHandler struct {
	inner         slog.Handler
	inMaskedGroup bool
	privacy       privacyKeys
	groupDepth    int
	blockedGroup  string
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}
