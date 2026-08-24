package lifecycle

import "sync"

type CleanupCloser struct {
	cleanup func()
	once    *sync.Once
}

func NewCleanupCloser(cleanup func()) CleanupCloser {
	return CleanupCloser{cleanup: cleanup, once: &sync.Once{}}
}

func (c *CleanupCloser) Close() {
	if c == nil || c.cleanup == nil {
		return
	}

	if c.once == nil {
		c.cleanup()

		return
	}

	c.once.Do(c.cleanup)
}
