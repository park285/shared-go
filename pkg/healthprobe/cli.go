package healthprobe

import (
	"fmt"
	"io"
	"time"
)

var smokeTimezones = []string{"Asia/Seoul", "Asia/Tokyo", "UTC"}

// RunMain은 헬스체크 CLI 진입점이다. args는 os.Args(프로그램명 포함)이며 프로세스 종료 코드를 반환한다.
// CLI는 운영자가 통제하는 자기 서비스 loopback/내부망 probe라 SSRF 기본 차단을 우회한다.
func RunMain(args []string, stdout, stderr io.Writer) int {
	return runMain(args, stdout, stderr, CheckURLInternal)
}

func runMain(args []string, stdout, stderr io.Writer, checkURL func(string) error) int {
	if len(args) == 2 && args[1] == "--smoke" {
		return runSmoke(stdout, stderr)
	}
	if len(args) != 2 {
		if _, err := fmt.Fprintln(stderr, "usage: healthcheck <url>|--smoke"); err != nil {
			return 1
		}

		return 2
	}
	if err := checkURL(args[1]); err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return 1
		}

		return 1
	}

	return 0
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
