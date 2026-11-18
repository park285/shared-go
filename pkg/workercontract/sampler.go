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

type QueueSnapshotStatus string

const (
	QueueSnapshotCurrent     QueueSnapshotStatus = "current"
	QueueSnapshotUnavailable QueueSnapshotStatus = "unavailable"
)

type QueueSnapshotErrorCode string

const (
	QueueNotSampled     QueueSnapshotErrorCode = "not_sampled"
	QueueSnapshotFailed QueueSnapshotErrorCode = "queue_snapshot_failed"
	QueueSnapshotStale  QueueSnapshotErrorCode = "queue_snapshot_stale"
)

type QueueValues struct {
	Depth           int64
	OldestQueuedAge time.Duration
}

type QueueSnapshot struct {
	Status               QueueSnapshotStatus
	Depth                *int64
	OldestQueuedAgeMS    *int64
	LastSuccessAtEpochMS *int64
	ErrorCode            *QueueSnapshotErrorCode
}

type QueueSnapshotSource func(context.Context) (QueueValues, error)

type QueueSampler struct {
	mu     sync.RWMutex
	source QueueSnapshotSource
	latest QueueSnapshot
}

func NewQueueSampler(source QueueSnapshotSource) *QueueSampler {
	code := QueueNotSampled

	return &QueueSampler{
		source: source,
		latest: QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code},
	}
}

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

func (s *QueueSampler) Latest() QueueSnapshot {
	if s == nil {
		code := QueueNotSampled
		return QueueSnapshot{Status: QueueSnapshotUnavailable, ErrorCode: &code}
	}

	s.mu.RLock()

	defer s.mu.RUnlock()

	return cloneQueueSnapshot(s.latest)
}

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
