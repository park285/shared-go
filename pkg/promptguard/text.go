package promptguard

import (
	"fmt"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

type Views = guardtext.Views

func normalizeViews(text string) Views {
	return guardtext.NormalizeViews(text)
}

func normalizeText(text string) string {
	return guardtext.Normalize(text)
}

func normalizeASCIIByteReplacement(value byte) (string, bool) {
	return guardtext.NormalizeASCIIByteReplacement(value)
}

func normalizePostProcess(text string) string {
	return guardtext.NormalizePostProcess(text)
}

func stripControlChars(text string) string {
	return guardtext.StripControlChars(text)
}

func collapseWhitespace(text string) string {
	return guardtext.CollapseWhitespace(text)
}

func sanitizeUTF8(text string) string {
	return guardtext.SanitizeUTF8(text)
}

func containsSuspiciousBase64(input string) bool {
	return guardtext.ContainsSuspiciousBase64(input)
}

func decodeBase64Candidate(input string) ([]byte, error) {
	out, err := guardtext.DecodeBase64Candidate(input)
	if err != nil {
		return out, fmt.Errorf("decode base64 candidate: %w", err)
	}

	return out, nil
}
