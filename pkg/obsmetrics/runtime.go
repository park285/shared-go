package obsmetrics

import (
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/promtext"
)

// runtime.ReadMemStats는 stop-the-world라 scrape가 겹치면 그만큼 전체 정지가 반복된다.
// 짧은 TTL 안에서는 직전 스냅샷을 재사용해 scraper 수와 무관하게 STW 빈도를 묶는다.
const memStatsTTL = time.Second

var memStats struct {
	mu       sync.Mutex
	snapshot runtime.MemStats
	readAt   time.Time
}

func cachedMemStats(now time.Time) runtime.MemStats {
	memStats.mu.Lock()
	defer memStats.mu.Unlock()

	if !memStats.readAt.IsZero() && now.Sub(memStats.readAt) < memStatsTTL {
		return memStats.snapshot
	}

	runtime.ReadMemStats(&memStats.snapshot)

	memStats.readAt = now

	return memStats.snapshot
}

// WriteRuntimeMetrics는 Go 런타임/프로세스 메트릭(go_*, process_*)을 텍스트 포맷으로 씁니다.
// MemStats 계열 값은 최대 1초까지 캐시된 스냅샷일 수 있습니다.
func WriteRuntimeMetrics(w io.Writer) bool {
	ms := cachedMemStats(time.Now())

	gauges := []struct {
		name  string
		help  string
		value string
	}{
		{"go_goroutines", "Number of goroutines that currently exist.", strconv.Itoa(runtime.NumGoroutine())},
		{"go_heap_alloc_bytes", "Bytes of allocated heap objects.", strconv.FormatUint(ms.HeapAlloc, 10)},
		{"go_heap_goal_bytes", "Heap size target for the end of the next GC cycle.", strconv.FormatUint(ms.NextGC, 10)},
		{"go_memstats_next_gc_bytes", "Number of heap bytes when next garbage collection will take place.", strconv.FormatUint(ms.NextGC, 10)},
	}

	if rss, ok := readResidentMemoryBytes(); ok {
		gauges = append(gauges, struct {
			name  string
			help  string
			value string
		}{"process_resident_memory_bytes", "Resident memory size in bytes.", strconv.FormatUint(rss, 10)})
	}

	for _, g := range gauges {
		if !WriteGauge(w, g.name, g.help, g.value) {
			return false
		}
	}

	if seconds, ok := readProcessCPUSeconds(); ok {
		if !writeScalar(w, "process_cpu_seconds_total", "Total user and system CPU time spent in seconds.", "counter", promtext.FormatFloat(seconds)) {
			return false
		}
	}

	return writeGCDurationSummary(w, ms)
}

func writeGCDurationSummary(w io.Writer, ms runtime.MemStats) bool {
	name := "go_gc_duration_seconds"

	if !writeMetricHeader(w, name, "A summary of the wall-time pause (stop-the-world) duration in garbage collection cycles.", "summary") {
		return false
	}

	pauseSeconds := float64(ms.PauseTotalNs) / 1e9
	if !writeMetricSample(w, name+"_sum", nil, promtext.FormatFloat(pauseSeconds)) {
		return false
	}

	return writeMetricSample(w, name+"_count", nil, strconv.FormatUint(uint64(ms.NumGC), 10))
}

// /proc/<pid>/stat의 utime·stime은 USER_HZ tick 단위다. USER_HZ는 x86·arm64 커널 ABI에서 100으로
// 고정돼 있고 client_golang procfs도 같은 값을 쓴다.
const userHZ = 100

// /proc/self/stat의 utime+stime을 초로 돌려준다. Linux 외 환경이나 읽기 실패 시 metric을 생략한다.
func readProcessCPUSeconds() (float64, bool) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}

	return parseProcessCPUSeconds(string(data))
}

// comm(2번째 필드)은 공백과 괄호를 포함할 수 있으므로 마지막 ')' 뒤부터 센다. 그 뒤 첫 필드가
// 3번째(state)이므로 utime(14번째)·stime(15번째)은 인덱스 11·12다.
func parseProcessCPUSeconds(stat string) (float64, bool) {
	_, afterComm, found := strings.CutLast(stat, ")")
	if !found {
		return 0, false
	}

	fields := strings.Fields(afterComm)
	if len(fields) < 13 {
		return 0, false
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, false
	}

	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, false
	}

	return float64(utime+stime) / userHZ, true
}

// /proc/self/statm 두 번째 필드는 RSS 페이지 수다. Linux 외 환경에서는 metric을 생략한다.
func readResidentMemoryBytes() (uint64, bool) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}

	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}

	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}

	pageSize := os.Getpagesize()
	if pageSize <= 0 {
		return 0, false
	}

	return pages * uint64(pageSize), true
}
