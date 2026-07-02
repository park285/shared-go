package sqldb

import (
	"database/sql"
	"time"
)

type PoolConfig struct {
	MaxOpenConns int
	MaxIdleConns int
	// MaxIdleConnsSet이 true면 MaxIdleConns가 0이어도 그대로 적용해 유휴 풀을 비활성화한다.
	// (SetMaxIdleConns(0) = 유휴 연결 미보유). false면 양수일 때만 적용한다.
	MaxIdleConnsSet bool
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func resolveMaxIdleConns(cfg PoolConfig) (int, bool) {
	if cfg.MaxIdleConnsSet {
		return cfg.MaxIdleConns, true
	}
	if cfg.MaxIdleConns > 0 {
		return cfg.MaxIdleConns, true
	}
	return 0, false
}

func Configure(db *sql.DB, cfg PoolConfig) {
	if db == nil {
		return
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if value, apply := resolveMaxIdleConns(cfg); apply {
		db.SetMaxIdleConns(value)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
}
