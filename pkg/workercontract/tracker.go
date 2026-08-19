package workercontract

import (
	"sync"
	"sync/atomic"
	"time"
)

type ExecutorTracker struct {
	running atomic.Int64
	nextID  atomic.Uint64
	mu      sync.RWMutex
	started map[uint64]time.Time
}

func NewExecutorTracker() *ExecutorTracker {
	return &ExecutorTracker{started: make(map[uint64]time.Time)}
}

func (t *ExecutorTracker) StartWorkers(count int) bool {
	if t == nil || count < 1 {
		return false
	}
	t.running.Add(int64(count))
	return true
}

func (t *ExecutorTracker) StopWorkers(count int) bool {
	if t == nil || count < 1 {
		return false
	}
	for {
		current := t.running.Load()
		if current < int64(count) || !t.running.CompareAndSwap(current, current-int64(count)) {
			if current < int64(count) {
				return false
			}
			continue
		}
		return true
	}
}

func (t *ExecutorTracker) BeginAttempt(now time.Time) uint64 {
	if t == nil {
		return 0
	}
	id := t.nextID.Add(1)
	t.mu.Lock()
	t.started[id] = now
	t.mu.Unlock()
	return id
}

func (t *ExecutorTracker) EndAttempt(id uint64) bool {
	if t == nil || id == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, present := t.started[id]; !present {
		return false
	}
	delete(t.started, id)
	return true
}

func (t *ExecutorTracker) Snapshot(now time.Time) ExecutorSnapshot {
	if t == nil {
		return ExecutorSnapshot{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	oldest := time.Duration(0)
	for _, startedAt := range t.started {
		age := now.Sub(startedAt)
		if age > oldest {
			oldest = age
		}
	}
	return ExecutorSnapshot{RunningWorkers: t.running.Load(), InFlight: int64(len(t.started)), OldestInFlightAgeMS: oldest.Milliseconds()}
}
