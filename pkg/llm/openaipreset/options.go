package openaipreset

import (
	"log/slog"
	"net/http"
	"strings"

	sharedllm "github.com/park285/shared-go/pkg/llm"
)

type config struct {
	schemaName                   string
	temperature                  *float64
	reasoningEffort              string
	webSearch                    bool
	chatCompletions              bool
	allowChatCompletionsFallback bool
	httpClient                   *http.Client
	usageReporter                sharedllm.UsageReporter
	logger                       *slog.Logger
	maxRetries                   *int
	promptCacheKeyPrefix         string
}

type Option func(*config)

// WithPromptCacheKeyPrefix는 요청마다 prefix+task를 prompt_cache_key로 전송한다.
// GPT-5.6+는 이 키가 있어야 prefix 캐시 매칭이 성립한다.
func WithPromptCacheKeyPrefix(prefix string) Option {
	return func(c *config) {
		c.promptCacheKeyPrefix = strings.TrimSpace(prefix)
	}
}

func WithSchemaName(name string) Option {
	return func(c *config) {
		if strings.TrimSpace(name) != "" {
			c.schemaName = name
		}
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

func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func WithAllowChatCompletionsFallback(enabled bool) Option {
	return func(c *config) {
		c.allowChatCompletionsFallback = enabled
	}
}

// 미지정 시 sharedllm.DefaultOpenAIMaxRetries(2)가 적용되며, 소비자 자체 재시도와
// 겹치면 총 시도가 (소비자 시도) × (1 + retries)로 곱연산된다. 음수는 0으로 절단된다.
func WithMaxRetries(retries int) Option {
	return func(c *config) {
		c.maxRetries = &retries
	}
}
