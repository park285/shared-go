package promptguard

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxBase64Candidates   = 8
	minBase64CandidateLen = 20
	maxBase64CandidateLen = 8192
	maxDecodedTotalBytes  = 16384
)

// decodedBase64Segments는 입력에 박힌 base64 후보를 디코드해 plain 세그먼트로 돌려,
// 인코딩으로 룰 매칭을 우회하는 페이로드를 원문과 동일한 가중치로 평가하게 한다.
// 후보 수·크기에 상한을 두고 1단계만 디코드한다(디코딩 결과 안의 base64는 다시 풀지 않음).
func decodedBase64Segments(input string) []textSegment {
	var segments []textSegment

	decodedTotal := 0

	for i := 0; i < len(input) && len(segments) < maxBase64Candidates; {
		match := nextBase64Candidate(input, i)

		i = match.next
		if len(match.value) < minBase64CandidateLen || len(match.value) > maxBase64CandidateLen {
			continue
		}

		decoded, err := decodeBase64Candidate(match.value)
		if err != nil || !isReadableText(decoded) {
			continue
		}

		decodedTotal += len(decoded)
		if decodedTotal > maxDecodedTotalBytes {
			break
		}

		segments = append(segments, textSegment{
			Kind:  segmentPlain,
			Views: normalizeViews(string(decoded)),
		})
	}

	return segments
}

func candidateBase64Encodings(s string) []*base64.Encoding {
	hasPadding := strings.ContainsRune(s, '=')
	hasURLAlphabet := strings.ContainsAny(s, "-_")
	hasStdAlphabet := strings.ContainsAny(s, "+/")

	if hasPadding {
		if hasURLAlphabet && !hasStdAlphabet {
			return []*base64.Encoding{
				base64.URLEncoding.Strict(),
				base64.StdEncoding.Strict(),
			}
		}

		return []*base64.Encoding{
			base64.StdEncoding.Strict(),
			base64.URLEncoding.Strict(),
		}
	}

	if hasURLAlphabet && !hasStdAlphabet {
		return []*base64.Encoding{
			base64.RawURLEncoding.Strict(),
			base64.RawStdEncoding.Strict(),
			base64.URLEncoding.Strict(),
			base64.StdEncoding.Strict(),
		}
	}

	return []*base64.Encoding{
		base64.RawStdEncoding.Strict(),
		base64.StdEncoding.Strict(),
		base64.RawURLEncoding.Strict(),
		base64.URLEncoding.Strict(),
	}
}

func isReadableText(data []byte) bool {
	n := len(data)
	if n == 0 {
		return false
	}

	printableCount := 0
	totalChars := 0

	i := 0
	for i < n {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return false
		}

		i += size
		totalChars++

		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printableCount++
		}
	}

	return totalChars > 0 && printableCount*100 > totalChars*90
}
