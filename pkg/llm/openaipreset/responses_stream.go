package openaipreset

import (
	"context"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/v2/pkg/llm/internal/openaidiag"
)

func (c *Client) completedResponsesStream(ctx context.Context, params responses.ResponseNewParams) (result *responses.Response, resultErr error) {
	stream := c.openai.Responses.NewStreaming(ctx, params)

	defer func() {
		// 연결 종료 실패도 부분 성공으로 반환하지 않으며 기존 요청 오류를 우선한다.
		if err := stream.Close(); err != nil && resultErr == nil {
			result = nil
			resultErr = fmt.Errorf("close responses stream: %w", openaidiag.SafeError(err))
		}
	}()

	var completed *responses.Response

	for stream.Next() {
		event := stream.Current()

		if completed != nil {
			return nil, errors.New("responses stream event after completion")
		}

		switch event.Type {
		case "response.completed":
			response := event.AsResponseCompleted().Response
			if response.Status != "completed" || response.ID == "" {
				return nil, errors.New("responses stream has invalid completion")
			}

			// completed의 전체 출력이 기준이므로 중간 delta는 결합하지 않는다.
			completed = &response
		case "response.failed", "response.incomplete", "error":
			return nil, errors.New("responses stream did not complete successfully")
		}
	}

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("responses stream: %w", openaidiag.SafeError(err))
	}

	if completed == nil {
		return nil, errors.New("responses stream ended without completion")
	}

	return completed, nil
}
