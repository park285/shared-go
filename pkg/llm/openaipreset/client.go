package openaipreset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/park285/shared-go/pkg/httputil"
	sharedjson "github.com/park285/shared-go/pkg/json"
	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/logging"
)

const (
	providerLabel         = "openai"
	defaultSchemaName     = "response"
	defaultRequestTimeout = 120 * time.Second
)

var errClientNil = errors.New("openaipreset: client is nil")

type Client struct {
	generator       sharedllm.JSONGenerator
	openai          openai.Client
	model           string
	schemaName      string
	temperature     *float64
	reasoningEffort string
	webSearch       bool
	chatCompletions bool
	usageReporter   sharedllm.UsageReporter
	logger          *slog.Logger
}

func New(baseURL, apiKey, model string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("openaipreset: model is empty")
	}

	cfg := &config{
		schemaName:                   defaultSchemaName,
		allowChatCompletionsFallback: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	requestOpts := []option.RequestOption{option.WithAPIKey(strings.TrimSpace(apiKey))}
	if normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalizedBaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(normalizedBaseURL))
	}
	requestOpts = append(requestOpts, option.WithHTTPClient(httpClient))

	generator, err := sharedllm.NewOpenAICompatibleJSONGenerator(sharedllm.OpenAICompatibleConfig{
		BaseURL:                      baseURL,
		APIKey:                       apiKey,
		HTTPClient:                   httpClient,
		AllowChatCompletionsFallback: cfg.allowChatCompletionsFallback,
	})
	if err != nil {
		return nil, err
	}

	reporter := cfg.usageReporter
	if reporter == nil {
		reporter = sharedllm.NoopUsageReporter{}
	}

	return &Client{
		generator:       generator,
		openai:          openai.NewClient(requestOpts...),
		model:           strings.TrimSpace(model),
		schemaName:      cfg.schemaName,
		temperature:     cfg.temperature,
		reasoningEffort: cfg.reasoningEffort,
		webSearch:       cfg.webSearch,
		chatCompletions: cfg.chatCompletions,
		usageReporter:   reporter,
		logger:          cfg.logger,
	}, nil
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
	resp, err := c.generate(ctx, c.schemaName, systemPrompt, userPrompt, schema)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (c *Client) RunInto(ctx context.Context, task, prompt string, schema map[string]any, out any) error {
	if c == nil {
		return errClientNil
	}
	if out == nil {
		return errors.New("openaipreset: output target is nil")
	}
	resp, err := c.generate(ctx, task, "", prompt, schema)
	if err != nil {
		return err
	}
	if err := sharedjson.Unmarshal([]byte(resp.Text), out); err != nil {
		return fmt.Errorf("decode %s json failed: %w; output=%s", strings.TrimSpace(task), err, sharedllm.RedactDiagnostic(resp.Text, 2048))
	}
	return nil
}

func (c *Client) generate(ctx context.Context, taskName, systemPrompt, userPrompt string, schema map[string]any) (sharedllm.JSONResponse, error) {
	attrs := promptSummaryAttrs(c.model, systemPrompt, userPrompt)
	logging.Info(ctx, c.logger, "llm.request.started", "llm request started", attrs...)
	started := time.Now()

	resp, err := sharedllm.RunJSON(ctx, c.generator, sharedllm.JSONRequest{
		TaskName:        taskName,
		SystemPrompt:    systemPrompt,
		UserPrompt:      userPrompt,
		SchemaName:      taskName,
		Schema:          schema,
		Model:           c.model,
		Temperature:     c.temperature,
		ReasoningEffort: c.reasoningEffort,
		WebSearch:       c.webSearch,
		ChatCompletions: c.chatCompletions,
	}, providerLabel, c.usageReporter)

	elapsed := logging.SinceMS(started)
	if err != nil {
		logging.Error(ctx, c.logger, "llm.request.failed", "llm request failed", append(attrs, elapsed)...)
		return sharedllm.JSONResponse{}, err
	}
	logging.Info(ctx, c.logger, "llm.request.succeeded", "llm request succeeded", append(attrs, elapsed)...)
	return resp, nil
}

func promptSummaryAttrs(model, systemPrompt, userPrompt string) []slog.Attr {
	prompt := strings.TrimSpace(systemPrompt + "\n" + userPrompt)
	attrs := []slog.Attr{
		slog.String("provider", providerLabel),
		slog.String("model", model),
		slog.Int("prompt_len", len(prompt)),
	}
	if prompt != "" {
		sum := sha256.Sum256([]byte(prompt))
		attrs = append(attrs, slog.String("prompt_sha256_8", hex.EncodeToString(sum[:8])))
	}
	return attrs
}
