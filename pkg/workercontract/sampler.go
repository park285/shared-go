package workercontract

import (
	"context"
	"sync"
	"time"
)

const (
	QueueSampleInterval = 15 * time.Second
	QueueSampleTimeout  = 2 * time.Second
)

// QueueSnapshotStatus는 latest observation의 가용성을 나타낸다.
type QueueSnapshotStatus string

const (
	QueueSnapshotCurrent     QueueSnapshotStatus = "current"
	QueueSnapshotUnavailable QueueSnapshotStatus = "unavailable"
)

// QueueSnapshotErrorCode는 diagnostics에 공개 가능한 bounded code다.
type QueueSnapshotErrorCode string

const (
	QueueNotSampled     QueueSnapshotErrorCode = "not_sampled"
	QueueSnapshotFailed QueueSnapshotErrorCode = "queue_snapshot_failed"
	QueueSnapshotStale  QueueSnapshotErrorCode = "queue_snapshot_stale"
)

// QueueValues는 source가 한 번에 관측한 ready backlog다.
type QueueValues struct {
	Depth           int64
	OldestQueuedAge time.Duration
}

// QueueSnapshot은 latest attempt와 last-success metadata를 분리한다.
type QueueSnapshot struct {
	Status               QueueSnapshotStatus
	Depth                *int64
	OldestQueuedAgeMS    *int64
	LastSuccessAtEpochMS *int64
	ErrorCode            *QueueSnapshotErrorCode
}

// QueueSnapshotSource는 하나의 canonical queue를 bounded context로 조회한다.
type QueueSnapshotSource func(context.Context) (QueueValues, error)

// QueueSampler는 HTTP 요청과 분리된 fixed-cadence queue sampler다.
type QueueSampler struct {
	mu     sync.RWMutex
	source QueueSnapshotSource
	latest QueueSnapshot
}

// NewQueueSampler는 아직 관측되지 않은 sampler를 만든다.
func NewQueueSampler(source QueueSnapshotSource) *QueueSampler {
	code := QueueNotSampled

	return &QueueSampler{
		source: source,
		latest: QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code},
	}
}

// Sample은 한 번만 조회하고 실패 시 current value를 제거한다.
func (s *QueueSampler) Sample(ctx context.Context, now time.Time) QueueSnapshot {
	if s == nil || s.source == nil {
		code := QueueSnapshotFailed
		return QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code}
	}

	queryCtx, cancel := context.WithTimeout(ctx, QueueSampleTimeout)
	values, err := s.source(queryCtx)

	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil || values.Depth < 0 || values.OldestQueuedAge < 0 ||
		(values.Depth == 0 && values.OldestQueuedAge != 0) {
		code := QueueSnapshotFailed

		s.latest.Status = QueueSnapshotUnavailable
		s.latest.Depth = nil
		s.latest.OldestQueuedAgeMS = nil
		s.latest.ErrorCode = &code

		return cloneQueueSnapshot(s.latest)
	}

	depth := values.Depth
	ageMS := values.OldestQueuedAge.Milliseconds()
	successAt := now.UnixMilli()

	s.latest = QueueSnapshot{
		Status:               QueueSnapshotCurrent,
		Depth:                &depth,
		OldestQueuedAgeMS:    &ageMS,
		LastSuccessAtEpochMS: &successAt,
	}

	return cloneQueueSnapshot(s.latest)
}

// Latest는 queue query 없이 마지막 attempt 결과를 반환한다.
func (s *QueueSampler) Latest() QueueSnapshot {
	if s == nil {
		code := QueueNotSampled
		return QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code}
	}

	s.mu.RLock()

	defer s.mu.RUnlock()

	return cloneQueueSnapshot(s.latest)
}

// Run은 immediate first attempt 뒤 fixed cadence로 sampling한다.
func (s *QueueSampler) Run(ctx context.Context) {
	if s == nil {
		return
	}

	s.Sample(ctx, time.Now())

	ticker := time.NewTicker(QueueSampleInterval)

	defer ticker.Stop()

	for {
		select {
		case sampledAt := <-ticker.C:
			s.Sample(ctx, sampledAt)
		case <-ctx.Done():
			return
		}
	}
}

// CurrentQueueSnapshot은 memory/SQLite owner가 이미 보유한 non-blocking 값을 만든다.
func CurrentQueueSnapshot(depth int64, oldestQueuedAge time.Duration, observedAt time.Time) QueueSnapshot {
	if depth < 0 || oldestQueuedAge < 0 || (depth == 0 && oldestQueuedAge != 0) {
		code := QueueSnapshotFailed
		return QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code}
	}

	ageMS := oldestQueuedAge.Milliseconds()
	successAt := observedAt.UnixMilli()

	return QueueSnapshot{
		Status:               QueueSnapshotCurrent,
		Depth:                &depth,
		OldestQueuedAgeMS:    &ageMS,
		LastSuccessAtEpochMS: &successAt,
	}
}

func cloneQueueSnapshot(snapshot QueueSnapshot) QueueSnapshot {
	clone := snapshot
	if snapshot.Depth != nil {
		value := *snapshot.Depth

		clone.Depth = &value
	}

	if snapshot.OldestQueuedAgeMS != nil {
		value := *snapshot.OldestQueuedAgeMS

		clone.OldestQueuedAgeMS = &value
	}

	if snapshot.LastSuccessAtEpochMS != nil {
		value := *snapshot.LastSuccessAtEpochMS

		clone.LastSuccessAtEpochMS = &value
	}

	if snapshot.ErrorCode != nil {
		value := *snapshot.ErrorCode

		clone.ErrorCode = &value
	}

	return clone
}
