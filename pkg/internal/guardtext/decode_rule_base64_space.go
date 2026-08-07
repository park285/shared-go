package guardtext

import "strings"

const inlineSingleSpaceBase64Bytes = 256

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

	encodedLen := rightEnd - leftStart - 1
	if index-leftStart < 4 || rightEnd-(index+1) < 4 || encodedLen < minBase64CandidateLen || encodedLen%4 == 1 {
		return 0, false
	}
	if !readableJoinedBase64(input, leftStart, index, rightEnd, encodedLen) {
		return 0, false
	}

	return rightEnd, true
}

func readableJoinedBase64(input string, leftStart, space, rightEnd, encodedLen int) bool {
	if encodedLen > inlineSingleSpaceBase64Bytes {
		candidate := input[leftStart:space] + input[space+1:rightEnd]
		decoded, err := DecodeBase64Candidate(candidate)
		return err == nil && IsReadableText(decoded)
	}

	var encoded [inlineSingleSpaceBase64Bytes]byte
	copied := copy(encoded[:], input[leftStart:space])
	copy(encoded[copied:], input[space+1:rightEnd])

	var decoded [inlineSingleSpaceBase64Bytes]byte
	candidate := encoded[:encodedLen]
	encodings := base64RawStd[:]
	hasPadding := false
	hasURLAlphabet := false
	hasStandardAlphabet := false
	for _, value := range candidate {
		switch value {
		case '=':
			hasPadding = true
		case '-', '_':
			hasURLAlphabet = true
		case '+', '/':
			hasStandardAlphabet = true
		}
	}
	if hasPadding {
		if hasURLAlphabet && !hasStandardAlphabet {
			encodings = base64PaddedURL[:]
		} else {
			encodings = base64PaddedStd[:]
		}
	} else if hasURLAlphabet && !hasStandardAlphabet {
		encodings = base64RawURL[:]
	}

	for _, encoding := range encodings {
		decodedLen, err := encoding.Decode(decoded[:], candidate)
		if err == nil {
			return IsReadableText(decoded[:decodedLen])
		}
	}

	return false
}
