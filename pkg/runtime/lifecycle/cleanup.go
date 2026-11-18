package lifecycle

import "sync"

type cleanupCloser struct {
	cleanup func()
}

func newCleanupCloser(cleanup func()) cleanupCloser {
	if cleanup == nil {
		return cleanupCloser{}
	}

	return cleanupCloser{cleanup: sync.OnceFunc(cleanup)}
}

func (c *cleanupCloser) Close() {
	if c == nil || c.cleanup == nil {
		return
	}

	c.cleanup()
}
