package llm

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
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
	parts = appendTrimmedPart(parts, "code", e.code)
	parts = appendTrimmedPart(parts, "api_type", e.apiType)
	parts = appendTrimmedPart(parts, "param", e.param)
	parts = appendTrimmedPart(parts, "error_type", e.errType)
	return strings.Join(parts, " ")
}

func appendTrimmedPart(parts []string, key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, key+"="+value)
}

func extractResponsesOutputText(resp *responses.Response) (string, error) {
	if resp == nil {
		return "", ErrOpenAIEmptyOutput
	}

	text := strings.TrimSpace(resp.OutputText())
	if text != "" {
		return text, nil
	}

	diagnostic := describeResponsesOutput(resp)
	if diagnostic == "" {
		return "", ErrOpenAIEmptyOutput
	}
	if responsesOutputHasRefusal(resp.Output) {
		return "", fmt.Errorf("%w: %w: %s", ErrOpenAIEmptyOutput, ErrOpenAIRefusalOutput, diagnostic)
	}
	return "", fmt.Errorf("%w: %s", ErrOpenAIEmptyOutput, diagnostic)
}

func describeResponsesOutput(resp *responses.Response) string {
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
	return strings.Join(appendResponseOutputDiagnostics(parts, resp.Output), " ")
}

func appendResponseOutputDiagnostics(parts []string, output []responses.ResponseOutputItemUnion) []string {
	outputTypes := make([]string, 0, len(output))
	for i := range output {
		item := &output[i]
		outputTypes = append(outputTypes, describeResponseOutputItemType(item))
		if responseOutputItemRefusal(item) != "" {
			parts = append(parts, "refusal=true")
		}
	}
	if len(outputTypes) > 0 {
		parts = append(parts, "output="+strings.Join(outputTypes, ","))
	}
	return parts
}

func responsesOutputHasRefusal(output []responses.ResponseOutputItemUnion) bool {
	for i := range output {
		item := &output[i]
		if responseOutputItemRefusal(item) != "" {
			return true
		}
	}
	return false
}

func describeResponseOutputItemType(item *responses.ResponseOutputItemUnion) string {
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

func responseOutputItemRefusal(item *responses.ResponseOutputItemUnion) string {
	if item == nil {
		return ""
	}
	if item.Type != "message" {
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

func shouldFallbackToChatCompletions(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrOpenAIRefusalOutput) {
		return false
	}
	if errors.Is(err, ErrOpenAIEmptyOutput) {
		return true
	}
	if shouldFallbackOpenAIError(err) {
		return true
	}
	return shouldFallbackNetworkError(err)
}

func shouldFallbackOpenAIError(err error) bool {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return shouldFallbackOpenAIStatus(apiErr.StatusCode) || shouldFallbackOpenAICode(apiErr.Code)
	}
	return false
}

func shouldFallbackOpenAIStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func shouldFallbackOpenAICode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "unsupported", "unsupported_endpoint", "unsupported_api", "not_implemented":
		return true
	default:
		return false
	}
}

func shouldFallbackNetworkError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func safeOpenAICompatibleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrOpenAIEmptyOutput) || errors.Is(err, ErrOpenAIRefusalOutput) {
		return err
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		return safeProviderError{
			statusCode: apiErr.StatusCode,
			code:       apiErr.Code,
			param:      apiErr.Param,
			apiType:    apiErr.Type,
			errType:    errorTypeName(apiErr),
		}
	}

	return safeProviderError{errType: errorTypeName(err)}
}

func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}
