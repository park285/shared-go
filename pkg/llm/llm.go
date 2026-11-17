package llm

import "errors"

var ErrOpenAIEmptyOutput = errors.New("llm: openai empty output")

type Message struct {
	Role    string
	Content string
	// CacheBreakpoint는 이 메시지 끝을 GPT-5.6+ explicit prompt cache prefix의
	// 경계로 표시한다. 이전 모델은 해당 필드를 400으로 거부하므로 호출자가
	// 모델에 맞춰 설정해야 한다.
	CacheBreakpoint bool
}

type Usage struct {
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CachedInputTokens     int
	CacheWriteTokens      int
	ReasoningOutputTokens int
}
