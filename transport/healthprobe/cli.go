package healthprobe

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

const cliTimeout = 4 * time.Second

func RunMain(args []string, stdout, stderr io.Writer) int {
	return runCLI(args, stdout, stderr, newCLIFetch(tlsOverridesFromEnv()))
}

type cliFetchFunc func(context.Context, string, map[string]string) ([]byte, error)

func runCLI(args []string, stdout, stderr io.Writer, fetch cliFetchFunc) int {
	if len(args) < 2 {
		return reportUsage(stderr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)

	defer cancel()

	switch args[1] {
	case "--api-key-env":
		if len(args) < 4 {
			return reportUsage(stderr)
		}

		return runHeaderChecks(ctx, args[2], args[3:], stderr, fetch)
	case "--body":
		if len(args) != 3 {
			return reportUsage(stderr)
		}

		return runBody(ctx, args[2], nil, stdout, stderr, fetch)
	case "--body-api-key-env":
		if len(args) != 4 {
			return reportUsage(stderr)
		}

		headers, code := apiKeyHeaders(args[2], stderr)
		if code != 0 {
			return code
		}

		return runBody(ctx, args[3], headers, stdout, stderr, fetch)
	default:
		return runChecks(ctx, args[1:], nil, stderr, fetch)
	}
}

func newCLIFetch(tls tlsOverrides) cliFetchFunc {
	opts := internalFetchOptions(tls)

	return func(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
		out, err := fetchURL(ctx, rawURL, headers, opts)
		if err != nil {
			return out, fmt.Errorf("fetch URL: %w", err)
		}

		return out, nil
	}
}

func runHeaderChecks(ctx context.Context, envName string, targets []string, stderr io.Writer, fetch cliFetchFunc) int {
	headers, code := apiKeyHeaders(envName, stderr)
	if code != 0 {
		return code
	}

	return runChecks(ctx, targets, headers, stderr, fetch)
}

func apiKeyHeaders(envName string, stderr io.Writer) (map[string]string, int) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		if _, err := fmt.Fprintln(stderr, "api key env name must not be empty"); err != nil {
			return nil, 1
		}

		return nil, 2
	}

	apiKey := os.Getenv(envName)
	if strings.TrimSpace(apiKey) == "" {
		return nil, reportFailure(stderr, fmt.Errorf("%s is empty or not set", envName))
	}

	return map[string]string{httputil.HeaderAPIKey: apiKey}, 0
}

func runChecks(ctx context.Context, targets []string, headers map[string]string, stderr io.Writer, fetch cliFetchFunc) int {
	group, groupCtx := errgroup.WithContext(ctx)

	for _, target := range targets {
		group.Go(func() error {
			if _, err := fetch(groupCtx, target, headers); err != nil {
				return fmt.Errorf("check %s: %w", target, err)
			}

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return reportFailure(stderr, err)
	}

	return 0
}

func runBody(ctx context.Context, rawURL string, headers map[string]string, stdout, stderr io.Writer, fetch cliFetchFunc) int {
	body, err := fetch(ctx, rawURL, headers)
	if err != nil {
		return reportFailure(stderr, err)
	}

	if _, err := stdout.Write(body); err != nil {
		return reportFailure(stderr, err)
	}

	return 0
}

func reportUsage(stderr io.Writer) int {
	if _, err := fmt.Fprintln(stderr, "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]|--body <url>|--body-api-key-env <env> <url>"); err != nil {
		return 1
	}

	return 2
}

func reportFailure(stderr io.Writer, failure error) int {
	if _, err := fmt.Fprintln(stderr, failure); err != nil {
		return 1
	}

	return 1
}
