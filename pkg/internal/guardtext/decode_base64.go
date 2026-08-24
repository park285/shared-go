package guardtext

import (
	"encoding/base64"
)

type base64Candidate struct {
	value string
	next  int
}

func base64SpansAtLeast(input string, minimum int) []encodedSpan {
	var (
		spans      []encodedSpan
		seenValues map[string]struct{}
		scratch    []byte
	)

	dedup := len(input) >= spanContextDedupMinInputBytes

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minimum || declaredNonTextDataPayload(input, start) {
			continue
		}

		if dedup {
			if _, duplicate := seenValues[match.value]; duplicate {
				continue
			}

			if seenValues == nil {
				seenValues = make(map[string]struct{}, 8)
			}

			seenValues[match.value] = struct{}{}
		}

		// 판정을 마친(디코드 불가·비가독) 스팬의 기각은 예산 소진이 아니라 완결이다:
		// 소비 단계에서도 동일하게 버려질 스팬을 목록에 남기면 scan 예산만 태워
		// 무해한 해시·숫자열 장문이 decode_incomplete로 오차단된다.
		scratch = growBase64Scratch(scratch, len(match.value))

		decoded, err := decodeBase64CandidateInto(scratch, match.value)

		if err != nil || !IsReadableText(decoded) {
			continue
		}

		spans = append(spans, encodedSpan{start: start, end: match.next})
	}

	return spans
}

// RawStdEncoding.DecodedLen이 네 후보 인코딩 중 항상 최대라 이 크기면 Decode가
// 목적지 부족으로 넘치지 않는다. 스팬마다 재사용하므로 열거 1회당 할당도 1회다.
func growBase64Scratch(scratch []byte, encodedLen int) []byte {
	needed := base64.RawStdEncoding.DecodedLen(encodedLen)
	if cap(scratch) >= needed {
		return scratch[:needed]
	}

	return make([]byte, needed)
}

// spanContextSeen은 값이 같아도 주변 문맥이 다르면 문맥 의존 매칭(임베디드 문맥
// 판정, splice 규칙 매칭) 결과가 달라질 수 있으므로 값 키 아래에 스팬 위치를 모아
// 앞뒤 윈도우까지 같을 때만 중복으로 접는다. 작은 입력은 반복으로 예산이 고갈될
// 수 없으므로 dedup을 아예 걸지 않아 핫패스 할당 상한을 지킨다.
type spanContextRef struct {
	text string
	span encodedSpan
}

type spanContextSeen struct {
	entries map[string][]spanContextRef
}

const spanContextDedupMinInputBytes = 1 << 10

func newSpanContextSeen(input string) *spanContextSeen {
	if len(input) < spanContextDedupMinInputBytes {
		return nil
	}

	return &spanContextSeen{}
}

func (s *spanContextSeen) duplicate(text string, span encodedSpan) bool {
	if s == nil {
		return false
	}

	value := text[span.start:span.end]
	for _, prior := range s.entries[value] {
		if sameSpanContext(prior, spanContextRef{text: text, span: span}) {
			return true
		}
	}

	if s.entries == nil {
		s.entries = make(map[string][]spanContextRef, 8)
	}

	s.entries[value] = append(s.entries[value], spanContextRef{text: text, span: span})

	return false
}

func sameSpanContext(left, right spanContextRef) bool {
	const contextBytes = 512

	return left.text[max(0, left.span.start-contextBytes):left.span.start] ==
		right.text[max(0, right.span.start-contextBytes):right.span.start] &&
		left.text[left.span.end:min(len(left.text), left.span.end+contextBytes)] ==
			right.text[right.span.end:min(len(right.text), right.span.end+contextBytes)]
}

func contextualBase64SpansAtLeast(input string, minimum int) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/minimum))
	work := protectedDecodeWork{}
	seenWholes := newSpanContextSeen(input)

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minimum {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		if pathSegments, ok := httpURLPathBase64Segments(input, whole, minimum); ok {
			for _, segment := range pathSegments {
				var complete bool

				spans, complete = appendContextualBase64Whole(spans, input, segment, minimum, &work, seenWholes)

				if !complete {
					return appendDecodeScanOverflow(spans, segment)
				}
			}

			continue
		}

		var complete bool

		spans, complete = appendContextualBase64Whole(spans, input, whole, minimum, &work, seenWholes)

		if !complete {
			return appendDecodeScanOverflow(spans, whole)
		}
	}

	return spans
}

func appendContextualBase64Whole(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	work *protectedDecodeWork,
	seenWholes *spanContextSeen,
) ([]encodedSpan, bool) {
	if declaredNonTextDataPayload(input, whole.start) {
		return spans, true
	}

	if seenWholes.duplicate(input, whole) {
		return spans, true
	}

	value := input[whole.start:whole.end]
	decoded, err := DecodeBase64Candidate(value)

	if err == nil && IsReadableText(decoded) {
		return append(spans, whole), true
	}

	if !looksLikeEmbeddedBase64(value) {
		return spans, true
	}

	return appendReadableBase64Subspans(append(spans, whole), input, whole, minimum, work)
}
