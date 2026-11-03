package obsmetrics

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReadResidentMemoryBytes(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("requires /proc/self/statm")
	}

	rss, ok := readResidentMemoryBytes()
	if !ok {
		t.Fatal("readResidentMemoryBytes() ok = false on linux")
	}

	if rss == 0 {
		t.Fatal("readResidentMemoryBytes() = 0, want > 0")
	}
}

func TestParseProcessCPUSeconds(t *testing.T) {
	t.Parallel()

	// comm에 공백과 괄호가 들어가도 마지막 ')' 뒤에서 utime(250)·stime(50) tick을 읽어야 한다.
	const stat = "4242 (bot (worker) 1) S 1 4242 4242 0 -1 4194560 1000 0 0 0 250 50 0 0 20 0 8 0 12345 1000000 2000 18446744073709551615\n"

	got, ok := parseProcessCPUSeconds(stat)
	if !ok {
		t.Fatal("parseProcessCPUSeconds() ok = false")
	}

	if got != 3 {
		t.Fatalf("parseProcessCPUSeconds() = %v, want 3", got)
	}

	for _, malformed := range []string{
		"",
		"4242 bot S 1 2 3",
		"4242 (bot) S 1 4242 4242 0 -1 4194560 1000 0 0 0 x 50",
		"4242 (bot) S 1 4242",
	} {
		if _, ok := parseProcessCPUSeconds(malformed); ok {
			t.Fatalf("parseProcessCPUSeconds(%q) ok = true, want false", malformed)
		}
	}
}

func TestReadProcessCPUSeconds(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("requires /proc/self/stat")
	}

	// tick은 10ms 단위라 막 시작한 프로세스는 0일 수 있으므로 값이 오를 때까지만 CPU를 쓴다.
	deadline := time.Now().Add(5 * time.Second)

	for {
		seconds, ok := readProcessCPUSeconds()
		if !ok {
			t.Fatal("readProcessCPUSeconds() ok = false on linux")
		}

		if seconds > 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatal("readProcessCPUSeconds() stayed 0 after spending CPU")
		}

		for i := range 1_000_000 {
			_ = strings.Repeat("x", i%8)
		}
	}
}

func TestWriteRuntimeMetrics_EmitsProcessCPUCounter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc/self/stat")
	}

	resetMemStatsCache(t)

	var buf bytes.Buffer

	if !WriteRuntimeMetrics(&buf) {
		t.Fatal("WriteRuntimeMetrics() = false")
	}

	body := buf.String()

	for _, want := range []string{
		"# TYPE process_cpu_seconds_total counter",
		"\nprocess_cpu_seconds_total ",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("runtime metrics missing %q:\n%s", want, body)
		}
	}
}
