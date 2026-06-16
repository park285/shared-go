package promptguard

func (g *Guard) cacheKeysForTest() []string {
	return g.cache.keys()
}
