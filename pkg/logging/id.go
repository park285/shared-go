package logging

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

func newID(prefix string) string {
	prefix = sanitizeIDPrefix(prefix)

	random := make([]byte, 6)

	if _, err := rand.Read(random); err != nil {
		var b strings.Builder

		b.WriteString(prefix)
		b.WriteByte('_')
		b.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))

		return b.String()
	}

	var b strings.Builder

	b.WriteString(prefix)
	b.WriteByte('_')
	b.WriteString(strconv.FormatInt(time.Now().UnixMilli(), 10))
	b.WriteByte('_')
	b.WriteString(hex.EncodeToString(random))

	return b.String()
}

func sanitizeIDPrefix(prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return "id"
	}

	var b strings.Builder

	for _, r := range prefix {
		if replacement, ok := sanitizedIDPrefixRune(r); ok {
			b.WriteRune(replacement)
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "id"
	}

	return out
}

func sanitizedIDPrefixRune(r rune) (rune, bool) {
	if isIDPrefixAlphaNumeric(r) {
		return r, true
	}

	if isIDPrefixSeparator(r) {
		return '_', true
	}

	return 0, false
}

func isIDPrefixAlphaNumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func isIDPrefixSeparator(r rune) bool {
	return r == '-' || r == '_' || r == '.'
}
