package guardtext

func shortRuleBase64Spans(
	input string,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	var (
		spans []encodedSpan
		seen  map[encodedSpan]struct{}
	)

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		match := nextBase64Candidate(input, i)
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.

		i = match.next

		if len(match.value) < 4 {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		if seenWholes.duplicate(input, whole) {
			continue
		}

		if pathSegments, ok := httpURLPathBase64Segments(input, whole, 4); ok {
			for _, segment := range pathSegments {
				spans, seen = appendShortRuleBase64Whole(spans, seen, input, segment, mayContribute, embeddedContextMayContribute, work, status)
				if !decodeWorkComplete(status) {
					break
				}
			}

			continue
		}

		spans, seen = appendShortRuleBase64Whole(spans, seen, input, whole, mayContribute, embeddedContextMayContribute, work, status)
	}

	return spans
}

func appendShortRuleBase64Whole(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, map[encodedSpan]struct{}) {
	if declaredNonTextDataPayload(input, whole.start) {
		return spans, seen
	}

	value := input[whole.start:whole.end]

	var wholeReadable bool

	if len(value) <= maxShortBase64CandidateLen {
		spans, wholeReadable = appendShortRuleReadableBase64Whole(
			spans,
			input,
			whole,
			mayContribute,
			embeddedContextMayContribute,
			work,
			status,
		)
	} else {
		wholeReadable = readableBase64Span(input, whole, work, status)
	}

	// 읽을 수 있는 전체 토큰은 권위 있는 인코딩 경계이므로 내부를 재해석하지 않는다.
	if wholeReadable || !looksLikeEmbeddedBase64(value) || !decodeWorkComplete(status) {
		return spans, seen
	}

	if seen == nil {
		seen = make(map[encodedSpan]struct{}, min(maxDecodeScans+1, 16))
	}

	seen[whole] = struct{}{}

	for subStart := whole.start; subStart < whole.end && len(spans) <= maxDecodeScans && decodeWorkComplete(status); subStart++ {
		maximumEnd := min(whole.end, subStart+maxShortBase64CandidateLen)
		for subEnd := maximumEnd; subEnd-subStart >= 4 && len(spans) <= maxDecodeScans; subEnd-- {
			span := encodedSpan{start: subStart, end: subEnd}
			if span == whole {
				continue
			}

			spans = appendMatchingShortBase64Span(spans, seen, input, span, mayContribute, embeddedContextMayContribute, work, status)
			if !decodeWorkComplete(status) {
				break
			}
		}
	}

	return spans, seen
}

func appendShortRuleReadableBase64Whole(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, bool) {
	previousCount := len(spans)
	spans, readable := appendEmbeddedProtectedBase64Span(
		spans,
		input,
		whole,
		chargeReadableOnly,
		true,
		true,
		embeddedDirectOrContext,
		mayContribute,
		embeddedContextMayContribute,
		work,
		status,
	)

	if !readable || len(spans) != previousCount || !decodeWorkComplete(status) {
		return spans, readable
	}

	decoded, err := DecodeBase64Candidate(input[whole.start:whole.end])
	if err == nil && nestedShortContextMayContribute(input, whole, string(decoded), mayContribute, work, status) {
		spans = append(spans, whole)
	}

	return spans, readable
}

func nestedShortContextMayContribute(
	input string,
	outer encodedSpan,
	decoded string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) bool {
	unexploredNesting := false

	for position := 0; position < len(decoded) && decodeWorkComplete(status); {
		start := position //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(decoded, position)

		position = match.next

		if len(match.value) < 4 || len(match.value) > maxShortBase64CandidateLen {
			continue
		}

		inner, err := DecodeBase64Candidate(match.value)
		if err != nil || !IsReadableText(inner) {
			continue
		}

		if !consumeProtectedDecodeWork(work, status, len(match.value)) {
			return false
		}

		nested := replaceDecodedSpan(decoded, encodedSpan{start: start, end: match.next}, string(inner))
		surface := contextualMatchSurface(input, outer, nested)

		if !consumeProtectedContextWork(work, status, len(surface)) {
			return false
		}

		if mayContribute == nil || mayContribute(surface) {
			return true
		}

		if !unexploredNesting {
			unexploredNesting = hasDecodableShortRuleDecodeSurface(nested)
		}
	}

	// 이 경로는 한 겹만 더 푼다. 남은 겹이 여전히 decode 가능하면 미탐색 층이 있는
	// 것이므로 status를 세워 fail-closed로 넘긴다 — 세우지 않으면 3겹 이상 short
	// 체인이 "완전히 탐색된 무해 입력"으로 보고된다.
	if unexploredNesting {
		mergeDecodeStatus(status, DecodeDepthLimit)
	}

	return false
}

func readableBase64Span(
	input string,
	span encodedSpan,
	work *protectedDecodeWork,
	status *DecodeStatus,
) bool {
	if (span.end-span.start)%4 == 1 {
		return false
	}

	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return false
	}

	return consumeProtectedDecodeWork(work, status, span.end-span.start)
}

func appendMatchingShortBase64Span(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	span encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	if _, exists := seen[span]; exists {
		return spans
	}

	seen[span] = struct{}{}

	updated, _ := appendEmbeddedProtectedBase64Span(
		spans,
		input,
		span,
		chargePerAttempt,
		true,
		true,
		embeddedDirectOrContext,
		mayContribute,
		embeddedContextMayContribute,
		work,
		status,
	)
	if len(updated) != len(spans) || !decodeWorkComplete(status) {
		return updated
	}

	// whole-run 앞뒤에 base64 문자 1개만 붙어도 전체 decode가 깨져 이 sub-span 열거가
	// 유일한 탐지 경로가 된다. 여기서도 whole 경로와 동일하게 중첩 층을 검사해야
	// 노이즈 문자로 fail-closed를 우회하지 못한다.
	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return updated
	}

	if nestedShortContextMayContribute(input, span, string(decoded), mayContribute, work, status) {
		updated = append(updated, span)
	}

	return updated
}

// hasPlausibleShortRuleDecodeSurface와 달리 "그럴듯함"(소문자+숫자)이 아니라 실제 readable
// 복호 성공만 미탐색 층으로 인정한다. 넓은 술어를 쓰면 숫자가 섞인 무해한 2겹 중첩
// (예: base64^2("hi Bob2"))까지 fail-closed로 차단된다.
func hasDecodableShortRuleDecodeSurface(input string) bool {
	if containsHexFold(input) {
		for _, span := range shortRuleHexSpans(input) {
			if span.end > span.start {
				return true
			}
		}
	}

	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < 4 {
			continue
		}

		if len(match.value) > maxShortBase64CandidateLen {
			if looksLikeEmbeddedBase64(match.value) {
				return true
			}

			continue
		}

		var storage [maxShortBase64CandidateLen]byte

		decoded, err := decodeBase64CandidateInto(storage[:], match.value)

		if err == nil && IsReadableText(decoded) {
			return true
		}
	}

	return false
}

func plausibleShortBase64Value(value string) bool {
	if looksLikeEmbeddedBase64(value) {
		return true
	}

	var storage [maxShortBase64CandidateLen]byte

	decoded, err := decodeBase64CandidateInto(storage[:], value)

	return err == nil && IsReadableText(decoded)
}
