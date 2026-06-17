package obsmetrics

import "sync"

// Histogram은 고정 버킷 경계를 가진 thread-safe 누적 히스토그램입니다.
type Histogram struct {
	mu          sync.Mutex
	upperBounds []float64
	counts      []uint64
	sum         float64
	total       uint64
}

func NewHistogram(buckets []float64) *Histogram {
	bounds := make([]float64, len(buckets))
	copy(bounds, buckets)

	return &Histogram{
		upperBounds: bounds,
		counts:      make([]uint64, len(bounds)),
	}
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sum += v
	h.total++

	for i, ub := range h.upperBounds {
		if v <= ub {
			h.counts[i]++
		}
	}
}

type HistogramSnapshot struct {
	UpperBounds []float64
	Cumulative  []uint64
	Sum         float64
	Total       uint64
}

func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	cumulative := make([]uint64, len(h.counts))
	copy(cumulative, h.counts)

	bounds := make([]float64, len(h.upperBounds))
	copy(bounds, h.upperBounds)

	return HistogramSnapshot{
		UpperBounds: bounds,
		Cumulative:  cumulative,
		Sum:         h.sum,
		Total:       h.total,
	}
}
