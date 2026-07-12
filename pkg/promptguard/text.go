package promptguard

import "github.com/park285/shared-go/pkg/internal/guardtext"

type Views = guardtext.Views

func normalizeViews(text string) Views {
	return guardtext.NormalizeViews(text)
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
	return guardtext.DecodeBase64Candidate(input)
}
