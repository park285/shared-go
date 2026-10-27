// Package openaicfg는 llm과 openaipreset이 함께 쓰는 OpenAI 요청 구성 규칙을 소유한다.
// 스택 소비자가 직접 쓰지 않는 구현 세부라 internal에 둔다.
package openaicfg

import (
	"errors"
	"regexp"
	"strings"
)

// ValidateMaxRetries는 호출자가 명시한 SDK 재시도 수를 검증한다. LLM 요청은 응답 유실 시 과금·tool 사용이
// 중복될 수 있는 비멱등 비용 작업이라 라이브러리가 재시도 수를 정하지 않는다. 음수는 오류이며
// (option.WithMaxRetries는 음수에 panic한다) 0은 재시도 없음이다.
// 소비자 자체 재시도와 겹치면 총 시도는 (소비자 시도) × (1 + 재시도 수)다.
func ValidateMaxRetries(retries int) error {
	if retries < 0 {
		return errors.New("max retries must not be negative")
	}

	return nil
}

const defaultResponsesSchemaName = "schema"

var responsesSchemaNameInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ResponsesSchemaName은 Responses API의 schema name 제약에 맞게 task name을 정규화한다.
// OpenAI Responses API의 text.format.name은 [a-zA-Z0-9_-](최대 64자)만 허용한다.
// TaskName을 그대로 schema name으로 넘기면 점 등 비허용 문자가 섞여 400(invalid_value)이 난다.
func ResponsesSchemaName(name string) string {
	cleaned := responsesSchemaNameInvalidChars.ReplaceAllString(strings.TrimSpace(name), "_")
	if cleaned == "" {
		return defaultResponsesSchemaName
	}

	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}

	return cleaned
}
