package healthprobe

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
)

var smokeTimezones = []string{"Asia/Seoul", "Asia/Tokyo", "UTC"}

// RunMain은 헬스체크 CLI 진입점이다. args는 os.Args(프로그램명 포함)이며 프로세스 종료 코드를 반환한다.
// CLI는 운영자가 통제하는 자기 서비스 loopback/내부망 probe라 SSRF 기본 차단을 우회한다.
func RunMain(args []string, stdout, stderr io.Writer) int {
	return runCLI(args, stdout, stderr, cliFetch)
}

type cliFetchFunc func(string, map[string]string) ([]byte, error)

func runMain(args []string, stdout, stderr io.Writer, checkURL func(string) error) int {
	return runCLI(args, stdout, stderr, func(rawURL string, _ map[string]string) ([]byte, error) {
		return nil, checkURL(rawURL)
	})
}

func runCLI(args []string, stdout, stderr io.Writer, fetch cliFetchFunc) int {
	if len(args) == 2 && args[1] == "--smoke" {
		return runSmoke(stdout, stderr)
	}
	if len(args) < 2 {
		return reportUsage(stderr)
	}

	switch args[1] {
	case "--api-key-env":
		if len(args) < 4 {
			return reportUsage(stderr)
		}
		return runHeaderChecks(args[2], args[3:], stderr, fetch)
	case "--body":
		if len(args) != 3 {
			return reportUsage(stderr)
		}
		return runBody(args[2], nil, stdout, stderr, fetch)
	case "--body-api-key-env":
		if len(args) != 4 {
			return reportUsage(stderr)
		}
		headers, code := apiKeyHeaders(args[2], stderr)
		if code != 0 {
			return code
		}
		return runBody(args[3], headers, stdout, stderr, fetch)
	default:
		return runChecks(args[1:], nil, stderr, fetch)
	}
}

func cliFetch(rawURL string, headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return FetchURLInternal(rawURL)
	}
	return FetchURLWithHeadersInternal(rawURL, headers)
}

func runHeaderChecks(envName string, targets []string, stderr io.Writer, fetch cliFetchFunc) int {
	headers, code := apiKeyHeaders(envName, stderr)
	if code != 0 {
		return code
	}
	return runChecks(targets, headers, stderr, fetch)
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

func runChecks(targets []string, headers map[string]string, stderr io.Writer, fetch cliFetchFunc) int {
	for _, target := range targets {
		if _, err := fetch(target, headers); err != nil {
			return reportFailure(stderr, err)
		}
	}
	return 0
}

func runBody(rawURL string, headers map[string]string, stdout, stderr io.Writer, fetch cliFetchFunc) int {
	body, err := fetch(rawURL, headers)
	if err != nil {
		return reportFailure(stderr, err)
	}
	if _, err := stdout.Write(body); err != nil {
		return reportFailure(stderr, err)
	}
	return 0
}

func reportUsage(stderr io.Writer) int {
	if _, err := fmt.Fprintln(stderr, "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]|--body <url>|--body-api-key-env <env> <url>|--smoke"); err != nil {
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

func runSmoke(stdout, stderr io.Writer) int {
	for _, name := range smokeTimezones {
		if _, err := time.LoadLocation(name); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "load location %s: %v\n", name, err); writeErr != nil {
				return 1
			}

			return 1
		}
	}
	if _, err := fmt.Fprintln(stdout, "smoke ok"); err != nil {
		return 1
	}

	return 0
}
