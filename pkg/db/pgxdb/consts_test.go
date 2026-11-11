package pgxdb

import "time"

const (
	testDisable    = "disable"
	testLocalhost  = "localhost"
	testVerifyFull = "verify-full"
)

func testPoolConfig() PoolConfig {
	return PoolConfig{MaxConns: 20, ConnMaxLifetime: time.Hour, ConnMaxLifetimeJitter: 12 * time.Minute, ConnMaxIdleTime: 30 * time.Minute}
}

func testPing() PingConfig {
	return PingConfig{PingTimeout: 5 * time.Second}
}
