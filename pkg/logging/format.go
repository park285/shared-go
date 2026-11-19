package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

const formatJSON = "json"

func parseLogFormat(format string) error {
	switch format {
	case "", formatJSON:
		return nil
	default:
		return fmt.Errorf("invalid log format %q: only %q is supported", format, formatJSON)
	}
}

func newFormatHandler(level slog.Level, w io.Writer, privacy privacyKeys) slog.Handler {
	handler := newSanitizeHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: replaceBuiltinAttr,
	}))

	handler.privacy = privacy

	return handler
}

func replaceBuiltinAttr(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attr
	}

	switch attr.Key {
	case slog.LevelKey:
		return stringifyLevel(attr)
	case slog.SourceKey:
		return shortenSource(groups, attr)
	default:
		return attr
	}
}

func stringifyLevel(attr slog.Attr) slog.Attr {
	level, ok := attr.Value.Any().(slog.Level)
	if !ok {
		return attr
	}

	return slog.String(slog.LevelKey, level.String())
}

func shortenSource(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 || attr.Key != slog.SourceKey {
		return attr
	}

	source, ok := attr.Value.Any().(*slog.Source)
	if !ok {
		return attr
	}

	if source.File == "" && source.Line == 0 {
		return slog.Attr{}
	}

	var buf [128]byte

	out := append(buf[:0], lastPathSegments(source.File)...)

	out = append(out, ':')
	out = strconv.AppendInt(out, int64(source.Line), 10)

	return slog.String(slog.SourceKey, string(out))
}

func lastPathSegments(path string) string {
	prefix, _, found := strings.CutLast(path, "/")
	if !found {
		return path
	}

	grandparent, _, found := strings.CutLast(prefix, "/")
	if !found {
		return path
	}

	return path[len(grandparent)+1:]
}
