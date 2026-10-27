package lifecycle

import "sync"

// cleanupCloser는 cleanup을 한 번만 실행한다. 값으로 복사해도 같은 once를 공유한다.
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
