package envutil

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	boolTrue  = "true"
	boolFalse = "false"

	logKeyValue = "value"
)

func warnParse(key, value, kind string, err error, def any) {
	attrs := []any{
		"key", key,
		logKeyValue, value,
		"kind", kind,
		"returning_default", def,
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.Warn("invalid value for environment variable", attrs...)
}

func String(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

func StringRaw(key, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	return value
}

func Int(key string, def int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		warnParse(key, value, "int", err, def)
		return def
	}
	return parsed
}

func IntRaw(key string, def int) int {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		warnParse(key, value, "int", err, def)
		return def
	}
	return parsed
}

func IntNonNegative(key string, def int) int {
	value := Int(key, def)
	if value < 0 {
		return 0
	}
	return value
}

func Int64(key string, def int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		warnParse(key, value, "int64", err, def)
		return def
	}
	return parsed
}

func Bool(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	switch strings.ToLower(value) {
	case "1", boolTrue, "yes", "y", "on":
		return true
	case "0", boolFalse, "no", "n", "off":
		return false
	default:
		warnParse(key, value, "bool", nil, def)
		return def
	}
}

func BoolStrict(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	value = strings.ToLower(value)
	if value != boolTrue && value != boolFalse {
		slog.Warn("invalid boolean value for environment variable",
			"key", key,
			"value_present", true,
			"returning_default", def)
		return def
	}
	return value == boolTrue
}

func Float(key string, def float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		warnParse(key, value, "float", err, def)
		return def
	}
	return parsed
}

func Duration(key string, def time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		warnParse(key, value, "duration", err, def)
		return def
	}
	return parsed
}

func Required(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(fmt.Sprintf("required environment variable %s is not set or empty", key))
	}
	return value
}

func StringAny(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func List(key string) []string {
	return ListWithFallback(key, "")
}

func ListWithFallback(key, fallback string) []string {
	raw := StringOrFile(key, fallback)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	out := make([]string, 0, len(parts))

	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if _, ok := seen[part]; ok {
			continue
		}

		seen[part] = struct{}{}
		out = append(out, part)
	}

	return out
}

func Map(key string) map[string]string {
	raw := StringOrFile(key, "")
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	})

	out := make(map[string]string, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		idx := strings.IndexAny(part, ":=")
		if idx <= 0 || idx >= len(part)-1 {
			continue
		}

		entryKey := strings.TrimSpace(part[:idx])

		value := strings.TrimSpace(part[idx+1:])
		if entryKey == "" || value == "" {
			continue
		}

		out[entryKey] = value
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
