package outputguard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedIndexCacheUpdateBeforeEvictionAndCloneIsolation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	cache := newProtectedIndexCache(2, time.Hour, func() time.Time { return now })
	textsA := []string{makeTokenBoundaryText(protectedTokenWindow, protectedMinRunes)}
	textsB := []string{makeTokenBoundaryText(protectedTokenWindow, protectedMinRunes+1)}
	keyA := protectedTextsDigest(textsA)
	keyB := protectedTextsDigest(textsB)
	indexA := buildProtectedIndex(textsA)
	cache.put(keyA, indexA)
	cache.put(keyB, buildProtectedIndex(textsB))

	indexA.entries[0].runes[0] = 'z'
	storedA, ok := cache.get(keyA)
	require.True(t, ok)
	assert.NotEqual(t, 'z', storedA.entries[0].runes[0])

	replacement := buildProtectedIndex(textsA)
	cache.put(keyA, replacement)
	assert.Equal(t, 2, cache.len())
	_, bPresent := cache.get(keyB)
	assert.True(t, bPresent, "updating an existing key at capacity must not evict another key")
}

func TestProtectedIndexCacheCapacityAndTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	cache := newProtectedIndexCache(2, time.Hour, func() time.Time { return now })
	keys := make([][32]byte, 3)
	for i := range keys {
		texts := []string{makeTokenBoundaryText(protectedTokenWindow, protectedMinRunes+i)}
		keys[i] = protectedTextsDigest(texts)
		cache.put(keys[i], buildProtectedIndex(texts))
	}
	assert.Equal(t, 2, cache.len())
	_, firstPresent := cache.get(keys[0])
	assert.False(t, firstPresent)

	now = now.Add(time.Hour)
	_, presentAfterTTL := cache.get(keys[2])
	assert.False(t, presentAfterTTL)
}

func TestProtectedIndexCacheConcurrentAccess(t *testing.T) {
	t.Parallel()

	cache := newProtectedIndexCache(8, time.Hour, nil)
	done := make(chan struct{}, 16)
	for i := range 16 {
		go func(worker int) {
			texts := []string{makeTokenBoundaryText(protectedTokenWindow, protectedMinRunes+worker%4)}
			for range 100 {
				_ = cache.loadOrBuild(texts)
			}
			done <- struct{}{}
		}(i)
	}
	for range 16 {
		<-done
	}
	assert.LessOrEqual(t, cache.len(), 8)
}
