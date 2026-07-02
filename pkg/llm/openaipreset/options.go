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
}

type Option func(*config)

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
