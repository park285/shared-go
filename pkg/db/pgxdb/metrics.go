package pgxdb

import "github.com/jackc/pgx/v5/pgxpool"

// PoolMetricType은 메트릭의 누적값과 순간값을 구분한다.
type PoolMetricType uint8

const (
	PoolMetricGauge PoolMetricType = iota
	PoolMetricCounter
)

// PoolMetric은 접두사와 독립된 설명과 관측값을 담는다.
// State는 conns 계열에서만 사용하며 풀 식별 레이블은 호출자가 관리한다.
type PoolMetric struct {
	Suffix string
	Help   string
	Type   PoolMetricType
	State  string
	Value  float64
}

// VisitPoolMetrics는 중간 맵·슬라이스 없이 불변 Stat 스냅샷을 전달한다.
// 호출자는 수집마다 풀별 Stat을 한 번 얻는다. 입력이 nil이면 출력하지 않으며,
// 사용 불가한 풀을 0으로 보고해서는 안 된다.
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
