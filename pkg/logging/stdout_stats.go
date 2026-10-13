package logging

import "sync/atomic"

var (
	stdoutDropped   atomic.Uint64
	stdoutTruncated atomic.Uint64
)

// StdoutStats reports process-lifetime outcomes of file-backed stdout copies.
// Dropped counts queue-full/closed writes and target errors or short writes.
// Truncated counts oversized copies successfully written as bounded JSON; a failed
// truncated copy counts only as Dropped. Shutdown diagnostic records, file writes,
// and console-only loggers are excluded. Pending writes have no outcome yet.
type StdoutStats struct {
	Dropped   uint64
	Truncated uint64
}

// SnapshotStdout returns independently sampled, monotonic process counters.
// Close waits at most two seconds for async draining. A timed-out target may still
// complete later, so both these counters and the file shutdown summary describe
// known outcomes, not cancellation of pending I/O. Counters are never reset.
func SnapshotStdout() StdoutStats {
	return StdoutStats{
		Dropped:   stdoutDropped.Load(),
		Truncated: stdoutTruncated.Load(),
	}
}
