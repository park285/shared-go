package openaipreset

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/park285/shared-go/v2/pkg/httputil"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

const (
	providerLabel         = "openai"
	defaultSchemaName     = "response"
	defaultRequestTimeout = 120 * time.Second
)

var errClientNil = errors.New("openaipreset: client is nil")

// ErrResponsesJSONRequired는 strict Responses JSON contract를 fallback 없이 충족할 수 없을 때 반환된다.
var ErrResponsesJSONRequired = errors.New("openaipreset: responses JSON transport required")

type Client struct {
	generator                    sharedllm.JSONGenerator
	openai                       openai.Client
	model                        string
	schemaName                   string
	temperature                  *float64
	reasoningEffort              string
	webSearch                    bool
	chatCompletions              bool
	allowChatCompletionsFallback bool
	usageReporter                sharedllm.UsageReporter
	logger                       *slog.Logger
	promptCacheKeyPrefix         string
}

type PromptLayers struct {
	Invariant string
	Developer string
	User      string
}

func New(baseURL, apiKey, model string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("openaipreset: model is empty")
	}

	cfg := &config{schemaName: defaultSchemaName}

	for _, opt := range opts {
		opt(cfg)
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	requestOpts := []option.RequestOption{
		option.WithAPIKey(strings.TrimSpace(apiKey)),
		option.WithMaxRetries(sharedllm.ResolveOpenAIMaxRetries(cfg.maxRetries)),
	}
	if normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalizedBaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(normalizedBaseURL))
	}

	requestOpts = append(requestOpts, option.WithHTTPClient(httpClient))

	generator, err := sharedllm.NewOpenAICompatibleJSONGenerator(sharedllm.OpenAICompatibleConfig{
		BaseURL:                      baseURL,
		APIKey:                       apiKey,
		HTTPClient:                   httpClient,
		AllowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
		MaxRetries:                   cfg.maxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("open AI compatible JSON generator: %w", err)
	}

	reporter := cfg.usageReporter
	if reporter == nil {
		reporter = sharedllm.NoopUsageReporter{}
	}

	return &Client{
		generator:                    generator,
		openai:                       openai.NewClient(requestOpts...),
		model:                        strings.TrimSpace(model),
		schemaName:                   cfg.schemaName,
		temperature:                  cfg.temperature,
		reasoningEffort:              cfg.reasoningEffort,
		webSearch:                    cfg.webSearch,
		chatCompletions:              cfg.chatCompletions,
		allowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
		usageReporter:                reporter,
		logger:                       cfg.logger,
		promptCacheKeyPrefix:         cfg.promptCacheKeyPrefix,
	}, nil
}

func (c *Client) promptCacheKeyFor(task string) string {
	if c.promptCacheKeyPrefix == "" {
		return ""
	}

	return c.promptCacheKeyPrefix + strings.TrimSpace(task)
}

// reasoning·web-search 모델은 첫 응답 헤더까지 수십 초가 걸려 externalAPI 프로파일의 15s로는 끊긴다.
func defaultHTTPClient() *http.Client {
	return httputil.NewProfiledClient(httputil.TransportProfile{
		Timeout:               defaultRequestTimeout,
		DialTimeout:           5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: defaultRequestTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          128,
		MaxConnsPerHost:       32,
		MaxIdleConnsPerHost:   16,
	})
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}

	return c.model
}

func (c *Client) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c == nil {
		return "", errClientNil
	}

	resp, err := c.generate(ctx, c.schemaName, systemPrompt, "", "", userPrompt, schema)
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}

	return resp.Text, nil
}

func (c *Client) GenerateJSONAs[T any](
	ctx context.Context,
	task string,
	prompts PromptLayers,
	schema map[string]any,
) (T, error) {
	var zero T

	if c == nil {
		return zero, errClientNil
	}

	resp, err := c.generate(ctx, task, "", prompts.Invariant, prompts.Developer, prompts.User, schema)
	if err != nil {
		return zero, fmt.Errorf("generate: %w", err)
	}

	return decodeJSONAs[T](task, resp.Text)
}
