package lockutil

import "sync"

const keyedMutexShardCount = 256

// KeyedMutex는 key를 shard에 해시해 잠그므로 서로 다른 key가 같은 shard를 공유할 수 있다.
// 따라서 lock을 쥔 채 다른 key를 Lock하면 self-deadlock이 가능하다. 중첩 Lock을 금지하고
// 임계구역은 짧게 유지한다. 긴 작업이나 I/O는 Unlock 이후로 옮긴다.
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
