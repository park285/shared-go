package guardtext

import "unicode/utf8"

func NormalizeASCIIByteReplacement(value byte) (string, bool) {
	if value >= utf8.RuneSelf {
		return "", false
	}
	return normalizeASCIIReplacement[value], true
}
