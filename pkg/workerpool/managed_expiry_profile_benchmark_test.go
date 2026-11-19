package workerpool

import (
	"testing"
	"time"
)

func BenchmarkManagedExpiryProfile(b *testing.B) {
	for _, profile := range []struct {
		name     string
		capacity int
		maxAge   time.Duration
	}{
		{"compaction", 8, 5 * time.Minute}, {"draw", 16, 5 * time.Minute}, {"command", 100, 0},
	} {
		b.Run(profile.name, func(b *testing.B) {
			now := time.Now()
			pool := &ManagedPool{queue: newBoundedQueue[*managedJob](profile.capacity), outcomes: make(map[JobOutcome]uint64), attempts: make(map[JobOutcome]uint64), discarded: make(map[JobOutcome]uint64)}

			for range profile.capacity {
				pool.queue.Push(&managedJob{})
			}

			b.ReportAllocs()
			b.ReportMetric(float64(profile.capacity), "capacity")
			b.RunParallel(func(pb *testing.PB) {
				iteration := 0

				for pb.Next() {
					job := new(managedJob)

					if profile.maxAge > 0 {
						job.expiresAt = now.Add(profile.maxAge)

						if iteration%16 == 0 {
							job.expiresAt = now
						}
					}

					pool.mu.Lock()
					pool.queue.Pop()
					pool.queue.Push(job)

					for pool.queue.Len() < profile.capacity {
						pool.queue.Push(&managedJob{expiresAt: now.Add(profile.maxAge + time.Hour)})
					}

					pool.mu.Unlock()
					pool.nextExpiry()

					if iteration%16 == 0 {
						pool.expireStale(now)
					}

					iteration++
				}
			})
		})
	}
}
