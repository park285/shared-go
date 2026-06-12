package workerpool

import (
	"io"
	"log/slog"
	"testing"
)

func benchLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func BenchmarkQueuedPoolSubmitSingle(b *testing.B) {
	p := NewQueuedWithLogger(QueuedConfig{Workers: 4, QueueSize: 1024}, benchLogger())
	defer p.StopAndWait()

	noop := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !p.SubmitWait(noop) {
			b.Fatal("SubmitWait() = false, want true")
		}
	}
}

func BenchmarkQueuedPoolSubmitParallel(b *testing.B) {
	p := NewQueuedWithLogger(QueuedConfig{Workers: 8, QueueSize: 4096}, benchLogger())
	defer p.StopAndWait()

	noop := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !p.SubmitWait(noop) {
				b.Fatal("SubmitWait() = false, want true")
			}
		}
	})
}

func BenchmarkQueuedPoolTrySubmitFull(b *testing.B) {
	p := NewQueuedWithLogger(QueuedConfig{Workers: 1, QueueSize: 1}, benchLogger())

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		b.Fatal("TrySubmit() for blocking task = false, want true")
	}
	<-started
	if !p.TrySubmit(func() { <-release }) {
		b.Fatal("TrySubmit() to fill queue = false, want true")
	}

	noop := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if p.TrySubmit(noop) {
			b.Fatal("TrySubmit() on full queue = true, want false")
		}
	}

	b.StopTimer()
	close(release)
	p.StopAndWait()
}
