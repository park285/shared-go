package stringutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const truncatedHashHexChars = 32

func TruncatedHash(input string) string {
	return hash(input)[:truncatedHashHexChars]
}

func logHash(input string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if normalized == "" {
		return ""
	}

	return hash(normalized)
}

func TruncatedLogHash(input string) string {
	value := logHash(input)
	if value == "" {
		return ""
	}

	return value[:truncatedHashHexChars]
}

func hash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
