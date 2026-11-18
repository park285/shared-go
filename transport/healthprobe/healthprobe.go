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
	"strings"
	"time"

	"github.com/park285/shared-go/transport/h3"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/netguard"
)

const (
	serverNameEnv     = "HEALTHCHECK_SERVER_NAME"
	caCertFileEnv     = "HEALTHCHECK_CA_CERT_FILE"
	clientCertFileEnv = "HEALTHCHECK_CLIENT_CERT_FILE"
	clientKeyFileEnv  = "HEALTHCHECK_CLIENT_KEY_FILE"

	requestTimeout = 5 * time.Second

	defaultMaxBodyBytes int64 = 64 << 10
	maxRedirects              = 5
)

var (
	errBodyTooLarge         = errors.New("healthprobe: response body exceeds limit")
	errTooManyRedirects     = errors.New("healthprobe: too many redirects")
	errCredentialedRedirect = errors.New("healthprobe: client certificate redirect changes origin")
)

type tlsOverrides struct {
	CACertFile     string
	ClientCertFile string
	ClientKeyFile  string
	ServerName     string
}

func tlsOverridesFromEnv() tlsOverrides {
	return tlsOverrides{
		CACertFile:     os.Getenv(caCertFileEnv),
		ClientCertFile: os.Getenv(clientCertFileEnv),
		ClientKeyFile:  os.Getenv(clientKeyFileEnv),
		ServerName:     os.Getenv(serverNameEnv),
	}
}

type fetchOptions struct {
	TLS                      tlsOverrides
	MaxBodyBytes             int64
	FollowRedirects          bool
	ForwardHeadersOnRedirect bool
}

func internalFetchOptions(tls tlsOverrides) fetchOptions {
	return fetchOptions{
		TLS:             tls,
		MaxBodyBytes:    defaultMaxBodyBytes,
		FollowRedirects: true,
	}
}

func fetchURL(parent context.Context, rawURL string, headers map[string]string, opts fetchOptions) ([]byte, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate url: %w", err)
	}

	ctx, cancel := context.WithTimeout(parent, requestTimeout)

	defer cancel()

	if authErr := authorizeTarget(ctx, parsed); authErr != nil {
		return nil, fmt.Errorf("authorize target: %w", authErr)
	}

	client, closeFn, err := newClient(parsed, opts)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
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

	resp, err := client.Do(req) //nolint:bodyclose // readCappedBody가 고정된 httputil close 소유자에 넘겨 일반 반환과 panic에서 한 번만 닫는다.
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", rawURL, err)
	}

	body, err := readCappedBody(resp.Body, opts.MaxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", rawURL, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s status: %d", rawURL, resp.StatusCode)
	}

	return body, nil
}

func newClient(parsed *url.URL, opts fetchOptions) (*http.Client, func(), error) {
	if parsed.Scheme == "https" {
		clientCertFile := opts.TLS.ClientCertFile
		clientKeyFile := opts.TLS.ClientKeyFile

		h3Client, closeFn, clientErr := h3.NewClient(0, h3.ClientOptions{
			CACertFile:     opts.TLS.CACertFile,
			ClientCertFile: clientCertFile,
			ClientKeyFile:  clientKeyFile,
			ServerName:     opts.TLS.ServerName,
		})
		if clientErr != nil {
			return nil, nil, fmt.Errorf("client: %w", clientErr)
		}

		var credentialOrigin *url.URL

		if clientCertFile != "" || clientKeyFile != "" {
			credentialOrigin = parsed
		}

		h3Client.CheckRedirect = redirectPolicy(opts, credentialOrigin)

		return h3Client, closeFn, nil
	}

	client := &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: redirectPolicy(opts, nil),
	}

	// 공유 http.DefaultTransport의 pool은 다른 소비자 것이므로 여기서 닫지 않는다.
	return client, func() {}, nil
}

func redirectPolicy(opts fetchOptions, credentialOrigin *url.URL) func(req *http.Request, via []*http.Request) error {
	policy := netguard.RedirectPolicy(netguard.RedirectConfig{
		Policy:         networkPolicy(),
		MaxRedirects:   maxRedirects - 1,
		DisableFollow:  !opts.FollowRedirects,
		ForwardHeaders: opts.ForwardHeadersOnRedirect,
	})

	return func(req *http.Request, via []*http.Request) error {
		if credentialOrigin != nil && !sameOrigin(credentialOrigin, req.URL) {
			return errCredentialedRedirect
		}

		return mapPolicyError(policy(req, via))
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}

	if strings.EqualFold(target.Scheme, "https") {
		return "443"
	}

	if strings.EqualFold(target.Scheme, "http") {
		return "80"
	}

	return ""
}

// body의 close 소유권은 httputil.ReadAllAndClose에 있다. 호출부는 따로 닫지 않는다.
func readCappedBody(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBytes
	}

	data, err := httputil.ReadAllAndClose(body, maxBytes)
	if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		return nil, errBodyTooLarge
	}

	if err != nil {
		return nil, fmt.Errorf("read all and close: %w", err)
	}

	return data, nil
}

func authorizeTarget(ctx context.Context, parsed *url.URL) error {
	if err := mapPolicyError(networkPolicy().ValidateTarget(ctx, parsed)); err != nil {
		return fmt.Errorf("map policy error: %w", err)
	}

	return nil
}

func networkPolicy() netguard.Policy {
	return netguard.Policy{
		Resolver: healthprobeResolver{},
		Timeout:  requestTimeout,
		Schemes:  []string{"http", "https"},
		AllowedIPPrefixes: []netip.Prefix{
			netip.MustParsePrefix("0.0.0.0/0"),
			netip.MustParsePrefix("::/0"),
		},
	}
}

func mapPolicyError(err error) error {
	if errors.Is(err, netguard.ErrTooManyRedirects) {
		return fmt.Errorf("%w: %w", errTooManyRedirects, err)
	}

	return err
}

var lookupIPAddr = net.DefaultResolver.LookupIPAddr

type healthprobeResolver struct{}

func (healthprobeResolver) LookupIP(ctx context.Context, _, host string) ([]net.IP, error) {
	addrs, err := lookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("healthprobe: lookup IP addr: %w", err)
	}

	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}

	return ips, nil
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
