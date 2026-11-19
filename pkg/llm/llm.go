package llm

import "errors"

var (
	ErrOpenAIEmptyOutput   = errors.New("llm: openai empty output")
	ErrOpenAIRefusalOutput = errors.New("llm: openai refusal output")
)

type Message struct {
	Role            string
	Content         string
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
