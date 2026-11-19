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
	skeletonASCIIGuard = strings.NewReplacer(
		"m", latinMLowerPlaceholder,
		"M", latinMUpperPlaceholder,
		"I", "i",
	)
	skeletonASCIIRestore = strings.NewReplacer(
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

func NormalizeView(text string) string {
	return Normalize(ComposeJamoSequences(SanitizeUTF8(text)))
}

func Normalize(text string) string {
	return normalizeWithSpacePolicy(text, true)
}

func NormalizeFragment(text string) string {
	return normalizeWithSpacePolicy(text, false)
}

func normalizeWithSpacePolicy(text string, trimEdges bool) string {
	if normalized, ok := normalizeFastASCIIText(foldDisguisedCapitalI(text), trimEdges); ok {
		return normalized
	}

	if normalized, ok := normalizeFastHangulASCIIText(text, trimEdges); ok {
		return normalized
	}

	nfkcText := norm.NFKC.String(text)
	normalized := normalizeWithKoreanPreserved(nfkcText)

	return normalizePostProcessWithEdges(normalized, trimEdges)
}

func foldDisguisedCapitalI(text string) string {
	if !strings.Contains(text, "I") {
		return text
	}

	var folded []byte

	afterLower := false

	for index := range len(text) {
		value := text[index]
		if value == 'I' && afterLower && (index+1 == len(text) || text[index+1] != 'n' && text[index+1] != 'g') {
			if folded == nil {
				folded = []byte(text)
			}

			folded[index] = 'l'

			continue
		}

		afterLower = value >= 'a' && value <= 'z'
	}

	if folded == nil {
		return text
	}

	return string(folded)
}

func NormalizeEncodingSyntax(text string) string {
	normalized := norm.NFKC.String(decodeJSONWhitespaceEscapes(SanitizeUTF8(text)))
	if !strings.Contains(normalized, "\n") {
		return markReadableBareHex(StripControlChars(normalized))
	}

	var builder strings.Builder

	builder.Grow(len(normalized))

	previous := ""

	for index, line := range strings.Split(normalized, "\n") {
		line = StripControlChars(line)
		if index > 0 && !wrapsBase64Line(previous, line) {
			builder.WriteByte('\n')
		}

		builder.WriteString(line)

		previous = line
	}

	return markReadableBareHex(builder.String())
}

func decodeJSONWhitespaceEscapes(text string) string {
	if !strings.Contains(text, `\`) {
		return text
	}

	var builder strings.Builder

	builder.Grow(len(text))

	for index := 0; index < len(text); index++ {
		if text[index] != '\\' || index+1 == len(text) {
			builder.WriteByte(text[index])

			continue
		}

		switch text[index+1] {
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		default:
			builder.WriteString(text[index : index+2])
		}

		index++
	}

	return builder.String()
}

func wrapsBase64Line(previous, line string) bool {
	if previous == "" || line == "" || !isBase64Char(previous[len(previous)-1]) {
		return false
	}

	if isBareHexLine(line) && !isBareHexLine(previous) {
		return false
	}

	end := len(line)
	for padding := 0; end > 0 && line[end-1] == '=' && padding < 2; padding++ {
		end--
	}

	for index := range end {
		if !isBase64Char(line[index]) {
			return false
		}
	}

	return end > 0
}

func normalizeFastASCIIText(text string, trimEdges bool) (string, bool) {
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
			if trimEdges && i == 0 || lastSpace {
				needsRewrite = true
			}

			lastSpace = true

			continue
		}

		lastSpace = false
	}

	if trimEdges && lastSpace {
		needsRewrite = true
	}

	if !needsRewrite {
		return text, true
	}

	return rewriteNormalizedASCII(text, trimEdges), true
}

func rewriteNormalizedASCII(text string, trimEdges bool) string {
	var normalized strings.Builder

	normalized.Grow(len(text))

	if writeNormalizedASCII(&normalized, text, trimEdges, false) && !trimEdges {
		normalized.WriteByte(' ')
	}

	return normalized.String()
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

		run := foldDisguisedCapitalI(nonKoreanBuffer.String())
		if canSkipNonKoreanNormalize(run) {
			result.WriteString(run)
			nonKoreanBuffer.Reset()

			return
		}

		skeleton := confusableSkeleton(skeletonASCIIGuard.Replace(run))
		result.WriteString(skeletonASCIIRestore.Replace(norm.NFKC.String(skeleton)))
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
	return normalizePostProcessWithEdges(text, true)
}

func normalizePostProcessWithEdges(text string, trimEdges bool) string {
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

	if trimEdges {
		return strings.TrimSpace(builder.String())
	}

	return builder.String()
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
