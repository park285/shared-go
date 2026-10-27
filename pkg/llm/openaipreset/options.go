package openaipreset

import (
	"log/slog"
	"net/http"
	"strings"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

type config struct {
	temperature          *float64
	reasoningEffort      string
	webSearch            bool
	chatCompletions      bool
	httpClient           *http.Client
	usageReporter        sharedllm.UsageReporter
	logger               *slog.Logger
	maxRetries           *int
	promptCacheKeyPrefix string
}

type Option func(*config)

// WithPromptCacheKeyPrefix는 요청마다 prefix+task를 prompt_cache_key로 전송한다.
// GPT-5.6+는 이 키가 있어야 prefix 캐시 매칭이 성립한다.
func WithPromptCacheKeyPrefix(prefix string) Option {
	return func(c *config) {
		c.promptCacheKeyPrefix = strings.TrimSpace(prefix)
	}
}

func WithTemperature(temperature float64) Option {
	return func(c *config) {
		c.temperature = &temperature
	}
}

func WithWebSearch(enabled bool) Option {
	return func(c *config) {
		c.webSearch = enabled
	}
}

func WithChatCompletions() Option {
	return func(c *config) {
		c.chatCompletions = true
	}
}

func WithReasoningEffort(effort string) Option {
	return func(c *config) {
		if trimmed := strings.TrimSpace(effort); trimmed != "" {
			c.reasoningEffort = trimmed
		}
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithUsageReporter(reporter sharedllm.UsageReporter) Option {
	return func(c *config) {
		if reporter != nil {
			c.usageReporter = reporter
		}
	}
}

// WithLogger는 llm.request.started·succeeded·failed 로그를 받을 logger를 정한다. 지정하지 않거나 nil이면
// 기록 시점의 slog.Default()에 기록한다. 생성 시점에 고정하지 않으므로 client를 만든 뒤 slog.SetDefault로
// 바꾼 서비스 logger를 따른다. 로그 속성은 provider·model·prompt_len·소요 시간뿐이며 prompt 본문은 담지 않는다.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		c.logger = logger
	}
}

// WithMaxRetries는 필수 옵션이다. 지정하지 않거나 음수면 New가 오류를 반환하고 0은 재시도 없음이다.
// 소비자 자체 재시도와 겹치면 총 시도가 (소비자 시도) × (1 + retries)로 곱연산된다.
func WithMaxRetries(retries int) Option {
	return func(c *config) {
		c.maxRetries = &retries
	}
}
