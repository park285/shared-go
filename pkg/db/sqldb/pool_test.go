package sqldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("fake connector never connects")
}

func (fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake driver never opens")
}

func TestResolveMaxIdleConns(t *testing.T) {
	tests := []struct {
		name      string
		cfg       PoolConfig
		wantValue int
		wantApply bool
	}{
		{name: "explicit set zero disables idle pool", cfg: PoolConfig{MaxIdleConns: 0, MaxIdleConnsSet: true}, wantValue: 0, wantApply: true},
		{name: "explicit set positive", cfg: PoolConfig{MaxIdleConns: 3, MaxIdleConnsSet: true}, wantValue: 3, wantApply: true},
		{name: "unset positive applies", cfg: PoolConfig{MaxIdleConns: 5}, wantValue: 5, wantApply: true},
		{name: "unset zero skips", cfg: PoolConfig{MaxIdleConns: 0}, wantValue: 0, wantApply: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, apply := resolveMaxIdleConns(tt.cfg)
			if value != tt.wantValue || apply != tt.wantApply {
				t.Errorf("resolveMaxIdleConns() = (%d, %v), want (%d, %v)", value, apply, tt.wantValue, tt.wantApply)
			}
		})
	}
}

func TestConfigure_AppliesMaxOpenConns(t *testing.T) {
	db := sql.OpenDB(fakeConnector{})
	defer db.Close()

	Configure(db, PoolConfig{
		MaxOpenConns:    7,
		MaxIdleConns:    2,
		MaxIdleConnsSet: true,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	})

	if got := db.Stats().MaxOpenConnections; got != 7 {
		t.Errorf("MaxOpenConnections = %d, want 7", got)
	}
}

func TestConfigure_NilDBNoPanic(_ *testing.T) {
	Configure(nil, PoolConfig{MaxOpenConns: 5})
}
