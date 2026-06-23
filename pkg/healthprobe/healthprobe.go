package healthprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/park285/shared-go/pkg/h3"
)

const (
	ServerNameEnv = "HEALTHCHECK_SERVER_NAME"
	CACertFileEnv = "HEALTHCHECK_CA_CERT_FILE"

	requestTimeout = 5 * time.Second

	DefaultMaxBodyBytes int64 = 64 << 10
	maxRedirects              = 5
)

var (
	ErrBodyTooLarge     = errors.New("healthprobe: response body exceeds limit")
	ErrHostNotAllowed   = errors.New("healthprobe: host not in allowlist")
	ErrPrivateNetwork   = errors.New("healthprobe: target resolves to a private or loopback address")
	ErrTooManyRedirects = errors.New("healthprobe: too many redirects")
)

type FetchOptions struct {
	AllowedHosts             []string
	RestrictPrivateNetworks  bool
	MaxBodyBytes             int64
	FollowRedirects          bool
	ForwardHeadersOnRedirect bool
}

func defaultFetchOptions() FetchOptions {
	return FetchOptions{
		RestrictPrivateNetworks: true,
		MaxBodyBytes:            DefaultMaxBodyBytes,
		FollowRedirects:         true,
	}
}

func internalFetchOptions() FetchOptions {
	opts := defaultFetchOptions()
	opts.RestrictPrivateNetworks = false
	return opts
}

// https는 H3(QUIC)로, http는 HTTP/1.1로 1회 GET 후 2xx 여부를 검사한다.
func CheckURL(rawURL string) error {
	_, err := FetchURL(rawURL)
	return err
}

func FetchURL(rawURL string) ([]byte, error) {
	return fetchURL(rawURL, nil, defaultFetchOptions())
}

func FetchURLWithHeaders(rawURL string, headers map[string]string) ([]byte, error) {
	return fetchURL(rawURL, headers, defaultFetchOptions())
}

func CheckURLInternal(rawURL string) error {
	_, err := FetchURLInternal(rawURL)
	return err
}

func FetchURLInternal(rawURL string) ([]byte, error) {
	return fetchURL(rawURL, nil, internalFetchOptions())
}

func FetchURLWithHeadersInternal(rawURL string, headers map[string]string) ([]byte, error) {
	return fetchURL(rawURL, headers, internalFetchOptions())
}

func FetchURLWithOptions(rawURL string, headers map[string]string, opts FetchOptions) ([]byte, error) {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	return fetchURL(rawURL, headers, opts)
}

func fetchURL(rawURL string, headers map[string]string, opts FetchOptions) ([]byte, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate url: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if authErr := authorizeTarget(ctx, parsed, opts); authErr != nil {
		return nil, authErr
	}

	client, closeFn, err := newClient(parsed, opts)
	if err != nil {
		return nil, err
	}
	defer closeFn()

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

	body, err := readCappedBody(resp.Body, opts.MaxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", rawURL, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s status: %d", rawURL, resp.StatusCode)
	}

	return body, nil
}

func newClient(parsed *url.URL, opts FetchOptions) (*http.Client, func(), error) {
	var guard func(net.IP) error
	if opts.RestrictPrivateNetworks {
		guard = dialGuard
	}

	if parsed.Scheme == "https" {
		h3Client, closeFn, clientErr := h3.NewClient(0, h3.ClientOptions{
			CACertFile: os.Getenv(CACertFileEnv),
			ServerName: os.Getenv(ServerNameEnv),
			DialGuard:  guard,
		})
		if clientErr != nil {
			return nil, nil, clientErr
		}
		h3Client.CheckRedirect = redirectPolicy(opts)
		return h3Client, closeFn, nil
	}

	client := &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: redirectPolicy(opts),
	}
	if guard != nil {
		client.Transport = guardedHTTPTransport(guard)
	}
	return client, func() {}, nil
}

func dialGuard(ip net.IP) error {
	if isPrivateIP(ip) {
		return fmt.Errorf("%w: dialed %s", ErrPrivateNetwork, ip)
	}
	return nil
}

func guardedHTTPTransport(guard func(net.IP) error) *http.Transport {
	dialer := &net.Dialer{Timeout: requestTimeout}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: parse dial addr %q: %w", ErrPrivateNetwork, address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%w: unresolved dial addr %q", ErrPrivateNetwork, address)
		}
		return guard(ip)
	}
	return &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   requestTimeout,
		ExpectContinueTimeout: time.Second,
	}
}

// redirectPolicy는 redirect를 따를지/얼마나/헤더를 유지할지 강제한다. cross-host redirect에서
// custom header(예: Authorization, X-API-Key)가 다른 host로 따라가는 누출을 막는다.
func redirectPolicy(opts FetchOptions) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !opts.FollowRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= maxRedirects {
			return ErrTooManyRedirects
		}
		if err := authorizeTarget(req.Context(), req.URL, opts); err != nil {
			return err
		}

		if opts.ForwardHeadersOnRedirect {
			return nil
		}

		previous := via[len(via)-1].URL
		if !sameHost(previous, req.URL) {
			stripSensitiveHeaders(req)
		}
		return nil
	}
}

func sameHost(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname())
}

func stripSensitiveHeaders(req *http.Request) {
	for name := range req.Header {
		req.Header.Del(name)
	}
}

func readCappedBody(body io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return data, nil
}

func authorizeTarget(ctx context.Context, parsed *url.URL, opts FetchOptions) error {
	host := parsed.Hostname()
	if len(opts.AllowedHosts) > 0 && !hostAllowed(host, opts.AllowedHosts) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
	}
	if opts.RestrictPrivateNetworks {
		if err := rejectPrivateNetwork(ctx, host); err != nil {
			return err
		}
	}
	return nil
}

func hostAllowed(host string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(host, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

var lookupIPAddr = net.DefaultResolver.LookupIPAddr

func rejectPrivateNetwork(ctx context.Context, host string) error {
	addrs, err := lookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %s resolves to no address", ErrPrivateNetwork, host)
	}
	for _, addr := range addrs {
		if isPrivateIP(addr.IP) {
			return fmt.Errorf("%w: %s -> %s", ErrPrivateNetwork, host, addr.IP)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

func parseURL(raw string) (*url.URL, error) {
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
