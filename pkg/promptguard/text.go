package promptguard

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mtibben/confusables"
	"github.com/ymw0407/jamo/pkg/jamo"
	"golang.org/x/text/unicode/norm"
)

type Views struct {
	Raw    string
	Norm   string
	Joined string
}

type base64Candidate struct {
	value string
	next  int
}

var jamoTable = &unicode.RangeTable{
	R16: []unicode.Range16{
		// Hangul Compatibility Jamo(U+3130–U+318F)만 허용하여 공격 표면을 축소한다.
		// 나머지 자모 블록(U+1100, U+A960, U+D7B0)은 실제 사용자 입력에서 등장하지 않으므로 제거.
		{Lo: 0x3130, Hi: 0x318F, Stride: 1},
	},
}

var hangulTable = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0xAC00, Hi: 0xD7A3, Stride: 1},
	},
}

var normalizeFastPathASCII = buildNormalizeFastPathASCII()

func normalizeViews(text string) Views {
	raw := sanitizeUTF8(text)

	raw = composeJamoSequences(raw)

	normalized := normalizeCore(raw)
	joined := joinShortSeparators(normalized, 4)

	return Views{
		Raw:    raw,
		Norm:   normalized,
		Joined: joined,
	}
}

func normalizeCore(text string) string {
	nfkcText := norm.NFKC.String(text)
	normalized := normalizeWithKoreanPreserved(nfkcText)

	return normalizePostProcess(normalized)
}

func normalizeWithKoreanPreserved(text string) string {
	var (
		result          strings.Builder
		nonKoreanBuffer strings.Builder
	)

	result.Grow(len(text))

	flushNonKorean := func() {
		if nonKoreanBuffer.Len() == 0 {
			return
		}

		run := nonKoreanBuffer.String()
		if canSkipNonKoreanNormalize(run) {
			result.WriteString(run)
			nonKoreanBuffer.Reset()

			return
		}

		skeleton := confusables.Skeleton(skeletonMGuard.Replace(run))
		result.WriteString(skeletonMRestore.Replace(norm.NFKC.String(skeleton)))
		nonKoreanBuffer.Reset()
	}

	for _, r := range text {
		if unicode.Is(hangulTable, r) || unicode.Is(jamoTable, r) {
			flushNonKorean()
			result.WriteRune(r)

			continue
		}

		nonKoreanBuffer.WriteRune(r)
	}

	flushNonKorean()

	return result.String()
}

func buildNormalizeFastPathASCII() [utf8.RuneSelf]bool {
	var allowed [utf8.RuneSelf]bool
	for r := range utf8.RuneSelf {
		allowed[r] = isNormalizeFastPathRune(rune(r))
	}

	return allowed
}

func isNormalizeFastPathRune(r rune) bool {
	if unicode.IsMark(r) {
		return false
	}

	s := string(r)

	return confusables.Skeleton(s) == s && norm.NFKC.String(s) == s
}

func canSkipNonKoreanNormalize(text string) bool {
	for _, r := range text {
		if r >= utf8.RuneSelf || !normalizeFastPathASCII[r] {
			return false
		}
	}

	return true
}

func normalizePostProcess(text string) string {
	var builder strings.Builder

	builder.Grow(len(text))

	lastSpace := false
	for _, r := range text {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r) {
			continue
		}

		r = unicode.ToLower(r)
		if unicode.IsSpace(r) || unicode.Is(unicode.Z, r) {
			if !lastSpace {
				builder.WriteByte(' ')

				lastSpace = true
			}

			continue
		}

		builder.WriteRune(r)

		lastSpace = false
	}

	return strings.TrimSpace(builder.String())
}

func stripControlChars(text string) string {
	var builder strings.Builder

	builder.Grow(len(text))

	for _, r := range text {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r) {
			continue
		}

		builder.WriteRune(r)
	}

	return builder.String()
}

func collapseWhitespace(text string) string {
	var builder strings.Builder

	builder.Grow(len(text))

	lastSpace := false

	for _, r := range text {
		if unicode.IsSpace(r) || unicode.Is(unicode.Z, r) {
			if !lastSpace {
				builder.WriteByte(' ')

				lastSpace = true
			}

			continue
		}

		builder.WriteRune(r)

		lastSpace = false
	}

	return builder.String()
}

func sanitizeUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}

	var builder strings.Builder

	builder.Grow(len(text))

	for text != "" {
		r, size := utf8.DecodeRuneInString(text)
		builder.WriteRune(r)

		text = text[size:]
	}

	return builder.String()
}

