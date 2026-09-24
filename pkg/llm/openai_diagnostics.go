package llm

import (
	"fmt"

	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
)

func extractResponsesOutputText(resp *responses.Response) (string, error) {
	out, err := openaidiag.Text(resp)
	if err != nil {
		return out, fmt.Errorf("text: %w", err)
	}

	return out, nil
}

func safeOpenAICompatibleError(err error) error {
	if safeErr := openaidiag.SafeError(err); safeErr != nil {
		return fmt.Errorf("safe error: %w", safeErr)
	}

	return nil
}
