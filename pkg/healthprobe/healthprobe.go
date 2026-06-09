package healthprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/park285/shared-go/pkg/h3"
)

const (
	ServerNameEnv = "HEALTHCHECK_SERVER_NAME"
	CACertFileEnv = "HEALTHCHECK_CA_CERT_FILE"

	requestTimeout = 5 * time.Second
)

// https는 H3(QUIC)로, http는 HTTP/1.1로 1회 GET 후 2xx 여부를 검사한다.
func CheckURL(rawURL string) error {
	_, err := FetchURL(rawURL)
	return err
}

func FetchURL(rawURL string) ([]byte, error) {
	return fetchURL(rawURL, nil)
}

func FetchURLWithHeaders(rawURL string, headers map[string]string) ([]byte, error) {
	return fetchURL(rawURL, headers)
}

func fetchURL(rawURL string, headers map[string]string) ([]byte, error) {
	parsed, err := ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate url: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	client := http.DefaultClient
	if parsed.Scheme == "https" {
		h3Client, closeFn, clientErr := h3.NewClient(0, h3.ClientOptions{
			CACertFile: os.Getenv(CACertFileEnv),
			ServerName: os.Getenv(ServerNameEnv),
		})
		if clientErr != nil {
			return nil, clientErr
		}
		defer closeFn()
		client = h3Client
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for name, value := range headers {
		if name == "" {
			continue
		}
		req.Header.Set(name, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", rawURL, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", rawURL, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s status: %d", rawURL, resp.StatusCode)
	}

	return body, nil
}

func ParseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, errors.New("url missing host")
	}

	return parsed, nil
}