func joinShortSeparators(text string, maxRun int) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}

	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		next, joined := consumeSeparatorRun(runes, i, maxRun, out)
		if joined {
			i = next
			continue
		}

		if next == i {
			out = append(out, runes[i])
			i++

			continue
		}

		appendSingleSpace(&out)

		i = next
	}

	return strings.TrimSpace(string(out))
}

func consumeSeparatorRun(runes []rune, start, maxRun int, out []rune) (int, bool) {
	if !isJoinSeparator(runes[start]) {
		return start, false
	}

	end := nextSeparatorRunEnd(runes, start)
	if shouldJoinSeparatorRun(runes, start, end, maxRun) {
		return end, true
	}

	if len(out) == 0 || out[len(out)-1] != ' ' {
		return end, true
	}

	return end, true
}

func nextSeparatorRunEnd(runes []rune, start int) int {
	end := start + 1
	for end < len(runes) && isJoinSeparator(runes[end]) {
		end++
	}

	return end
}

func shouldJoinSeparatorRun(runes []rune, start, end, maxRun int) bool {
	prevWord := start > 0 && isWordish(runes[start-1])
	nextWord := end < len(runes) && isWordish(runes[end])

	return prevWord && nextWord && (end-start) <= maxRun
}

func appendSingleSpace(out *[]rune) {
	if len(*out) == 0 || (*out)[len(*out)-1] != ' ' {
		*out = append(*out, ' ')
	}
}

func isJoinSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Z, r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isWordish(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
}

func composeJamoSequences(text string) string {
	var (
		result     strings.Builder
		jamoBuffer strings.Builder
	)

	result.Grow(len(text))

	flushJamo := func() {
		if jamoBuffer.Len() == 0 {
			return
		}

		jamoStr := jamoBuffer.String()

		// jamo.ComposeHangeul은 길이 1 슬라이스 입력 시 combineHangulSyllables 내부에서
		// index OOB panic을 일으킨다. safeComposeHangeul로 panic을 잡아 fallback한다.
		composed, err := safeComposeHangeul(jamoStr)
		if err == nil && len(composed) > 0 && len([]rune(composed[0])) > 0 {
			result.WriteString(composed[0])
		} else {
			result.WriteString(jamoStr)
		}

		jamoBuffer.Reset()
	}

	for _, r := range text {
		if unicode.Is(jamoTable, r) {
			jamoBuffer.WriteRune(r)

			continue
		}

		flushJamo()
		result.WriteRune(r)
	}

	flushJamo()

	return result.String()
}

func containsSuspiciousBase64(input string) bool {
	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		if len(match.value) < 20 {
			i = match.next
			continue
		}

		i = match.next

		decodedBytes, err := decodeBase64Candidate(match.value)
		if err != nil {
			continue
		}

		if isReadableText(decodedBytes) {
			return true
		}
	}

	return false
}

func nextBase64Candidate(input string, start int) base64Candidate {
	if !isBase64Char(input[start]) {
		return base64Candidate{next: start + 1}
	}

	next := consumeBase64Chars(input, start)

	next = consumeBase64Padding(input, next)

	return base64Candidate{
		value: input[start:next],
		next:  next,
	}
}

func consumeBase64Chars(input string, start int) int {
	end := start
	for end < len(input) && isBase64Char(input[end]) {
		end++
	}

	return end
}

func consumeBase64Padding(input string, start int) int {
	end := start

	paddingCount := 0
	for end < len(input) && input[end] == '=' && paddingCount < 2 {
		end++

		paddingCount++
	}

	return end
}

func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '+' || c == '/' || c == '-' || c == '_'
}

func decodeBase64Candidate(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("base64 decode: empty input")
	}

	var lastErr error

	for _, encoding := range candidateBase64Encodings(s) {
		decoded, err := encoding.DecodeString(s)
		if err == nil {
			return decoded, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("base64 decode: %w", lastErr)
}

// safeComposeHangeul은 jamo.ComposeHangeul을 호출하되, 벤더 패키지 내부
// combineHangulSyllables의 index OOB panic을 recover하여 error로 변환한다.
// 단일 자음·모음 등 길이가 부족한 입력에서 발생하는 DoS를 방어한다.
func safeComposeHangeul(s string) (result []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("safeComposeHangeul: recovered panic: %v", r)
		}
	}()

	composed, composeErr := jamo.ComposeHangeul(s)
	if composeErr != nil {
		return nil, fmt.Errorf("compose hangeul: %w", composeErr)
	}

	return composed, nil
}
