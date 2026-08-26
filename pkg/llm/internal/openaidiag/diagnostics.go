package openaidiag

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

var (
	ErrEmptyOutput   = errors.New("llm: openai empty output")
	ErrRefusalOutput = errors.New("llm: openai refusal output")
)

type safeProviderError struct {
	statusCode int
	code       string
	param      string
	apiType    string
	errType    string
}

func (e safeProviderError) Error() string {
	parts := []string{"llm provider request failed"}

	if e.statusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.statusCode))
	}

	parts = appendPart(parts, "code", e.code)
	parts = appendPart(parts, "api_type", e.apiType)
	parts = appendPart(parts, "param", e.param)
	parts = appendPart(parts, "error_type", e.errType)

	return strings.Join(parts, " ")
}

func appendPart(parts []string, key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}

	return append(parts, key+"="+value)
}

func Text(resp *responses.Response) (string, error) {
	if resp == nil {
		return "", ErrEmptyOutput
	}

	out, err := PreferredText(resp, resp.OutputText())
	if err != nil {
		return out, fmt.Errorf("preferred text: %w", err)
	}

	return out, nil
}

func PreferredText(resp *responses.Response, text string) (string, error) {
	if resp == nil {
		return "", ErrEmptyOutput
	}

	if text = strings.TrimSpace(text); text != "" {
		return text, nil
	}

	diagnostic := describeOutput(resp)
	if diagnostic == "" {
		return "", ErrEmptyOutput
	}

	if outputHasRefusal(resp.Output) {
		return "", fmt.Errorf("%w: %w: %s", ErrEmptyOutput, ErrRefusalOutput, diagnostic)
	}

	return "", fmt.Errorf("%w: %s", ErrEmptyOutput, diagnostic)
}

func SafeError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrEmptyOutput) || errors.Is(err, ErrRefusalOutput) {
		return err
	}

	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}

	if apiErr, ok := errors.AsType[*openai.Error](err); ok && apiErr != nil {
		return safeProviderError{
			statusCode: apiErr.StatusCode,
			code:       apiErr.Code,
			param:      apiErr.Param,
			apiType:    apiErr.Type,
			errType:    ErrorClass(apiErr),
		}
	}

	return safeProviderError{errType: ErrorClass(err)}
}

func describeOutput(resp *responses.Response) string {
	if resp == nil {
		return ""
	}

	parts := make([]string, 0, 4)

	if resp.Status != "" {
		parts = append(parts, fmt.Sprintf("status=%s", resp.Status))
	}

	if resp.IncompleteDetails.Reason != "" {
		parts = append(parts, fmt.Sprintf("incomplete_reason=%s", resp.IncompleteDetails.Reason))
	}

	return strings.Join(appendOutputDiagnostics(parts, resp.Output), " ")
}

func appendOutputDiagnostics(parts []string, output []responses.ResponseOutputItemUnion) []string {
	outputTypes := make([]string, 0, len(output))
	for i := range output {
		item := &output[i]

		outputTypes = append(outputTypes, describeOutputItem(item))

		if outputItemRefusal(item) != "" {
			parts = append(parts, "refusal=true")
		}
	}

	if len(outputTypes) > 0 {
		parts = append(parts, "output="+strings.Join(outputTypes, ","))
	}

	return parts
}

func outputHasRefusal(output []responses.ResponseOutputItemUnion) bool {
	for i := range output {
		if outputItemRefusal(&output[i]) != "" {
			return true
		}
	}

	return false
}

func describeOutputItem(item *responses.ResponseOutputItemUnion) string {
	if item == nil {
		return "unknown"
	}

	itemType := strings.TrimSpace(item.Type)
	if itemType == "" {
		itemType = "unknown"
	}

	if item.Status != "" {
		return itemType + "/" + item.Status
	}

	return itemType
}

func outputItemRefusal(item *responses.ResponseOutputItemUnion) string {
	if item == nil || item.Type != "message" {
		return ""
	}

	for i := range item.Content {
		content := &item.Content[i]
		if content.Type == "refusal" && strings.TrimSpace(content.Refusal) != "" {
			return strings.TrimSpace(content.Refusal)
		}
	}

	return ""
}

// ErrorClass는 메시지 본문을 배제하고 원인 타입만 남긴다. Provider·decoder 에러
// 메시지에는 요청/응답 원문 조각이 섞여 들어오므로 진단에 그대로 실을 수 없다.
func ErrorClass(err error) string {
	if err == nil {
		return ""
	}

	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}
