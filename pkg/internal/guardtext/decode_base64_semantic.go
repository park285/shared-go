package guardtext

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	semanticWindowBytes  = maxDecodedCandidateLen / 2
	minSemanticTextRunes = 4
)

type semanticDecodeResult struct {
	projected  string
	candidates []string
	status     DecodeStatus
}

func mergeSemanticCandidates(semantic []string, decoded DecodeResult) DecodeResult {
	merged := DecodeResult{
		Candidates:       make([]string, 0, min(maxDecodeCandidates, len(semantic)+len(decoded.Candidates))),
		Status:           decoded.Status,
		standaloneBase64: decoded.standaloneBase64,
		maxDepth:         decoded.maxDepth,
	}
	total := 0

	for _, candidates := range [][]string{semantic, decoded.Candidates} {
		for _, candidate := range candidates {
			if slices.Contains(merged.Candidates, candidate) {
				continue
			}

			if len(candidate) > maxDecodedCandidateLen || total+len(candidate) > maxDecodedTotalBytes {
				merged.Status |= DecodeByteLimit

				continue
			}

			if len(merged.Candidates) >= maxDecodeCandidates {
				merged.Status |= DecodeCandidateLimit

				continue
			}

			merged.Candidates = append(merged.Candidates, candidate)

			total += len(candidate)
		}
	}

	return merged
}

func decodeSemanticRuleInput(input string, mayContribute func(string) bool) semanticDecodeResult {
	result := semanticDecodeResult{projected: input}

	var spans []encodedSpan

	for position := 0; position < len(input) && len(spans) <= maxDecodeScans; {
		start := position //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, position)

		position = match.next

		if len(match.value) < minBase64CandidateLen {
			continue
		}

		span := encodedSpan{start: start, end: match.next}
		semantic, status, recognized := decodeSemanticBase64(input, span)

		if !recognized {
			continue
		}

		if status != 0 {
			result.status |= status

			return result
		}

		candidates, candidateStatus := semanticContributionCandidates(input, span, semantic, mayContribute)
		if candidateStatus != 0 {
			result.status |= candidateStatus

			return result
		}

		for _, candidate := range candidates {
			if !appendSemanticCandidate(&result, candidate) {
				return result
			}
		}

		spans = append(spans, span)
	}

	if len(spans) > maxDecodeScans {
		result.status |= DecodeScanLimit

		return result
	}

	if len(spans) > 0 {
		result.projected = projectSemanticSpans(input, spans)
	}

	return result
}

func appendSemanticCandidate(result *semanticDecodeResult, candidate string) bool {
	if candidate == "" || slices.Contains(result.candidates, candidate) {
		return true
	}

	if len(candidate) > maxDecodedCandidateLen {
		result.status |= DecodeByteLimit

		return false
	}

	total := len(candidate)

	for _, existing := range result.candidates {
		total += len(existing)
	}

	if total > maxDecodedTotalBytes {
		result.status |= DecodeByteLimit

		return false
	}

	if len(result.candidates) >= maxDecodeCandidates {
		result.status |= DecodeCandidateLimit

		return false
	}

	result.candidates = append(result.candidates, candidate)

	return true
}

func decodeSemanticBase64(input string, span encodedSpan) (string, DecodeStatus, bool) {
	if declaredNonTextDataPayload(input, span.start) {
		decoded, err := DecodeBase64Candidate(input[span.start:span.end])
		if err != nil {
			return "", 0, false
		}

		if len(decoded) > maxProtectedDecodeBytes {
			return "", DecodeByteLimit, true
		}

		return semanticTextProjection(decoded), 0, true
	}

	return decodeCompressedSemanticText(input[span.start:span.end])
}

