package pgxdb

import "github.com/jackc/pgx/v5/pgxpool"

// PoolMetricType identifies the cumulative or instantaneous value of a metric.
type PoolMetricType uint8

const (
	PoolMetricGauge PoolMetricType = iota
	PoolMetricCounter
)

// PoolMetric contains a prefix-independent descriptor and one observed value.
// State is empty except for the conns family. Callers own pool identity labels.
type PoolMetric struct {
	Suffix string
	Help   string
	Type   PoolMetricType
	State  string
	Value  float64
}

// VisitPoolMetrics emits one immutable pgx Stat snapshot without collecting an
// intermediate map or slice. Callers take one Stat per pool per scrape. Nil
// inputs emit nothing; an unavailable pool must not be reported as zero-valued.
func VisitPoolMetrics(stat *pgxpool.Stat, emit func(PoolMetric)) {
	if stat == nil || emit == nil {
		return
	}

	emit(PoolMetric{Suffix: "acquires_total", Help: "Cumulative successful connection acquisitions from the pgx pool.", Type: PoolMetricCounter, Value: float64(stat.AcquireCount())})
	emit(PoolMetric{Suffix: "acquire_duration_seconds_total", Help: "Cumulative time spent in successful pgx pool acquisitions.", Type: PoolMetricCounter, Value: stat.AcquireDuration().Seconds()})
	emit(PoolMetric{Suffix: "empty_acquires_total", Help: "Cumulative successful acquisitions that waited because the pgx pool had no idle connection.", Type: PoolMetricCounter, Value: float64(stat.EmptyAcquireCount())})
	emit(PoolMetric{Suffix: "empty_acquire_wait_seconds_total", Help: "Cumulative time successful acquisitions waited for a connection while the pgx pool was empty.", Type: PoolMetricCounter, Value: stat.EmptyAcquireWaitTime().Seconds()})
	emit(PoolMetric{Suffix: "canceled_acquires_total", Help: "Cumulative pgx pool acquisitions canceled by their context.", Type: PoolMetricCounter, Value: float64(stat.CanceledAcquireCount())})
	emit(PoolMetric{Suffix: "conns", Help: "Current pgx pool connections by state.", Type: PoolMetricGauge, State: "acquired", Value: float64(stat.AcquiredConns())})
	emit(PoolMetric{Suffix: "conns", Help: "Current pgx pool connections by state.", Type: PoolMetricGauge, State: "idle", Value: float64(stat.IdleConns())})
	emit(PoolMetric{Suffix: "conns", Help: "Current pgx pool connections by state.", Type: PoolMetricGauge, State: "constructing", Value: float64(stat.ConstructingConns())})
	emit(PoolMetric{Suffix: "max_conns", Help: "Configured maximum size of the pgx pool.", Type: PoolMetricGauge, Value: float64(stat.MaxConns())})
	emit(PoolMetric{Suffix: "new_conns_total", Help: "Cumulative pgx pool connection construction attempts, including failed attempts.", Type: PoolMetricCounter, Value: float64(stat.NewConnsCount())})
	emit(PoolMetric{Suffix: "max_lifetime_destroy_total", Help: "Cumulative pgx pool connections destroyed after reaching their maximum lifetime.", Type: PoolMetricCounter, Value: float64(stat.MaxLifetimeDestroyCount())})
	emit(PoolMetric{Suffix: "max_idle_destroy_total", Help: "Cumulative pgx pool connections destroyed after reaching their maximum idle time.", Type: PoolMetricCounter, Value: float64(stat.MaxIdleDestroyCount())})
}
