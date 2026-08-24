package guardtext

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ymw0407/jamo/pkg/jamo"
	"golang.org/x/text/unicode/norm"
)

type Views struct {
	Raw    string
	Norm   string
	Joined string
}

var jamoTable = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0x3130, Hi: 0x318F, Stride: 1}},
}

var hangulTable = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0xAC00, Hi: 0xD7A3, Stride: 1}},
}

var normalizeFastPathASCII = buildNormalizeFastPathASCII()

const (
	latinMLowerPlaceholder = "\uE000"
	latinMUpperPlaceholder = "\uE001"
)

var (
	skeletonMGuard = strings.NewReplacer(
		"m", latinMLowerPlaceholder,
		"M", latinMUpperPlaceholder,
	)
	skeletonMRestore = strings.NewReplacer(
		latinMLowerPlaceholder, "m",
		latinMUpperPlaceholder, "M",
		"ʍ", "m",
	)
	normalizeASCIIReplacement = buildNormalizeASCIIReplacement()
)

func NormalizeViews(text string) Views {
	raw := ComposeJamoSequences(SanitizeUTF8(text))
	normalized := Normalize(raw)

	return Views{
		Raw:    raw,
		Norm:   normalized,
		Joined: JoinShortSeparators(normalized, 4),
	}
}

func Normalize(text string) string {
	if normalized, ok := normalizeFastASCIIText(text); ok {
		return normalized
	}

	nfkcText := norm.NFKC.String(text)
	normalized := normalizeWithKoreanPreserved(nfkcText)

	return NormalizePostProcess(normalized)
}

// NormalizeEncodingSyntax는 Base64 대소문자를 보존하면서 호환 문법과 인코딩 경계의 format/control 문자를 정규화한다.
func NormalizeEncodingSyntax(text string) string {
	return StripControlChars(norm.NFKC.String(SanitizeUTF8(text)))
}

func EncodingSyntaxNeedsNormalization(text string) bool {
	requiresCompatibilityCheck := false

	for _, value := range text {
		if unicode.Is(unicode.Cf, value) || unicode.Is(unicode.Cc, value) {
			return true
		}

		if value >= utf8.RuneSelf && (value < 0xAC00 || value > 0xD7A3) {
			requiresCompatibilityCheck = true
		}
	}

	return requiresCompatibilityCheck && norm.NFKC.QuickSpanString(text) != len(text)
}

func normalizeFastASCIIText(text string) (string, bool) {
	needsRewrite := false
	lastSpace := false

	for i := range len(text) {
		value := text[i]
		if value >= utf8.RuneSelf {
			return "", false
		}

		replacement := normalizeASCIIReplacement[value]
		if replacement == "" {
			needsRewrite = true
			continue
		}

		if len(replacement) != 1 || replacement[0] != value {
			needsRewrite = true
		}

		if replacement == " " {
			if i == 0 || lastSpace {
				needsRewrite = true
			}

			lastSpace = true

			continue
		}

		lastSpace = false
	}

	if lastSpace {
		needsRewrite = true
	}

	if !needsRewrite {
		return text, true
	}

	var normalized strings.Builder

	normalized.Grow(len(text))

	pendingSpace := false

	for i := range len(text) {
		value := text[i]
		replacement := normalizeASCIIReplacement[value]

		if replacement == "" {
			continue
		}

		if replacement == " " {
			pendingSpace = normalized.Len() > 0
			continue
		}

		if pendingSpace {
			normalized.WriteByte(' ')

			pendingSpace = false
		}

		normalized.WriteString(replacement)
	}

	return normalized.String(), true
}

func buildNormalizeASCIIReplacement() [utf8.RuneSelf]string {
	var replacements [utf8.RuneSelf]string

	for value := range utf8.RuneSelf {
		if value == ' ' {
			replacements[value] = " "
			continue
		}

		text := string(rune(value))
		nfkcText := norm.NFKC.String(text)
		normalized := normalizeWithKoreanPreserved(nfkcText)

		replacements[value] = NormalizePostProcess(normalized)
	}

	return replacements
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

		skeleton := confusableSkeleton(skeletonMGuard.Replace(run))
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

func NormalizePostProcess(text string) string {
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

func StripControlChars(text string) string {
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

func StripFormatAndCombining(text string) string {
	needsStrip := false
	allASCII := true

	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			allASCII = false
			break
		}
	}

	if allASCII {
		return text
	}

	for _, r := range text {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) {
			needsStrip = true
			break
		}
	}

	if !needsStrip {
		return text
	}

	var builder strings.Builder

	builder.Grow(len(text))

	for _, r := range text {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) {
			continue
		}

		builder.WriteRune(r)
	}

	return builder.String()
}

