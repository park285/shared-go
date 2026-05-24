package lifecycle

type CleanupCloser struct {
	cleanup func()
}

func NewCleanupCloser(cleanup func()) CleanupCloser {
	return CleanupCloser{cleanup: cleanup}
}

func (c *CleanupCloser) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
	}
}
