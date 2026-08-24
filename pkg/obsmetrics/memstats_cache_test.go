package obsmetrics

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetMemStatsCache(t *testing.T) {
	t.Helper()

	memStats.mu.Lock()

	memStats.readAt = time.Time{}
	memStats.snapshot = runtime.MemStats{}
	memStats.mu.Unlock()
}

func TestCachedMemStats_ReusesSnapshotWithinTTL(t *testing.T) {
	resetMemStatsCache(t)

	base := time.Now()
	first := cachedMemStats(base)

	if first.NextGC == 0 {
		t.Fatal("first read returned an empty snapshot")
	}

	// 힙을 실제로 늘려도 TTL 안에서는 같은 스냅샷이어야 캐시가 동작한 것이다.
	sink := make([]byte, 8<<20)
	for i := range sink {
		sink[i] = byte(i)
	}

	t.Cleanup(func() { _ = sink[0] })

	cached := cachedMemStats(base.Add(memStatsTTL - time.Millisecond))
	if cached.TotalAlloc != first.TotalAlloc || cached.NumGC != first.NumGC {
		t.Fatalf("snapshot changed within TTL: TotalAlloc %d -> %d, NumGC %d -> %d",
			first.TotalAlloc, cached.TotalAlloc, first.NumGC, cached.NumGC)
	}

	refreshed := cachedMemStats(base.Add(memStatsTTL))
	if refreshed.TotalAlloc == first.TotalAlloc {
		t.Fatalf("snapshot not refreshed past TTL: TotalAlloc stayed %d", first.TotalAlloc)
	}
}

func TestCachedMemStats_ConcurrentScrapersAreSafe(t *testing.T) {
	resetMemStatsCache(t)

	const scrapers = 32

	var wg sync.WaitGroup

	wg.Add(scrapers)

	start := make(chan struct{})
	now := time.Now()

	for i := range scrapers {
		go func(i int) {
			defer wg.Done()

			<-start

			got := cachedMemStats(now.Add(time.Duration(i) * time.Millisecond))
			if got.NextGC == 0 {
				t.Errorf("scraper %d got an empty snapshot", i)
			}
		}(i)
	}

	close(start)
	wg.Wait()
}

func TestWriteRuntimeMetrics_EmitsMemStatsFamilies(t *testing.T) {
	resetMemStatsCache(t)

	var buf bytes.Buffer

	if !WriteRuntimeMetrics(&buf) {
		t.Fatal("WriteRuntimeMetrics() = false")
	}

	body := buf.String()

	for _, want := range []string{
		"# TYPE go_goroutines gauge",
		"# TYPE go_heap_alloc_bytes gauge",
		"# TYPE go_memstats_next_gc_bytes gauge",
		"# TYPE go_gc_duration_seconds summary",
		"go_gc_duration_seconds_count",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("runtime metrics missing %q:\n%s", want, body)
		}
	}
}
