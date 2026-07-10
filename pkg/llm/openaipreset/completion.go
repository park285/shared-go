package openaipreset

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	json "github.com/park285/shared-go/pkg/json"
	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/llm/internal/openaidiag"
)

type Message struct {
	Role    string
	Content string
}

type ResponseFormat struct {
	Name   string
	Schema map[string]any
	Strict bool
}

type CompletionRequest struct {
	Messages        []Message
	Model           string
	Temperature     *float64
	ReasoningEffort string
	WebSearch       bool
	CacheKey        string
	ResponseFormat  *ResponseFormat
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

	params, requestedModel := c.completionParams(req)
	attrs := promptSummaryAttrs(requestedModel, completionPrompt(req.Messages))
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

func completionPrompt(messages []Message) string {
	var prompt strings.Builder
	for i := range messages {
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		if prompt.Len() > 0 {
			prompt.WriteByte('\n')
		}
		prompt.WriteString(content)
	}
	return prompt.String()
}

func (c *Client) completionParams(req CompletionRequest) (responses.ResponseNewParams, string) {
	model := c.completionModel(req.Model)
	temperature := c.temperature
	if req.Temperature != nil {
		temperature = req.Temperature
	}

	params := responses.ResponseNewParams{
		Model: model,
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: completionInput(req.Messages),
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

	return params, model
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
		out = append(out, responses.ResponseInputItemParamOfMessage(content, completionRole(message.Role)))
	}
	return out
}

func completionRole(role string) responses.EasyInputMessageRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
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
	if looksLikeToolCallEnvelope(text) {
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
		if text == "" || looksLikeToolCallEnvelope(text) {
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

func looksLikeToolCallEnvelope(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed[0] != '{' {
		return false
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return false
	}

	for _, key := range []string{"tool_calls", "function_call", "tool_call"} {
		if _, ok := envelope[key]; ok {
			return true
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
		ReasoningOutputTokens: int(usage.OutputTokensDetails.ReasoningTokens),
	}
}
