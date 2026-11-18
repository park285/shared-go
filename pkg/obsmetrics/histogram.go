package obsmetrics

import (
	"math"
	"sync/atomic"
)

type Histogram struct {
	upperBounds []float64
	counts      []atomic.Uint64
	sumBits     atomic.Uint64
	total       atomic.Uint64
}

func NewHistogram(buckets []float64) *Histogram {
	bounds := make([]float64, len(buckets))
	copy(bounds, buckets)

	return &Histogram{
		upperBounds: bounds,
		counts:      make([]atomic.Uint64, len(bounds)),
	}
}

func (h *Histogram) Observe(v float64) {
	if h == nil {
		return
	}

	h.total.Add(1)
	atomicAddFloat64(&h.sumBits, v)

	for i, ub := range h.upperBounds {
		if v <= ub {
			h.counts[i].Add(1)

			return
		}
	}
}

func atomicAddFloat64(bits *atomic.Uint64, delta float64) {
	for {
		oldBits := bits.Load()
		newBits := math.Float64bits(math.Float64frombits(oldBits) + delta)

		if bits.CompareAndSwap(oldBits, newBits) {
			return
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
	if h == nil {
		return HistogramSnapshot{}
	}

	cumulative := make([]uint64, len(h.counts))

	var running uint64

	for i := range h.counts {
		running += h.counts[i].Load()

		cumulative[i] = running
	}

	bounds := make([]float64, len(h.upperBounds))
	copy(bounds, h.upperBounds)

	return HistogramSnapshot{
		UpperBounds: bounds,
		Cumulative:  cumulative,
		Sum:         math.Float64frombits(h.sumBits.Load()),
		Total:       h.total.Load(),
	}
}
