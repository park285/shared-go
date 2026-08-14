package llm

import (
	"errors"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/pkg/llm/internal/openaidiag"
)

func extractResponsesOutputText(resp *responses.Response) (string, error) {
	return openaidiag.Text(resp)
}

func shouldFallbackToChatCompletions(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrOpenAIRefusalOutput) {
		return false
	}
	if shouldFallbackOpenAIError(err) {
		return true
	}
	return false
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

func safeOpenAICompatibleError(err error) error {
	return openaidiag.SafeError(err)
}
