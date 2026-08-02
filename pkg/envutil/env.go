package envutil

import (
	"errors"
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
	boolYes   = "yes"
	boolOn    = "on"
	boolOff   = "off"
)

func warnParse(key, value, kind string, err error, def any) {
	attrs := []any{
		"key", key,
		"value_present", value != "",
		"kind", kind,
		"returning_default", def,
	}
	if err != nil {
		attrs = append(attrs, "error", parseErrorKind(err))
	}
	slog.Warn("invalid value for environment variable", attrs...)
}

func parseErrorKind(err error) string {
	var numErr *strconv.NumError
	if errors.As(err, &numErr) {
		switch {
		case errors.Is(numErr.Err, strconv.ErrSyntax):
			return "invalid_syntax"
		case errors.Is(numErr.Err, strconv.ErrRange):
			return "out_of_range"
		}
	}
	return "parse_failed"
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

func IntE(key string, def int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, strictParseError(key, "int", err)
	}
	return parsed, nil
}

func Int64E(key string, def int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, strictParseError(key, "int64", err)
	}
	return parsed, nil
}

func Bool(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, ok := lookupBool(value)
	if !ok {
		warnParse(key, value, "bool", nil, def)
		return def
	}
	return parsed
}

func BoolE(key string, def bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	return parseBoolE(key, value)
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

func FloatE(key string, def float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, strictParseError(key, "float64", err)
	}
	return parsed, nil
}

// BoolExplicit은 값과 함께 "명시적으로 설정되었는지"를 반환한다. 미설정과 공백-only는
// 모두 explicit=false로 접어 unset과 동일하게 다룬다(String/Bool의 trim 규칙과 일치).
func BoolExplicit(key string) (value bool, explicit bool, err error) {
	raw, found := os.LookupEnv(key)
	if !found {
		return false, false, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, false, nil
	}
	parsed, ok := lookupBool(trimmed)
	if !ok {
		return false, true, strictParseError(key, "bool", strconv.ErrSyntax)
	}
	return parsed, true, nil
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

func DurationE(key string, def time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, strictParseError(key, "duration", err)
	}
	return parsed, nil
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

// Deprecated: iris-stack 소비자가 없습니다. 단일 키는 IntE를 쓰고, 다중 키 폴백이
// 필요하면 호출부에서 StringAny로 키를 고른 뒤 파싱하십시오.
func IntAnyE(keys []string, def int) (int, error) {
	key, value, ok := firstEnvValue(keys)
	if !ok {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, strictParseError(key, "int", err)
	}
	return parsed, nil
}

// Deprecated: iris-stack 소비자가 없습니다. 단일 키는 Int64E를 쓰고, 다중 키 폴백이
// 필요하면 호출부에서 StringAny로 키를 고른 뒤 파싱하십시오.
func Int64AnyE(keys []string, def int64) (int64, error) {
	key, value, ok := firstEnvValue(keys)
	if !ok {
		return def, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, strictParseError(key, "int64", err)
	}
	return parsed, nil
}

// Deprecated: iris-stack 소비자가 없습니다. 단일 키는 BoolE를 쓰고, 다중 키 폴백이
// 필요하면 호출부에서 StringAny로 키를 고른 뒤 파싱하십시오.
func BoolAnyE(keys []string, def bool) (bool, error) {
	key, value, ok := firstEnvValue(keys)
	if !ok {
		return def, nil
	}
	return parseBoolE(key, value)
}

func firstEnvValue(keys []string) (string, string, bool) {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return key, value, true
		}
	}
	return "", "", false
}

// lookupBool은 Bool/BoolE/BoolExplicit/dotenv 로더가 공유하는 유일한 bool 수용 집합이다.
// strict 변형은 미수용 값에 대한 반환(기본값 vs 에러)만 다르고 수용 집합은 같다.
func lookupBool(value string) (parsed bool, ok bool) {
	switch strings.ToLower(value) {
	case "1", boolTrue, boolYes, "y", boolOn:
		return true, true
	case "0", boolFalse, "no", "n", boolOff:
		return false, true
	default:
		return false, false
	}
}

func parseBoolE(key, value string) (bool, error) {
	parsed, ok := lookupBool(value)
	if !ok {
		return false, strictParseError(key, "bool", strconv.ErrSyntax)
	}
	return parsed, nil
}

func strictParseError(key, kind string, err error) error {
	cause := strconv.ErrSyntax
	if errors.Is(err, strconv.ErrRange) {
		cause = strconv.ErrRange
	}
	return fmt.Errorf("invalid %s env %s (%w)", kind, key, cause)
}

func List(key string) []string {
	return listWithFallback(key, "")
}

// Deprecated: iris-stack 소비자가 없습니다. List를 쓰고 빈 결과에 대한 기본값은
// 호출부에서 처리하십시오.
func ListWithFallback(key, fallback string) []string {
	return listWithFallback(key, fallback)
}

func listWithFallback(key, fallback string) []string {
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
