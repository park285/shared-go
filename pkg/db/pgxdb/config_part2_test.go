package pgxdb

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDefaultPoolConfig_IgnoresEnvUsesStaticDefaults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setMin bool
		setMax bool
		minVal string
		maxVal string
	}{
		{name: "unset"},
		{name: "zero and over-cap", setMin: true, setMax: true, minVal: "0", maxVal: "500"},
		{name: "normal values", setMin: true, setMax: true, minVal: "7", maxVal: "33"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setMin {
				t.Setenv("DB_POOL_MIN_CONNS", tc.minVal)
			}

			if tc.setMax {
				t.Setenv("DB_POOL_MAX_CONNS", tc.maxVal)
			}

			pc := DefaultPoolConfig()
			if pc.MinConns != 0 {
				t.Errorf("MinConns = %d, want static 0 (env not read by library)", pc.MinConns)
			}

			if pc.MaxConns != 20 {
				t.Errorf("MaxConns = %d, want static 20 (env not read by library)", pc.MaxConns)
			}

			if pc.ConnMaxLifetime != time.Hour {
				t.Errorf("ConnMaxLifetime = %v, want 1h", pc.ConnMaxLifetime)
			}

			if pc.ConnMaxIdleTime != 30*time.Minute {
				t.Errorf("ConnMaxIdleTime = %v, want 30m", pc.ConnMaxIdleTime)
			}
		})
	}
}

func TestWithPoolDefaults_FillsUnsetFromStaticDefaultPoolConfig(t *testing.T) {
	t.Setenv("DB_POOL_MIN_CONNS", "7")
	t.Setenv("DB_POOL_MAX_CONNS", "33")

	def := DefaultPoolConfig()
	got := withPoolDefaults(PoolConfig{})

	if got.MinConns != def.MinConns || got.MaxConns != def.MaxConns {
		t.Errorf("conns = %d/%d, want %d/%d (single source: DefaultPoolConfig)", got.MinConns, got.MaxConns, def.MinConns, def.MaxConns)
	}

	if got.MinConns != 0 || got.MaxConns != 20 {
		t.Errorf("conns = %d/%d, want static 0/20 regardless of DB_POOL_* env", got.MinConns, got.MaxConns)
	}

	if got.ConnMaxLifetime != def.ConnMaxLifetime || got.ConnMaxIdleTime != def.ConnMaxIdleTime {
		t.Errorf("lifetimes = %v/%v, want %v/%v", got.ConnMaxLifetime, got.ConnMaxIdleTime, def.ConnMaxLifetime, def.ConnMaxIdleTime)
	}

	if got.ConnMaxLifetimeJitter != got.ConnMaxLifetime/5 {
		t.Errorf("jitter = %v, want lifetime/5", got.ConnMaxLifetimeJitter)
	}
}

func TestWithPoolDefaults_PreservesExplicitValues(t *testing.T) {
	t.Setenv("DB_POOL_MIN_CONNS", "7")
	t.Setenv("DB_POOL_MAX_CONNS", "33")

	got := withPoolDefaults(PoolConfig{MinConns: 3, MaxConns: 12, ConnMaxLifetime: 2 * time.Hour, ConnMaxLifetimeJitter: 90 * time.Second, ConnMaxIdleTime: 5 * time.Minute})
	if got.MinConns != 3 || got.MaxConns != 12 {
		t.Errorf("conns = %d/%d, want explicit 3/12 (fallback must not override)", got.MinConns, got.MaxConns)
	}

	if got.ConnMaxLifetime != 2*time.Hour || got.ConnMaxLifetimeJitter != 90*time.Second || got.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("lifetimes = %v/%v/%v, want explicit values preserved", got.ConnMaxLifetime, got.ConnMaxLifetimeJitter, got.ConnMaxIdleTime)
	}
}

func TestWithPoolDefaults_PreservesExplicitMinConnsZero(t *testing.T) {
	got := withPoolDefaults(PoolConfig{MinConns: 0, MaxConns: 8})
	if got.MinConns != 0 {
		t.Errorf("MinConns = %d, want explicit 0 preserved (operator intent, pgx no-min-idle)", got.MinConns)
	}

	if got.MaxConns != 8 {
		t.Errorf("MaxConns = %d, want explicit 8 preserved", got.MaxConns)
	}
}

func TestValidateConnCounts_Int32Range(t *testing.T) {
	if err := validateConnCounts(PoolConfig{MinConns: 1, MaxConns: 20}); err != nil {
		t.Fatalf("valid counts: unexpected error %v", err)
	}

	if err := validateConnCounts(PoolConfig{MaxConns: math.MaxInt32 + 1}); err == nil {
		t.Fatal("max > int32: expected error, got nil")
	}

	if err := validateConnCounts(PoolConfig{MinConns: -1}); err == nil {
		t.Fatal("negative min: expected error, got nil")
	}
}