func decodeCompressedSemanticText(value string) (string, DecodeStatus, bool) {
	for offset := 0; offset < 4 && offset < len(value); offset++ {
		if !hasCompressedBase64Prefix(value[offset:]) {
			continue
		}

		decoded, err := DecodeBase64Candidate(value[offset:])
		if err != nil {
			continue
		}

		reader, compressed := newCompressedReader(decoded)
		if !compressed {
			continue
		}

		content, readErr := io.ReadAll(io.LimitReader(reader, maxProtectedDecodeBytes+1))
		closeErr := reader.Close()

		if readErr != nil || closeErr != nil || len(content) > maxProtectedDecodeBytes {
			return "", DecodeByteLimit, true
		}

		if !IsReadableText(content) {
			return "", DecodeByteLimit, true
		}

		return string(content), 0, true
	}

	return "", 0, false
}

func hasCompressedBase64Prefix(value string) bool {
	prefixBytes := min(len(value), 8)

	prefixBytes -= prefixBytes % 4

	if prefixBytes < 4 {
		return false
	}

	var storage [6]byte

	decoded, err := decodeBase64CandidateInto(storage[:], value[:prefixBytes])

	if err != nil || len(decoded) < 2 {
		return false
	}

	if decoded[0] == 0x1f && decoded[1] == 0x8b {
		return true
	}

	cmf, flg := decoded[0], decoded[1]

	return cmf&0x0f == 8 && cmf>>4 <= 7 && (uint16(cmf)<<8|uint16(flg))%31 == 0
}

func newCompressedReader(data []byte) (io.ReadCloser, bool) {
	if reader, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		return reader, true
	}

	if reader, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
		return reader, true
	}

	return nil, false
}

func semanticTextProjection(data []byte) string {
	var (
		projected strings.Builder
		run       strings.Builder
	)

	projected.Grow(min(len(data), maxDecodedCandidateLen))
	run.Grow(64)

	runRunes := 0
	runHasWord := false
	flushRun := func() {
		value := strings.TrimSpace(run.String())
		if runRunes >= minSemanticTextRunes && runHasWord && value != "" {
			if projected.Len() > 0 {
				projected.WriteByte(' ')
			}

			projected.WriteString(value)
		}

		run.Reset()

		runRunes = 0
		runHasWord = false
	}

	for len(data) > 0 {
		value, size := utf8.DecodeRune(data)
		if value == utf8.RuneError && size == 1 {
			flushRun()

			data = data[1:]

			continue
		}

		data = data[size:]

		if unicode.IsPrint(value) || unicode.IsSpace(value) {
			run.WriteRune(value)

			runRunes++

			runHasWord = runHasWord || unicode.IsLetter(value) || unicode.IsNumber(value)

			continue
		}

		flushRun()
	}

	flushRun()

	if projected.Len() == 0 {
		return " "
	}

	return projected.String()
}

func semanticContributionCandidates(
	input string,
	span encodedSpan,
	semantic string,
	mayContribute func(string) bool,
) ([]string, DecodeStatus) {
	windows, status := semanticWindows(semantic)
	if status != 0 {
		return nil, status
	}

	candidates := make([]string, 0, min(len(windows), maxDecodeCandidates))
	for index, window := range windows {
		windowCandidates, windowStatus := semanticWindowCandidates(window, mayContribute)
		if windowStatus != 0 {
			return candidates, windowStatus
		}

		for _, candidate := range windowCandidates {
			candidates = appendUniqueString(candidates, candidate)

			contextual := semanticContextCandidate(input, span, candidate, index == 0, index == len(windows)-1)

			if contextual != candidate && mayContribute != nil && mayContribute(contextual) {
				candidates = appendUniqueString(candidates, contextual)
			}

			if len(candidates) > maxDecodeCandidates {
				return candidates[:maxDecodeCandidates], DecodeCandidateLimit
			}
		}
	}

	return candidates, 0
}

