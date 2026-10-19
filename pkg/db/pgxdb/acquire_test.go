package pgxdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquirePolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxConns int
		reserve  int32
		timeout  time.Duration
		valid    bool
	}{
		{name: "minimum zero", maxConns: 2, reserve: 1, timeout: time.Second, valid: true},
		{name: "timeout only", maxConns: 1, timeout: time.Second, valid: true},
		{name: "negative reserve", maxConns: 2, reserve: -1, timeout: time.Second},
		{name: "no ordinary capacity", maxConns: 1, reserve: 1, timeout: time.Second},
		{name: "reserve above maximum", maxConns: 2, reserve: 3, timeout: time.Second},
		{name: "missing timeout", maxConns: 2},
		{name: "negative timeout", maxConns: 2, timeout: -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Pool: PoolConfig{MaxConns: tc.maxConns}, AcquirePolicy: &AcquirePolicy{ReservedConns: tc.reserve, Timeout: tc.timeout}}
			cfg, err := buildConfigPool(&Config{Host: "localhost", Port: 5432, SSLMode: testDisable}, opts)

			if tc.valid {
				require.NoError(t, err)
				require.Zero(t, cfg.MinConns)
			} else {
				requireConfigurationFailure(t, err)
			}
		})
	}

	// DSN 검증은 0인 덮어쓰기 필드가 아닌 유효 최대값을 사용하며 접속 시도 전에 거부한다.
	for _, opts := range []Options{
		{AcquirePolicy: &AcquirePolicy{ReservedConns: 1, Timeout: time.Second}},
		{Pool: PoolConfig{MaxConns: 1}, AcquirePolicy: &AcquirePolicy{ReservedConns: 1, Timeout: time.Second}},
	} {
		_, err := OpenPoolDSN(t.Context(), "postgres://test@127.0.0.1:1/test?sslmode=disable&pool_max_conns=1", opts)
		requireConfigurationFailure(t, err)
	}

	_, err := buildConfigPool(&Config{Host: "localhost", Port: 5432, SSLMode: testDisable}, Options{Pool: PoolConfig{MaxConns: 1}})
	require.NoError(t, err)
}

func TestWithControlAcquirePreservesParent(t *testing.T) {
	type valueKey struct{}

	parent, cancel := context.WithTimeout(context.WithValue(t.Context(), valueKey{}, "lease"), time.Second)

	defer cancel()

	control := WithControlAcquire(parent)
	require.Equal(t, "lease", control.Value(valueKey{}))

	want, ok := parent.Deadline()
	require.True(t, ok)

	got, ok := control.Deadline()
	require.True(t, ok)
	require.Equal(t, want, got)
	cancel()
	<-control.Done()
	require.ErrorIs(t, control.Err(), context.Canceled)
	require.Nil(t, parent.Value(controlAcquireKey{}), "marking must not mutate reusable parent")
}
