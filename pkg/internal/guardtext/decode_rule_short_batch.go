package guardtext

import "strings"

type shortRuleReading struct {
	span      encodedSpan
	projected encodedSpan
	decoded   string
}

// 독립된 전체 토큰의 동시 변환은 추가 해석이다. 원문을 일부 남기는 해석도 유지한다.
func (d *contextDecoder) admitShortRuleReadings(current decodeQueueEntry, readings []shortRuleReading) {
	if len(readings) < 2 || !d.result.Complete() || !independentShortReadings(current.text, readings) {
		for _, reading := range readings {
			d.admitContextual(current, reading.span, reading.decoded)
		}

		return
	}

	if !consumeProtectedContextWork(&d.protectedWork, &d.result.Status, len(current.text)) {
		return
	}

	var projection strings.Builder

	projection.Grow(len(current.text))

	position := 0

	for index := range readings {
		reading := &readings[index]
		projection.WriteString(current.text[position:reading.span.start])

		position = reading.span.end
		reading.projected.start = projection.Len()
		projection.WriteString(reading.decoded)

		reading.projected.end = projection.Len()
	}

	projection.WriteString(current.text[position:])

	projected := projection.String()

	for _, reading := range readings {
		d.admitContextual(current, reading.span, reading.decoded)
	}

	for _, reading := range readings {
		window, ok := d.admitMatchingShortContext(current, projected, reading.projected, reading.decoded)
		if !ok {
			continue
		}

		if hasPotentialDecodeSurface(reading.decoded) || hasDecodableShortRuleDecodeSurface(reading.decoded) || d.ruleExpansionCrossBoundaryMayContribute(projected, reading.projected) {
			d.deferRuleExpansion(current, window)
		}
	}
}

func (d *contextDecoder) admitMatchingShortContext(current decodeQueueEntry, input string, span encodedSpan, decoded string) (string, bool) {
	window, bounded := contextualAdmissionCandidate(input, span, decoded)
	if !bounded {
		d.result.Status |= DecodeByteLimit
		return "", false
	}

	if !consumeProtectedContextWork(&d.protectedWork, &d.result.Status, len(window)) {
		return "", false
	}

	if d.mayContribute(window) {
		d.admit(current, window)
	}

	return window, true
}

func independentShortReadings(input string, readings []shortRuleReading) bool {
	position := 0

	for _, reading := range readings {
		if reading.span.start < position {
			return false
		}

		for position < reading.span.start {
			position = nextBase64Candidate(input, position).next
		}

		if position != reading.span.start {
			return false
		}

		whole := nextBase64Candidate(input, position)
		if whole.next != reading.span.end {
			return false
		}

		position = whole.next
	}

	return true
}
