package openaipreset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/llm/internal/openaidiag"
)

type Message = sharedllm.Message

type ResponseFormat struct {
	Name   string
	Schema map[string]any
	Strict bool
}

type CompletionRequest struct {
	Messages           []Message
	Model              string
	Temperature        *float64
	ReasoningEffort    string
	WebSearch          bool
	CacheKey           string
	ResponseFormat     *ResponseFormat
	InstructionProfile *sharedllm.InstructionProfile
}

type CompletionResponse struct {
	Text  string
	Model string
	Usage sharedllm.Usage
}

func (c *Client) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if c == nil {
		return CompletionResponse{}, errClientNil
	}
	if ctx == nil {
		return CompletionResponse{}, sharedllm.ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return CompletionResponse{}, err
	}

	params, requestedModel, err := c.completionParams(req)
	if err != nil {
		return CompletionResponse{}, err
	}
	attrs := promptSummaryAttrs(requestedModel, completionPromptLen(req.Messages))
	return runRequest(ctx, c.logger, attrs, func() (CompletionResponse, error) {
		resp, err := c.openai.Responses.New(ctx, params)
		if err != nil {
			return CompletionResponse{}, fmt.Errorf("openai responses API: %w", openaidiag.SafeError(err))
		}

		completion := CompletionFromResponse(resp, requestedModel)
		completion.Text, err = openaidiag.PreferredText(resp, completion.Text)
		if err != nil {
			return CompletionResponse{}, err
		}
		c.usageReporter.RecordUsage(ctx, providerLabel, completion.Model, completion.Usage)
		return completion, nil
	})
}

func completionPromptLen(messages []Message) int {
	total := 0
	for i := range messages {
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		if total > 0 {
			total++
		}
		total += len(content)
	}
	return total
}

func (c *Client) completionParams(req CompletionRequest) (responses.ResponseNewParams, string, error) {
	model := c.completionModel(req.Model)
	temperature := c.temperature
	if req.Temperature != nil {
		temperature = req.Temperature
	}
	messages := req.Messages
	if req.InstructionProfile != nil {
		adapted, err := sharedllm.AdaptInstructionMessages(messages, *req.InstructionProfile)
		if err != nil {
			return responses.ResponseNewParams{}, model, err
		}
		messages = adapted
	}

	params := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: completionInput(messages),
		},
	}
	if temperature != nil {
		params.Temperature = openai.Float(*temperature)
	}
	if req.WebSearch || c.webSearch {
		params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsAuto)}
		params.Tools = []responses.ToolUnionParam{responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch)}
	}
	if req.ResponseFormat != nil {
		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   strings.TrimSpace(req.ResponseFormat.Name),
					Schema: req.ResponseFormat.Schema,
					Strict: openai.Bool(req.ResponseFormat.Strict),
				},
			},
		}
	}
	if effort := c.completionReasoningEffort(req.ReasoningEffort); effort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort)}
	}
	if cacheKey := strings.TrimSpace(req.CacheKey); cacheKey != "" {
		params.PromptCacheKey = openai.String(cacheKey)
	}

	return params, model, nil
}

func (c *Client) completionModel(model string) string {
	if override := strings.TrimSpace(model); override != "" {
		return override
	}
	return strings.TrimSpace(c.model)
}

func (c *Client) completionReasoningEffort(effort string) string {
	if override := strings.TrimSpace(effort); override != "" {
		return override
	}
	return strings.TrimSpace(c.reasoningEffort)
}

func completionInput(messages []Message) responses.ResponseInputParam {
	out := make(responses.ResponseInputParam, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		out = append(out, responses.ResponseInputItemParamOfMessage(content, CompletionRole(message.Role)))
	}
	return out
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

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return false
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		if !ok {
			return false
		}
		if key == "tool_calls" || key == "function_call" || key == "tool_call" {
			return true
		}
		if err := skipJSONValue(decoder); err != nil {
			return false
		}
	}

	return false
}

func skipJSONValue(decoder *json.Decoder) error {
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, isDelim := token.(json.Delim)
		switch {
		case !isDelim:
			if depth == 0 {
				return nil
			}
		case delim == '{' || delim == '[':
			depth++
		default:
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
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
		ReasoningOutputTokens: int(usage.OutputTokensDetails.ReasoningTokens),
	}
}
