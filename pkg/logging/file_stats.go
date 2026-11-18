package logging

import "sync/atomic"

var fileWriteFailed atomic.Uint64

type FileStats struct {
	WriteFailed uint64
}

func SnapshotFile() FileStats {
	return FileStats{WriteFailed: fileWriteFailed.Load()}
}
