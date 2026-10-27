package obsmetrics

import (
	"runtime"
	"testing"
)

func TestReadResidentMemoryBytes(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("requires /proc/self/statm")
	}

	rss, ok := readResidentMemoryBytes()
	if !ok {
		t.Fatal("readResidentMemoryBytes() ok = false on linux")
	}

	if rss == 0 {
		t.Fatal("readResidentMemoryBytes() = 0, want > 0")
	}
}