func CollapseWhitespace(text string) string {
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

func SanitizeUTF8(text string) string {
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

func JoinShortSeparators(text string, maxRun int) string {
	if joined, ok := joinShortSeparatorsASCII(text, maxRun); ok {
		return joined
	}

	return joinShortSeparatorsRunes(text, maxRun)
}

func joinShortSeparatorsRunes(text string, maxRun int) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}

	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		if !isJoinSeparator(runes[i]) {
			out = append(out, runes[i])
			i++

			continue
		}

		end := i + 1
		for end < len(runes) && isJoinSeparator(runes[end]) {
			end++
		}

		if shouldJoinSeparatorRun(runes, i, end, maxRun) {
			i = end

			continue
		}

		if len(out) == 0 || out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}

		i = end
	}

	return strings.TrimSpace(string(out))
}

func joinShortSeparatorsASCII(text string, maxRun int) (string, bool) {
	hasSeparator := false

	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			return "", false
		}

		if isASCIIJoinSeparator(text[i]) {
			hasSeparator = true
		}
	}

	if !hasSeparator {
		return text, true
	}

	var joined strings.Builder

	joined.Grow(len(text))

	pendingSpace := false

	for i := 0; i < len(text); {
		if !isASCIIJoinSeparator(text[i]) {
			if pendingSpace {
				joined.WriteByte(' ')

				pendingSpace = false
			}

			joined.WriteByte(text[i])

			i++

			continue
		}

		end := i + 1
		for end < len(text) && isASCIIJoinSeparator(text[end]) {
			end++
		}

		previousWord := i > 0 && isASCIIWordish(text[i-1])
		nextWord := end < len(text) && isASCIIWordish(text[end])

		if !previousWord || !nextWord || end-i > maxRun {
			pendingSpace = joined.Len() > 0
		}

		i = end
	}

	return joined.String(), true
}

func isASCIIJoinSeparator(value byte) bool {
	return value >= '!' && value <= '/' ||
		value >= ':' && value <= '@' ||
		value >= '[' && value <= '`' ||
		value >= '{' && value <= '~' ||
		value == ' ' || value >= '\t' && value <= '\r'
}

func isASCIIWordish(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func shouldJoinSeparatorRun(runes []rune, start, end, maxRun int) bool {
	prevWord := start > 0 && isWordish(runes[start-1])
	nextWord := end < len(runes) && isWordish(runes[end])

	return prevWord && nextWord && end-start <= maxRun
}

func isJoinSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Z, r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isWordish(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
}

func ComposeJamoSequences(text string) string {
	allASCII := true

	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			allASCII = false
			break
		}
	}

	if allASCII {
		return text
	}

	hasJamo := false

	for _, r := range text {
		if unicode.Is(jamoTable, r) {
			hasJamo = true
			break
		}
	}

	if !hasJamo {
		return text
	}

	var (
		result     strings.Builder
		jamoBuffer strings.Builder
	)

	result.Grow(len(text))

	flushJamo := func() {
		if jamoBuffer.Len() == 0 {
			return
		}

		jamoText := jamoBuffer.String()
		composed, err := safeComposeHangeul(jamoText)

		if err == nil && len(composed) > 0 && len([]rune(composed[0])) > 0 {
			result.WriteString(composed[0])
		} else {
			result.WriteString(jamoText)
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

	value := string(r)

	return confusableSkeleton(value) == value && norm.NFKC.String(value) == value
}

func canSkipNonKoreanNormalize(text string) bool {
	for _, r := range text {
		if r >= utf8.RuneSelf || !normalizeFastPathASCII[r] {
			return false
		}
	}

	return true
}

func safeComposeHangeul(text string) (result []string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("safeComposeHangeul: recovered panic: %v", recovered)
		}
	}()

	composed, composeErr := jamo.ComposeHangeul(text)
	if composeErr != nil {
		return nil, fmt.Errorf("compose hangeul: %w", composeErr)
	}

	return composed, nil
}
