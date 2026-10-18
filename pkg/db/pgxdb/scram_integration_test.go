package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type scramAuthenticationCase struct {
	name       string
	password   string
	equivalent string
}

var scramAuthenticationCases = []scramAuthenticationCase{
	{"ascii", "synthetic-password", "synthetic-password"},
	{"quotes", "synthetic-'\\password", "synthetic-'\\password"},
	{"hangul", "synthetic-비밀번호", "synthetic-비밀번호"},
	{"hangul normalization", "synthetic-\u1100\u1161", "synthetic-가"},
	{"nonbreaking space", "synthetic-a\u00a0b", "synthetic-a b"},
	{"control raw fallback", "synthetic-\a", "synthetic-\a"},
	{"private use raw fallback", "synthetic-\ue000", "synthetic-\ue000"},
	{"unassigned raw fallback", "synthetic-\u1d2c", "synthetic-\u1d2c"},
	{"mapped empty raw fallback", "\u00ad", "\u00ad"},
	{"long normalized raw fallback", "q" + strings.Repeat("\u0301", 35), "q" + strings.Repeat("\u0301", 35)},
	{"long composed raw fallback", "á" + strings.Repeat("\u0301", 34), "á" + strings.Repeat("\u0301", 34)},
}

func TestSCRAMVerifierStoredAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	config, admin := newSCRAMTestSession(ctx, t)

	for i, test := range scramAuthenticationCases {
		t.Run(test.name, func(t *testing.T) {
			checkStoredSCRAMAuthentication(ctx, t, admin, config, i, test)
		})
	}
}

func newSCRAMTestSession(ctx context.Context, t *testing.T) (*pgx.ConnConfig, *pgx.Conn) {
	t.Helper()

	config, err := ParseConfig(requireContainer(t))
	if err != nil {
		t.Fatal("parse disposable PostgreSQL configuration failed")
	}

	config.RequireAuth = "scram-sha-256"

	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("connect disposable PostgreSQL failed")
	}

	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer closeCancel()

		if closeErr := admin.Close(closeCtx); closeErr != nil {
			t.Errorf("close disposable PostgreSQL: %v", closeErr)
		}
	})

	_, configureErr := admin.Exec(ctx, "SET log_statement = 'none'; SET log_min_error_statement = 'panic'; SET log_parameter_max_length_on_error = 0")
	if configureErr != nil {
		t.Fatal("configure credential statement logging failed")
	}

	return config, admin
}

func checkStoredSCRAMAuthentication(ctx context.Context, t *testing.T, admin *pgx.Conn, config *pgx.ConnConfig, index int, test scramAuthenticationCase) {
	t.Helper()

	verifier, err := SCRAMVerifier(test.password)
	if err != nil {
		t.Fatal("compatible password rejected")
	}

	role := fmt.Sprintf("pgxdb_scram_test_%d", index)
	identifier := pgx.Identifier{role}.Sanitize()
	// verifier는 고정 알고리즘·base64 문자만 사용한다. 원문은 SQL로 보내지 않는다.
	if _, createErr := admin.Exec(ctx, "CREATE ROLE "+identifier+" LOGIN PASSWORD '"+verifier+"'"); createErr != nil {
		t.Fatal("create verifier-backed test role failed")
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()

		if _, dropErr := admin.Exec(cleanupCtx, "DROP ROLE "+identifier); dropErr != nil {
			t.Errorf("drop verifier-backed test role: %v", dropErr)
		}
	})

	var storedUnchanged bool

	if scanErr := admin.QueryRow(ctx, "SELECT rolpassword = $2 FROM pg_authid WHERE rolname = $1", role, verifier).Scan(&storedUnchanged); scanErr != nil || !storedUnchanged {
		t.Fatal("PostgreSQL did not store the supplied verifier unchanged")
	}

	client := config.Copy()

	client.User = role
	checkSCRAMPassword(ctx, t, client, test.password, true)

	if test.equivalent != test.password {
		checkSCRAMPassword(ctx, t, client, test.equivalent, true)
	}

	checkSCRAMPassword(ctx, t, client, "wrong-synthetic-password", false)
}

func checkSCRAMPassword(ctx context.Context, t *testing.T, config *pgx.ConnConfig, password string, accepted bool) {
	t.Helper()

	config.Password = password

	conn, err := pgx.ConnectConfig(ctx, config)

	if conn != nil {
		if closeErr := conn.Close(ctx); closeErr != nil {
			t.Fatal("close authenticated connection failed")
		}
	}

	if accepted {
		if err != nil {
			t.Fatal("stored verifier rejected a compatible password")
		}

		return
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "28P01" {
		t.Fatal("incorrect password did not fail with invalid_password")
	}
}
