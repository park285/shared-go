package lifecycle

type Managed struct {
	CleanupCloser
}

func NewManaged(cleanup func()) Managed {
	return Managed{
		CleanupCloser: NewCleanupCloser(cleanup),
	}
}

func (m *Managed) Close() {
	if m == nil {
		return
	}
	m.CleanupCloser.Close()
}
