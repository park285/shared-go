package envutil

import (
	"errors"
	"fmt"
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

func IntE(key string, def int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid int env %s (%w)", key, strictParseCause(err))
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
		return 0, fmt.Errorf("invalid int64 env %s (%w)", key, strictParseCause(err))
	}

	return parsed, nil
}

func BoolE(key string, def bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}

	out, err := parseBoolE(key, value)
	if err != nil {
		return out, fmt.Errorf("parse bool e: %w", err)
	}

	return out, nil
}

func FloatE(key string, def float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid float64 env %s (%w)", key, strictParseCause(err))
	}

	return parsed, nil
}

// BoolExplicit은 값과 함께 "명시적으로 설정되었는지"를 반환한다. 미설정과 공백-only는
// 모두 explicit=false로 접어 unset과 동일하게 다룬다(String/BoolE의 trim 규칙과 일치).
func BoolExplicit(key string) (value, explicit bool, err error) {
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
		return false, true, fmt.Errorf("invalid bool env %s (%w)", key, strconv.ErrSyntax)
	}

	return parsed, true, nil
}

func DurationE(key string, def time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration env %s (%w)", key, strictParseCause(err))
	}

	return parsed, nil
}

// lookupBool은 BoolE/BoolExplicit(과 BoolE를 쓰는 LoadDotenv 플래그)이 공유하는 유일한 bool 수용 집합이다.
// 미수용 값은 모두 에러로 돌려주며 기본값으로 접는 경로는 없다.
func lookupBool(value string) (parsed, ok bool) {
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
		return false, fmt.Errorf("invalid bool env %s (%w)", key, strconv.ErrSyntax)
	}

	return parsed, nil
}

func strictParseCause(err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return strconv.ErrRange
	}

	return strconv.ErrSyntax
}

func List(key string) ([]string, error) {
	raw, err := StringOrSecretFile(key, "")
	if err != nil {
		return nil, fmt.Errorf("read list env %s: %w", key, err)
	}

	if raw == "" {
		return nil, nil
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

	return out, nil
}

func Map(key string) (map[string]string, error) {
	raw, err := StringOrSecretFile(key, "")
	if err != nil {
		return nil, fmt.Errorf("read map env %s: %w", key, err)
	}

	if raw == "" {
		return nil, nil //nolint:nilnil // 설정되지 않은 map의 canonical 값은 nil이며 오류가 아니다.
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	})

	out := make(map[string]string, len(parts))
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		idx := strings.IndexAny(part, ":=")
		if idx <= 0 || idx >= len(part)-1 {
			return nil, fmt.Errorf("invalid map env %s entry #%d", key, index+1)
		}

		entryKey := strings.TrimSpace(part[:idx])

		value := strings.TrimSpace(part[idx+1:])
		if entryKey == "" || value == "" {
			return nil, fmt.Errorf("invalid map env %s entry #%d", key, index+1)
		}

		if _, exists := out[entryKey]; exists {
			return nil, fmt.Errorf("duplicate map env %s key at entry #%d", key, index+1)
		}

		out[entryKey] = value
	}

	if len(out) == 0 {
		return nil, nil //nolint:nilnil // 구분자-only map도 설정되지 않은 map과 같은 canonical 값이다.
	}

	return out, nil
}
