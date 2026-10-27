package pgxdb

import "time"

const (
	testDisable    = "disable"
	testLocalhost  = "localhost"
	testVerifyFull = "verify-full"
)

// testPoolConfig는 v2.17.0까지 라이브러리가 채우던 값(최대 20, 수명 1h, 지터 수명/5, 유휴 30m)을
// 호출자가 명시하는 구조화 풀 설정이다.
func testPoolConfig() PoolConfig {
	return PoolConfig{MaxConns: 20, ConnMaxLifetime: time.Hour, ConnMaxLifetimeJitter: 12 * time.Minute, ConnMaxIdleTime: 30 * time.Minute}
}

// testPing은 v2.17.0까지의 시작 ping 기본값 5초를 명시한 값이다.
func testPing() PingConfig {
	return PingConfig{PingTimeout: 5 * time.Second}
}
