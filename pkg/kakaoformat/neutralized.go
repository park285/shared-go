package kakaoformat

import "strings"

const neutralizedMarkers = "*_`~]#&<\\"

// 호출자가 마커 뒤 ZWSP로 무력화한 표시 문자를 코드·강조 parser보다 먼저 보호합니다.
// 접기용 연속 ZWSP와 원래 URL은 그대로 두고, 가시 문자와 단발 ZWSP도 복원합니다.
func protectNeutralizedMarkers(input string, dst *store) string {
	if !strings.Contains(input, "\u200b") {
		return input
	}

	var output strings.Builder
	// 같은 마커의 복원값은 고정이므로 메시지 안에서 보호 표식을 재사용합니다.
	var tokens [len(neutralizedMarkers)]string

	start := 0

	for i := 0; i < len(input); i++ {
		marker := strings.IndexByte(neutralizedMarkers, input[i])
		if marker < 0 || !strings.HasPrefix(input[i+1:], "\u200b") {
			continue
		}

		end := i + 1 + len("\u200b")

		if tokens[marker] == "" {
			tokens[marker] = dst.Put(input[i:end])
		}

		output.WriteString(input[start:i])
		output.WriteString(tokens[marker])

		start = end
		i = end - 1
	}

	if start == 0 {
		return input
	}

	output.WriteString(input[start:])

	return output.String()
}
