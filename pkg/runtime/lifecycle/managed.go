package lifecycle

type Managed struct {
	cleanupCloser
}

func NewManaged(cleanup func()) Managed {
	return Managed{
		cleanupCloser: newCleanupCloser(cleanup),
	}
}

func (m *Managed) Close() {
	if m == nil {
		return
	}

	m.cleanupCloser.Close()
}
