package promptguard

import (
	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

func normalizeViews(text string) guardtext.Views {
	return guardtext.NormalizeViews(text)
}

func normalizeText(text string) string {
	return guardtext.Normalize(text)
}

func normalizeASCIIByteReplacement(value byte) (string, bool) {
	return guardtext.NormalizeASCIIByteReplacement(value)
}

func sanitizeUTF8(text string) string {
	return guardtext.SanitizeUTF8(text)
}
