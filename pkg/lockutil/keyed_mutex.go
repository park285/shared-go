package lockutil

import "sync"

const keyedMutexShardCount = 256

type KeyedMutex struct {
	shards [keyedMutexShardCount]sync.Mutex
}

func (m *KeyedMutex) Lock(key string) {
	m.shards[keyedMutexShard(key)].Lock()
}

func (m *KeyedMutex) Unlock(key string) {
	m.shards[keyedMutexShard(key)].Unlock()
}

func keyedMutexShard(key string) uint8 {
	const offset32 = 2166136261
	const prime32 = 16777619

	hash := uint32(offset32)
	for index := range len(key) {
		hash ^= uint32(key[index])
		hash *= prime32
	}
	return uint8(hash)
}
