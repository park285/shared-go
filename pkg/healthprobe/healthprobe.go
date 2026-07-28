package healthprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/park285/shared-go/pkg/h3"
	"github.com/park285/shared-go/pkg/netguard"
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
	AllowPrivateNetworks     bool
	MaxBodyBytes             int64
	FollowRedirects          bool
	ForwardHeadersOnRedirect bool
}

func defaultFetchOptions() FetchOptions {
	return FetchOptions{
		MaxBodyBytes:    DefaultMaxBodyBytes,
		FollowRedirects: true,
	}
}

func internalFetchOptions() FetchOptions {
	opts := defaultFetchOptions()
	opts.AllowPrivateNetworks = true
	return opts
}

// https는 H3(QUIC)로, http는 HTTP/1.1로 1회 GET 후 2xx 여부를 검사한다.
func CheckURL(rawURL string) error {
	_, err := FetchURL(rawURL)
	return err
}

func FetchURL(rawURL string) ([]byte, error) {
	return fetchURL(context.Background(), rawURL, nil, defaultFetchOptions())
}

func CheckURLInternal(rawURL string) error {
	_, err := FetchURLInternal(rawURL)
	return err
}

func FetchURLInternal(rawURL string) ([]byte, error) {
	return fetchURL(context.Background(), rawURL, nil, internalFetchOptions())
}

func FetchURLWithHeadersInternal(rawURL string, headers map[string]string) ([]byte, error) {
	return fetchURL(context.Background(), rawURL, headers, internalFetchOptions())
}

func fetchURL(parent context.Context, rawURL string, headers map[string]string, opts FetchOptions) ([]byte, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate url: %w", err)
	}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
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
	if !opts.AllowPrivateNetworks {
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
	if netguard.IsBlockedIP(ip) {
		return fmt.Errorf("%w: %w: dialed %s", ErrPrivateNetwork, netguard.ErrBlockedIP, ip)
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

func redirectPolicy(opts FetchOptions) func(req *http.Request, via []*http.Request) error {
	policy := netguard.RedirectPolicy(netguard.RedirectConfig{
		Policy:         networkPolicy(opts),
		MaxRedirects:   maxRedirects - 1,
		DisableFollow:  !opts.FollowRedirects,
		ForwardHeaders: opts.ForwardHeadersOnRedirect,
	})
	return func(req *http.Request, via []*http.Request) error {
		return mapPolicyError(policy(req, via))
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
	return mapPolicyError(networkPolicy(opts).ValidateTarget(ctx, parsed))
}

func networkPolicy(opts FetchOptions) netguard.Policy {
	policy := netguard.Policy{
		Resolver:     healthprobeResolver{},
		Timeout:      requestTimeout,
		AllowedHosts: opts.AllowedHosts,
		Schemes:      []string{"http", "https"},
	}
	if opts.AllowPrivateNetworks {
		policy.AllowedIPPrefixes = []netip.Prefix{
			netip.MustParsePrefix("0.0.0.0/0"),
			netip.MustParsePrefix("::/0"),
		}
	}
	return policy
}

func mapPolicyError(err error) error {
	switch {
	case errors.Is(err, netguard.ErrBlockedIP):
		return fmt.Errorf("%w: %w", ErrPrivateNetwork, err)
	case errors.Is(err, netguard.ErrHostNotAllowed):
		return fmt.Errorf("%w: %w", ErrHostNotAllowed, err)
	case errors.Is(err, netguard.ErrTooManyRedirects):
		return fmt.Errorf("%w: %w", ErrTooManyRedirects, err)
	default:
		return err
	}
}

var lookupIPAddr = net.DefaultResolver.LookupIPAddr

type healthprobeResolver struct{}

func (healthprobeResolver) LookupIP(ctx context.Context, _ string, host string) ([]net.IP, error) {
	addrs, err := lookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func isPrivateIP(ip net.IP) bool {
	return netguard.IsBlockedIP(ip)
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
