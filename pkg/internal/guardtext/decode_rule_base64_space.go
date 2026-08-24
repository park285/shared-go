package guardtext

import "strings"

func normalizeSingleSpaceBase64(input string) (string, bool) {
	var normalized strings.Builder

	lastWrite := 0
	changed := false

	for index := 1; index+1 < len(input); index++ {
		if !joinsSplitBase64Token(input, index) {
			continue
		}

		if !changed {
			normalized.Grow(len(input) - 1)
		}

		normalized.WriteString(input[lastWrite:index])

		lastWrite = index + 1
		changed = true
	}

	if !changed {
		return input, false
	}

	normalized.WriteString(input[lastWrite:])

	return normalized.String(), true
}

func joinsSplitBase64Token(input string, index int) bool {
	if input[index] != ' ' || !isBase64Char(input[index-1]) || !isBase64Char(input[index+1]) {
		return false
	}

	leftStart := index - 1
	for leftStart > 0 && isBase64Char(input[leftStart-1]) {
		leftStart--
	}

	rightEnd := base64TokenEnd(input, index+1)

	if index-leftStart < 4 || rightEnd-(index+1) < 4 {
		return false
	}

	// 왼쪽 길이가 4의 배수면 그 공백은 정상 토큰 경계다(양쪽이 각자 온전한 base64).
	// 그런 분할은 기존 2조각 조합 경로가 이미 잡으므로, 여기서 합치면 그 탐지만 잃는다.
	// 이 규칙이 메우는 구멍은 4자 그룹 정렬을 깨뜨려 어느 쪽도 단독 해독되지 않는 분할이다.
	if (index-leftStart)%4 == 0 {
		return false
	}

	candidate := input[leftStart:index] + input[index+1:rightEnd]
	if len(candidate) < minBase64CandidateLen || len(candidate)%4 == 1 {
		return false
	}

	decoded, err := DecodeBase64Candidate(candidate)

	return err == nil && IsReadableText(decoded)
}

func base64TokenEnd(input string, start int) int {
	end := start
	for end < len(input) && isBase64Char(input[end]) {
		end++
	}

	for padding := 0; end < len(input) && input[end] == '=' && padding < 2; padding++ {
		end++
	}

	return end
}
