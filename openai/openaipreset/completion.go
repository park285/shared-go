package openaipreset

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/openai/openai-go/v3/responses"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

type CompletionResponse struct {
	Text  string
	Model string
	Usage sharedllm.Usage
}

func CompletionRole(role string) responses.EasyInputMessageRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "developer":
		return responses.EasyInputMessageRoleDeveloper
	case "assistant":
		return responses.EasyInputMessageRoleAssistant
	case "system":
		return responses.EasyInputMessageRoleSystem
	default:
		return responses.EasyInputMessageRoleUser
	}
}

func CompletionFromResponse(resp *responses.Response, requestedModel string) CompletionResponse {
	if resp == nil {
		return CompletionResponse{Model: strings.TrimSpace(requestedModel)}
	}

	model := strings.TrimSpace(resp.Model)
	if model == "" {
		model = strings.TrimSpace(requestedModel)
	}

	return CompletionResponse{
		Text:  completionText(resp),
		Model: model,
		Usage: UsageFromResponseUsage(&resp.Usage),
	}
}

func completionText(resp *responses.Response) string {
	if resp == nil {
		return ""
	}

	if text := preferredMessageText(resp.Output); text != "" {
		return text
	}

	text := strings.TrimSpace(resp.OutputText())
	if LooksLikeToolCallEnvelope(text) {
		return ""
	}

	return text
}

func preferredMessageText(items []responses.ResponseOutputItemUnion) string {
	var fallback string

	for i := range items {
		item := items[i]
		if item.Type != "message" || item.Status != "completed" {
			continue
		}

		text := messageContentText(item.Content)
		if text == "" || LooksLikeToolCallEnvelope(text) {
			continue
		}

		if item.Phase == responses.ResponseOutputMessagePhaseFinalAnswer {
			return text
		}

		fallback = text
	}

	return fallback
}

func messageContentText(content []responses.ResponseOutputMessageContentUnion) string {
	var builder strings.Builder

	for i := range content {
		if content[i].Type != "output_text" {
			continue
		}

		builder.WriteString(content[i].Text)
	}

	return strings.TrimSpace(builder.String())
}

// LooksLikeToolCallEnvelope는 최상위 키만 순차 스캔해 tool-call envelope를 판정한다.
// 전체 파싱과 달리 키를 만난 시점에 종료하므로, envelope 뒤쪽이 잘려 있어도 true다
// (tool-call 잔재를 사용자 텍스트로 흘리지 않는 쪽이 보수적).
func LooksLikeToolCallEnvelope(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed[0] != '{' {
		return false
	}

	decoder := jsontext.NewDecoder(strings.NewReader(trimmed))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != jsontext.KindBeginObject {
		return false
	}

	for decoder.PeekKind() != jsontext.KindEndObject {
		token, err := decoder.ReadToken()
		if err != nil {
			return false
		}

		if token.Kind() != jsontext.KindString {
			return false
		}

		key := token.String()
		if key == "tool_calls" || key == "function_call" || key == "tool_call" {
			return true
		}

		if err := decoder.SkipValue(); err != nil {
			return false
		}
	}

	return false
}

func UsageFromResponseUsage(usage *responses.ResponseUsage) sharedllm.Usage {
	if usage == nil {
		return sharedllm.Usage{}
	}

	return sharedllm.Usage{
		InputTokens:           int(usage.InputTokens),
		OutputTokens:          int(usage.OutputTokens),
		TotalTokens:           int(usage.TotalTokens),
		CachedInputTokens:     int(usage.InputTokensDetails.CachedTokens),
		CacheWriteTokens:      int(usage.InputTokensDetails.CacheWriteTokens),
		ReasoningOutputTokens: int(usage.OutputTokensDetails.ReasoningTokens),
	}
}
