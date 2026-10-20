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
	// 혼동 문자 skeleton은 ASCII 대문자 I를 l로 접어 Instructions·API 같은 대·소문자 표기를 다른 단어로 만든다.
	// 단어 중간의 위장은 foldDisguisedCapitalI가 먼저 l로 바꾸고, 남은 대문자 I는 같은 글자의 소문자로 바꾼다.
	// 1·|·다른 문자의 I 모양 혼동만 l로 접는다.
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

// NormalizeView는 NormalizeViews(text).Norm과 같은 값을 Joined 없이 만든다.
func NormalizeView(text string) string {
	return Normalize(ComposeJamoSequences(SanitizeUTF8(text)))
}

func Normalize(text string) string {
	return normalizeWithSpacePolicy(text, true)
}

// NormalizeFragment는 연결할 조각의 앞뒤 공백을 보존하고 나머지 정규화 계약은 Normalize와 공유한다.
func NormalizeFragment(text string) string {
	return normalizeWithSpacePolicy(text, false)
}

func normalizeWithSpacePolicy(text string, trimEdges bool) string {
	// 빠른 경로의 글자별 대응표는 앞뒤 문맥을 보지 못하므로 위장 I를 먼저 바꾼다.
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

// foldDisguisedCapitalI는 ASCII 소문자 바로 뒤의 대문자 I를 l로 읽는다. 단어 중간의 대문자 I는 l을
// 흉내 낸 위장(poIicy, reveaI, aII)이거나 camelCase의 새 단어 시작이다. 규칙 핵심어 중 i로 시작하는
// ignore·instructions·internal이 될 수 있는 In·Ig 시작(previousInstructions, pleaseIgnore)만 i로 남긴다.
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

			// 바꾼 l은 소문자이므로 aII처럼 이어진 I도 같은 위장으로 읽는다.
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

// NormalizeEncodingSyntax는 Base64 대소문자를 보존하면서 호환 문법과 인코딩 경계의 format/control 문자를 정규화한다.
// 줄바꿈은 줄 단위로 감싼 Base64만 잇는다. 설정·로그 행까지 이으면 무관한 값들이 한 디코딩
// 영역이 되어 해석 조합이 행 수에 지수적으로 늘고, 정상 입력이 작업 예산 소진으로 미완료가 된다.
// JSON 문자열의 줄바꿈·탭 이스케이프는 실제 문자로 풀어 같은 행 규칙을 적용한다. 읽을 수 있는 텍스트로
// 풀리는 표지 없는 hex 토큰에는 hex: 표지를 붙인다.
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

// decodeJSONWhitespaceEscapes는 JSON 문자열에 붙여 넣은 로그·설정의 \n·\r·\t를 실제 문자로 바꾼다.
// 이스케이프한 역슬래시(\\)는 그대로 둔다.
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

// wrapsBase64Line은 앞 줄이 패딩 없이 Base64 문자로 끝나고 다음 줄 전체가 Base64 행일 때만 참이다.
// 패딩은 인코딩의 끝이므로 그 뒤의 줄은 같은 값의 연속이 아니다. 긴 hex 숫자 줄은 앞 줄도 그런 줄일
// 때만 잇는다. 다른 줄에 붙이면 단어와 섞여 hex 토큰으로 읽히지 않고, 숫자와 a-f만인 Base64 행은
// 지시 문장을 만들지 못한다.
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
