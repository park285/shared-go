package promptguard

import (
	"strings"
	"unicode"
)

type decodeViewState struct {
	started    bool
	space      bool
	joined     bool
	word       bool
	separators uint8
}

// graphViewText는 viewTexts가 만든 조각 text에 앞 상태의 공백·결합 규칙을 이어 적용한다. 원문 view는 그대로 쓴다.
func graphViewText(text, view string, state decodeViewState) (string, decodeViewState) {
	if view == viewRaw {
		return text, state
	}

	var output strings.Builder

	output.Grow(len(text) + 1)

	for _, value := range text {
		if value == ' ' {
			state.space = state.started
			continue
		}

		if state.space {
			writeGraphViewRune(&output, &state, view, ' ')

			state.space = false
		}

		writeGraphViewRune(&output, &state, view, value)

		state.started = true
	}

	return output.String(), state
}

func writeGraphViewRune(output *strings.Builder, state *decodeViewState, view string, value rune) {
	if view != viewJoined && view != viewAggregateJoined {
		output.WriteRune(value)

		return
	}

	if unicode.IsSpace(value) || unicode.Is(unicode.Z, value) || unicode.IsPunct(value) || unicode.IsSymbol(value) {
		state.separators = min(state.separators+1, 5)
		return
	}

	word := unicode.IsLetter(value) || unicode.IsNumber(value) || unicode.IsMark(value)
	if state.separators > 0 && (!state.word || !word || state.separators > 4) && state.joined {
		output.WriteByte(' ')
	}

	state.separators = 0
	state.word = word
	state.joined = true

	output.WriteRune(value)
}
