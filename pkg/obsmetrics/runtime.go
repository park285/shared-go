package obsmetrics

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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

	return writeGCDurationSummary(w, ms)
}

func writeGCDurationSummary(w io.Writer, ms runtime.MemStats) bool {
	name := "go_gc_duration_seconds"

	if _, err := fmt.Fprintf(w, "# HELP %s A summary of the wall-time pause (stop-the-world) duration in garbage collection cycles.\n", name); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "# TYPE %s summary\n", name); err != nil {
		return false
	}

	pauseSeconds := float64(ms.PauseTotalNs) / 1e9
	if _, err := fmt.Fprintf(w, "%s_sum %s\n", name, strconv.FormatFloat(pauseSeconds, 'g', -1, 64)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "%s_count %s\n", name, strconv.FormatUint(uint64(ms.NumGC), 10)); err != nil {
		return false
	}

	return true
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

	return pages * uint64(os.Getpagesize()), true
}
