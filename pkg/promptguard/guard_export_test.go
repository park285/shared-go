package promptguard

func (g *Guard) cacheKeysForTest() []string {
	if g.cache == nil {
		return nil
	}

	g.cache.mu.RLock()
	defer g.cache.mu.RUnlock()

	keys := make([]string, 0, len(g.cache.entries))
	for key := range g.cache.entries {
		keys = append(keys, key)
	}

	return keys
}
