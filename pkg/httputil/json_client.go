package httputil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sharedjson "github.com/park285/shared-go/pkg/json"
)

const apiKeyHeader = "X-API-Key" //nolint:gosec // G101: 헤더 이름일 뿐 실제 credential이 아님

// JSONClient는 내부 서비스 간 JSON API 호출용 공통 HTTP 클라이언트입니다.
type JSONClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewJSONClient는 공통 internal service client를 생성합니다.
func NewJSONClient(baseURL, apiKey string, timeout time.Duration) *JSONClient {
	return &JSONClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: NewInternalServiceClient(timeout),
	}
}

// NewRequest는 body 없는 요청을 생성합니다.
func (c *JSONClient) NewRequest(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	c.applyAPIKey(req)
	return req, nil
}

// NewJSONRequest는 JSON body 요청을 생성합니다.
func (c *JSONClient) NewJSONRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	body, err := sharedjson.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAPIKey(req)
	return req, nil
}

func (c *JSONClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	return resp, nil
}

func (c *JSONClient) CheckStatus(resp *http.Response) error {
	return CheckStatus(resp)
}

func (c *JSONClient) DecodeJSON(resp *http.Response, out any) error {
	return DecodeJSON(resp, out)
}

func (c *JSONClient) DiscardBody(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("discard body: %w", err)
	}
	return nil
}

func (c *JSONClient) applyAPIKey(req *http.Request) {
	if req == nil || c.apiKey == "" {
		return
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
}
