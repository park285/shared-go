package logging

import "log/slog"

type sanitizeHandler struct {
	inner          slog.Handler
	inPrivacyGroup bool
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}