func semanticWindowCandidates(window string, mayContribute func(string) bool) ([]string, DecodeStatus) {
	if mayContribute == nil {
		return []string{window}, 0
	}

	candidates := make([]string, 0, 1)

	if mayContribute(window) {
		candidates = append(candidates, window)
	}

	nested := DecodeCandidates(window)
	if !nested.Complete() {
		return nil, nested.Status
	}

	for _, candidate := range nested.Candidates {
		if mayContribute(candidate) {
			candidates = appendUniqueString(candidates, candidate)
		}
	}

	if shortResult, ok := decodeSingleShortRuleContext(window, mayContribute); ok {
		if !shortResult.Complete() {
			return candidates, shortResult.Status
		}

		for _, candidate := range shortResult.Candidates {
			candidates = appendUniqueString(candidates, candidate)
		}
	}

	return candidates, 0
}

func semanticWindows(input string) ([]string, DecodeStatus) {
	if len(input) <= semanticWindowBytes {
		return []string{input}, 0
	}

	windows := make([]string, 0, min(maxDecodeScans, len(input)/semanticWindowBytes+1))
	start := 0

	for start < len(input) {
		if len(windows) >= maxDecodeScans {
			return windows, DecodeScanLimit
		}

		end := min(len(input), start+semanticWindowBytes)
		for end > start && end < len(input) && !utf8.RuneStart(input[end]) {
			end--
		}

		if end <= start {
			return windows, DecodeByteLimit
		}

		windows = append(windows, input[start:end])
		if end == len(input) {
			break
		}

		next := moveRuneStart(input, end, oversizedTransformContextRunes)
		if next <= start {
			next = end
		}

		start = next
	}

	return windows, 0
}

func semanticContextCandidate(input string, span encodedSpan, replacement string, includePrefix, includeSuffix bool) string {
	start := span.start
	end := span.end

	if includePrefix {
		start = moveRuneStart(input, span.start, oversizedTransformContextRunes)
	}

	if includeSuffix {
		end = moveRuneEnd(input, span.end, oversizedTransformContextRunes)
	}

	var contextual strings.Builder

	contextual.Grow(end - start - (span.end - span.start) + len(replacement))
	contextual.WriteString(input[start:span.start])
	contextual.WriteString(replacement)
	contextual.WriteString(input[span.end:end])

	return contextual.String()
}

func appendUniqueString(values []string, candidate string) []string {
	if candidate == "" || slices.Contains(values, candidate) {
		return values
	}

	return append(values, candidate)
}

func projectSemanticSpans(input string, spans []encodedSpan) string {
	var projected strings.Builder

	projected.Grow(len(input))

	position := 0

	for _, span := range spans {
		projected.WriteString(input[position:span.start])
		projected.WriteByte(' ')

		position = span.end
	}

	projected.WriteString(input[position:])

	return projected.String()
}

func declaredNonTextDataPayload(input string, payloadStart int) bool {
	if payloadStart <= 0 || input[payloadStart-1] != ',' {
		return false
	}

	windowStart := max(0, payloadStart-256)
	metadata := strings.ToLower(input[windowStart : payloadStart-1])
	_, dataMetadata, found := strings.CutLast(metadata, "data:")

	if !found {
		return false
	}

	parts := strings.Split(dataMetadata, ";")
	if len(parts) < 2 || !containsFoldedValue(parts[1:], "base64") {
		return false
	}

	return isNonTextMediaType(strings.TrimSpace(parts[0]))
}

func containsFoldedValue(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}

	return false
}

func isNonTextMediaType(mediaType string) bool {
	if mediaType == "" || strings.HasPrefix(mediaType, "text/") {
		return false
	}

	if strings.Contains(mediaType, "json") || strings.Contains(mediaType, "xml") ||
		strings.Contains(mediaType, "javascript") || strings.Contains(mediaType, "ecmascript") ||
		strings.Contains(mediaType, "yaml") || strings.Contains(mediaType, "toml") ||
		mediaType == "application/x-www-form-urlencoded" || mediaType == "image/svg+xml" {
		return false
	}

	return true
}