func TestApplyAndOverlayPoolConfig_RejectAboveInt32Range(t *testing.T) {
	t.Parallel()

	overflow := PoolConfig{MaxConns: math.MaxInt32 + 1}
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
	parsedMax := pc.MaxConns

	if err := applyPoolConfig(pc, overflow); err == nil {
		t.Fatal("applyPoolConfig with MaxConns above int32: expected error, got nil")
	}

	if err := overlayPoolConfig(pc, overflow); err == nil {
		t.Fatal("overlayPoolConfig with MaxConns above int32: expected error, got nil")
	}

	if pc.MaxConns != parsedMax {
		t.Errorf("MaxConns = %d, want parsed %d preserved (rejected config must not leak a truncated value)", pc.MaxConns, parsedMax)
	}
}

func TestOverlayPoolConfig_RejectsMinAboveParsedDefaultMax(t *testing.T) {
	t.Parallel()

	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
	parsedMax := pc.MaxConns

	if parsedMax <= 0 {
		t.Fatalf("precondition: parsed MaxConns = %d, want positive pgx default", parsedMax)
	}

	overMax := int(parsedMax) + 1

	err := overlayPoolConfig(pc, PoolConfig{MinConns: overMax})
	if err == nil {
		t.Fatalf("overlayPoolConfig(MinConns=%d) over parsed MaxConns=%d: expected error, got nil", overMax, parsedMax)
	}

	if !strings.Contains(err.Error(), "exceeds max conns") {
		t.Errorf("error = %v, want inverted conn range error", err)
	}

	if int(pc.MinConns) == overMax {
		t.Error("rejected overlay must not be partially applied")
	}
}

func TestOverlayPoolConfig_RejectsMinAboveEffectiveMax(t *testing.T) {
	t.Parallel()

	t.Run("max left at dsn value", func(t *testing.T) {
		t.Parallel()

		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_max_conns=12")
		if pc.MaxConns != 12 {
			t.Fatalf("precondition: parsed MaxConns = %d, want 12", pc.MaxConns)
		}

		if err := overlayPoolConfig(pc, PoolConfig{MinConns: 30}); err == nil {
			t.Fatal("overlayPoolConfig(MinConns=30) over parsed MaxConns=12: expected error, got nil")
		}

		if pc.MinConns == 30 {
			t.Error("rejected overlay must not be partially applied")
		}
	})

	t.Run("min left at dsn value", func(t *testing.T) {
		t.Parallel()

		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_min_conns=8&pool_max_conns=20")
		if pc.MinConns != 8 {
			t.Fatalf("precondition: parsed MinConns = %d, want 8", pc.MinConns)
		}

		if err := overlayPoolConfig(pc, PoolConfig{MaxConns: 4}); err == nil {
			t.Fatal("overlayPoolConfig(MaxConns=4) under parsed MinConns=8: expected error, got nil")
		}

		if pc.MaxConns == 4 {
			t.Error("rejected overlay must not be partially applied")
		}
	})

	t.Run("inverted dsn with empty overlay", func(t *testing.T) {
		t.Parallel()

		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_min_conns=30&pool_max_conns=10")
		if pc.MinConns != 30 || pc.MaxConns != 10 {
			t.Fatalf("precondition: parsed conns = %d/%d, want 30/10", pc.MinConns, pc.MaxConns)
		}

		if err := overlayPoolConfig(pc, PoolConfig{}); err == nil {
			t.Fatal("overlayPoolConfig with inverted dsn range: expected error, got nil")
		}
	})
}

func TestOverlayPoolConfig_AcceptsValidPartialOverlay(t *testing.T) {
	t.Parallel()

	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_max_conns=20")
	if err := overlayPoolConfig(pc, PoolConfig{MinConns: 8}); err != nil {
		t.Fatalf("overlayPoolConfig(MinConns=8) under parsed MaxConns=20: unexpected error %v", err)
	}

	if pc.MinConns != 8 || pc.MaxConns != 20 {
		t.Errorf("conns = %d/%d, want 8/20", pc.MinConns, pc.MaxConns)
	}

	pc = mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_min_conns=2&pool_max_conns=20")
	if err := overlayPoolConfig(pc, PoolConfig{MaxConns: 6}); err != nil {
		t.Fatalf("overlayPoolConfig(MaxConns=6) over parsed MinConns=2: unexpected error %v", err)
	}

	if pc.MinConns != 2 || pc.MaxConns != 6 {
		t.Errorf("conns = %d/%d, want 2/6", pc.MinConns, pc.MaxConns)
	}
}
