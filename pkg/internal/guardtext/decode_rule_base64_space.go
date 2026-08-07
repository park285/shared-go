package guardtext

import "strings"

func normalizeSingleSpaceBase64(input string) (string, bool) {
	var normalized strings.Builder
	lastWrite := 0
	changed := false

	for index := 1; index+1 < len(input); index++ {
		rightEnd, valid := singleSpaceBase64Candidate(input, index)
		if !valid {
			continue
		}
		if !changed {
			normalized.Grow(len(input) - 1)
		}
		normalized.WriteString(input[lastWrite:index])
		lastWrite = index + 1
		changed = true
		index = rightEnd - 1
	}

	if !changed {
		return input, false
	}
	normalized.WriteString(input[lastWrite:])

	return normalized.String(), true
}

func singleSpaceBase64Candidate(input string, index int) (int, bool) {
	if input[index] != ' ' || !isBase64Char(input[index-1]) || !isBase64Char(input[index+1]) {
		return 0, false
	}

	leftStart := index - 1
	for leftStart > 0 && isBase64Char(input[leftStart-1]) {
		leftStart--
	}
	rightEnd := index + 1
	for rightEnd < len(input) && isBase64Char(input[rightEnd]) {
		rightEnd++
	}
	for padding := 0; rightEnd < len(input) && input[rightEnd] == '=' && padding < 2; padding++ {
		rightEnd++
	}

	if index-leftStart < 4 || rightEnd-(index+1) < 4 {
		return 0, false
	}
	candidate := input[leftStart:index] + input[index+1:rightEnd]
	if len(candidate) < minBase64CandidateLen || len(candidate)%4 == 1 {
		return 0, false
	}
	decoded, err := DecodeBase64Candidate(candidate)
	if err != nil || !IsReadableText(decoded) {
		return 0, false
	}

	return rightEnd, true
}
