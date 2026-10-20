package guardtext

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// WalkFlatBase64Readings는 독립된 Base64 구간마다 원문과 모든 국소 해석을 전달한다.
// 구간 내부는 기존 디코더의 후보·깊이·스캔 한도를 그대로 적용하고 실제 변환 작업
// 예산은 전체 입력에서 공유한다. 구간 사이의 조합은 visit 소유자가 증명해야 한다.
// 증명할 수 없는 인코딩이나 정규화 경계에서는 false이며 원래 검사를 생략하면 안 된다.
// 콜백 visit은 증명 전용이며 입력 평가나 공격 근거 기록에 사용하면 안 된다.
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

	// 선행 자모·결합 문자는 왼쪽 구간과 합성될 수 있다. 오른쪽 고정 문자도
	// 정규화 경계인지 확인하므로 내부·후행 결합 문자는 구간 안에서 처리할 수 있다.
	return norm.NFKC.PropertiesString(reading).BoundaryBefore()
}

func flatReadingBoundaries(input string, end int) bool {
	return end == len(input) || norm.NFKC.PropertiesString(input[end:]).BoundaryBefore()
}
