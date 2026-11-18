package logging

import "sync/atomic"

var (
	stdoutDropped   atomic.Uint64
	stdoutTruncated atomic.Uint64
)

type StdoutStats struct {
	Dropped   uint64
	Truncated uint64
}

func SnapshotStdout() StdoutStats {
	return StdoutStats{
		Dropped:   stdoutDropped.Load(),
		Truncated: stdoutTruncated.Load(),
	}
}
