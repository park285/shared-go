package guardtext

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

func WalkFlatBase64Readings(input string, visit func(readings []string) bool) bool {
	if !flatReadingInputSupported(input) {
		return false
	}

	work := protectedDecodeWork{}
	last := 0

	for position := 0; position < len(input); {
		if !isEncodedValueByte(input[position]) {
			position++
			continue
		}

		start := position //nolint:copyloopvar // 구간의 끝을 찾는 동안 시작 위치를 보존한다.
		for position < len(input) && isEncodedValueByte(input[position]) {
			position++
		}

		piece := input[start:position]
		if !HasPotentialRuleDecodeSurface(piece) {
			continue
		}

		decoded := flatBase64Readings(piece, &work)
		if !decoded.Complete() {
			return false
		}

		if len(decoded.Candidates) == 0 {
			continue
		}

		if !flatReadingBoundaries(input, position) {
			return false
		}

		var readings [maxDecodeCandidates + 1]string

		readings[0] = input[last:start]
		if !visit(readings[:1]) {
			return false
		}

		readings[0] = piece

		for index, candidate := range decoded.Candidates {
			if !flatReadingStable(candidate) || !consumeProtectedContextWork(&work, nil, len(candidate)) {
				return false
			}

			readings[index+1] = candidate
		}

		if !visit(readings[:len(decoded.Candidates)+1]) {
			return false
		}

		last = position
	}

	return visit([]string{input[last:]})
}

func flatReadingInputSupported(input string) bool {
	if strings.ContainsAny(input, `%&\`) || containsHexFold(input) {
		return false
	}

	for index := range len(input) {
		if !isEncodedValueByte(input[index]) {
			return true
		}
	}

	return false
}

func flatBase64Readings(piece string, work *protectedDecodeWork) DecodeResult {
	decoder := newRuleContextDecoder([]string{piece}, everyRuleReading, nil, nil)

	decoder.protectedWork = *work
	decoder.visited[piece] = struct{}{}
	decoder.queue = append(decoder.queue, decodeQueueEntry{text: piece})

	for decoder.pending() && decoder.result.Complete() {
		decoder.expandRuleNext()
	}

	*work = decoder.protectedWork

	return decoder.result
}

func everyRuleReading(string) bool { return true }

func flatReadingStable(reading string) bool {
	if strings.ContainsAny(reading, `%&\`) || containsHexFold(reading) || NormalizeEncodingSyntax(reading) != reading {
		return false
	}

	return norm.NFKC.PropertiesString(reading).BoundaryBefore()
}

func flatReadingBoundaries(input string, end int) bool {
	return end == len(input) || norm.NFKC.PropertiesString(input[end:]).BoundaryBefore()
}
