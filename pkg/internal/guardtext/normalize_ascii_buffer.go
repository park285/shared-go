package guardtext

import "unicode/utf8"

// NormalizeASCIIInto는 ASCII 입력을 Normalize와 같은 형태로 destination에 기록한다.
func NormalizeASCIIInto(destination, text []byte) ([]byte, bool) {
	output := destination[:0]
	pendingSpace := false
	for _, value := range text {
		if value >= utf8.RuneSelf {
			return nil, false
		}
		replacement := normalizeASCIIReplacement[value]
		if replacement == "" {
			continue
		}
		if replacement == " " {
			pendingSpace = len(output) > 0
			continue
		}
		if pendingSpace {
			output = append(output, ' ')
			pendingSpace = false
		}
		output = append(output, replacement...)
	}
	return output, true
}
