package pgxdb

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSSLMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{testDisable, "allow", "prefer", "require", "verify-ca", testVerifyFull} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeSSLMode(" \t" + strings.ToUpper(mode) + "\n")
			if err != nil || got != mode {
				t.Fatalf("NormalizeSSLMode = %q, %v; want %q", got, err, mode)
			}
		})
	}

	for _, mode := range []string{"", " \t\n", "bogus", "verify_full", "require disable"} {
		if _, err := NormalizeSSLMode(mode); err == nil {
			t.Errorf("NormalizeSSLMode(%q) must fail", mode)
		}
	}
}

func TestParseConfigNormalizesExplicitModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", testVerifyFull} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			inputMode := " " + strings.ToUpper(mode) + " "
			cfg := Config{Host: "db.example", Port: 5432, User: "app", Password: `dummy'password\tail`, Name: "app", SSLMode: inputMode}

			dsn, err := cfg.DSN()
			if err != nil {
				t.Fatal(err)
			}

			safeDSN, err := cfg.SafeDSN()
			if err != nil {
				t.Fatal(err)
			}

			for _, raw := range []string{dsn, safeDSN} {
				parsed, parseErr := pgx.ParseConfig(raw)
				require.NoError(t, parseErr)

				assertTLSMode(t, parsed, mode)
			}

			poolCfg, err := buildConfigPool(&cfg, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}

			assertTLSMode(t, poolCfg.ConnConfig, mode)

			if poolCfg.ConnConfig.Password != cfg.Password {
				t.Fatal("structured config lost its original password")
			}

			for _, raw := range []string{
				dsn,
				safeDSN,
				"postgres://app@db.example/app?sslmode=" + url.QueryEscape(inputMode),
				"host=db.example user=app dbname=app sslmode=" + libpqQuote(inputMode),
			} {
				parsed, err := ParseConfig(raw)
				if err != nil {
					t.Fatal(err)
				}

				assertTLSMode(t, parsed, mode)
			}
		})
	}
}

func assertTLSMode(t *testing.T, cfg *pgx.ConnConfig, mode string) {
	t.Helper()

	switch mode {
	case "disable":
		require.Nil(t, cfg.TLSConfig)
		require.Empty(t, cfg.Fallbacks)
	case "allow":
		require.Nil(t, cfg.TLSConfig)
		require.Len(t, cfg.Fallbacks, 1)
		require.NotNil(t, cfg.Fallbacks[0].TLSConfig)
	case "prefer":
		require.NotNil(t, cfg.TLSConfig)
		require.Len(t, cfg.Fallbacks, 1)
		require.Nil(t, cfg.Fallbacks[0].TLSConfig)
	case "require":
		require.NotNil(t, cfg.TLSConfig)
		require.Empty(t, cfg.Fallbacks)
	case "verify-ca":
		require.NotNil(t, cfg.TLSConfig)
		require.NotNil(t, cfg.TLSConfig.VerifyPeerCertificate)
		require.Empty(t, cfg.Fallbacks)
	case testVerifyFull:
		require.NotNil(t, cfg.TLSConfig)
		require.False(t, cfg.TLSConfig.InsecureSkipVerify)
		require.Equal(t, "db.example", cfg.TLSConfig.ServerName)
		require.Empty(t, cfg.Fallbacks)
	}
}

func TestParseConfigPreservesEscapedCredentialsAndOptions(t *testing.T) {
	t.Parallel()

	const password = `dummy'quoted\ password sslmode=bogus` //nolint:gosec // Deliberately malformed test credential; never used for authentication.

	for _, dsn := range []string{
		"host=db.example user=app password=" + libpqQuote(password) + " sslmode=' DISABLE ' application_name='migration worker' connect_timeout=7 default_query_exec_mode=exec",
		"postgres://" + url.UserPassword("app", password).String() + "@db.example/app?sslmode=%20DISABLE%20&application_name=migration%20worker&connect_timeout=7&default_query_exec_mode=exec",
	} {
		cfg, err := ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}

		if cfg.Password != password || cfg.RuntimeParams["application_name"] != "migration worker" || cfg.ConnectTimeout.Seconds() != 7 || cfg.DefaultQueryExecMode != pgx.QueryExecModeExec {
			t.Fatal("normalization changed credentials or connection options")
		}
	}
}

func TestParseConfigPreservesURLParameterEscaping(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoded string
		want    string
	}{
		{"migration%20worker", "migration worker"},
		{"migration+worker", "migration+worker"},
		{"migration%2Bworker", "migration+worker"},
		{"migration%26worker", "migration&worker"},
	} {
		cfg, err := ParseConfig("postgres://app@db.example/app?sslmode=%20DISABLE%20&application_name=" + test.encoded)
		if err != nil {
			t.Fatal(err)
		}

		if got := cfg.RuntimeParams["application_name"]; got != test.want {
			t.Errorf("application_name=%q: got %q, want %q", test.encoded, got, test.want)
		}
	}
}

func TestParseConfigPreservesPostgresURIAuthorities(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"postgres://%2Fvar%2Flib%2Fpostgresql/chatbot?sslmode=disable",
		"postgres://db1:5433,db2/chatbot?sslmode=disable",
		"postgres://db1,db2:5434/chatbot?sslmode=disable",
		"postgres://[::1]:5433,db2/chatbot?sslmode=disable",
		"postgres://app:dummy?value@db1:5433,db2/chatbot?sslmode=disable",
	} {
		want, err := pgx.ParseConfig(raw)
		require.NoError(t, err)

		got, err := ParseConfig(strings.Replace(raw, "sslmode=disable", "sslmode=%20DISABLE%20", 1))
		require.NoError(t, err)
		require.Equal(t, want.Host, got.Host)
		require.Equal(t, want.Port, got.Port)
		require.Equal(t, want.Database, got.Database)
		require.Equal(t, want.User, got.User)
		require.Equal(t, want.Password, got.Password)
		require.Equal(t, want.Fallbacks, got.Fallbacks)
	}
}

func TestParseConfigPreservesSemicolonQueryValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		query       string
		application string
		password    string
	}{
		{"raw application semicolon", "application_name=worker;blue&password=dummy", "worker;blue", "dummy"},
		{"raw password semicolon", "application_name=worker&password=dummy;PASSWORD", "worker", "dummy;PASSWORD"},
		{"encoded semicolons", "application_name=worker%3Bblue&password=dummy%3BPASSWORD", "worker;blue", "dummy;PASSWORD"},
		{"mixed escaped bytes", "application_name=worker;+blue%20team%26one%3Dtwo&password=dummy;+%20%26%3D%25%FF", "worker;+blue team&one=two", "dummy;+ &=%\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prefix := "postgres://app@db.example:5433/app?" + tc.query + "&connect_timeout=7&default_query_exec_mode=exec&sslmode="
			want, err := pgx.ParseConfig(prefix + "disable")
			require.NoError(t, err)

			got, err := ParseConfig(prefix + "%20DISABLE%20")
			require.NoError(t, err)
			require.Equal(t, tc.application, got.RuntimeParams["application_name"])
			require.Equal(t, tc.password, got.Password)
			require.Equal(t, want.RuntimeParams, got.RuntimeParams)
			require.Equal(t, want.Password, got.Password)
			require.Equal(t, want.User, got.User)
			require.Equal(t, want.Database, got.Database)
			require.Equal(t, want.Host, got.Host)
			require.Equal(t, want.Port, got.Port)
			require.Equal(t, want.ConnectTimeout, got.ConnectTimeout)
			require.Equal(t, want.DefaultQueryExecMode, got.DefaultQueryExecMode)
			assertTLSMode(t, got, "disable")
		})
	}
}

func TestParseConfigUsesActualQuerySSLMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		authority string
		mode      string
	}{
		{"bracket disable cannot shadow verify full", "[?sslmode=disable&x=]", testVerifyFull},
		{"bracket verify full cannot shadow disable", "[?sslmode=verify-full&x=]", "disable"},
		{"later host bracket", "db1:5433,[?sslmode=disable&x=]:5434", testVerifyFull},
		{"bracket slash is not a path", "[segment/?sslmode=disable&x=],db2", testVerifyFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prefix := "postgres://app@" + tc.authority + "/app?host=db.example&port=5432&sslmode="
			want, err := pgx.ParseConfig(prefix + tc.mode)
			require.NoError(t, err)

			got, err := ParseConfig(prefix + "%20" + strings.ToUpper(tc.mode) + "%20")
			require.NoError(t, err)
			require.Equal(t, want.Host, got.Host)
			require.Equal(t, want.Port, got.Port)
			require.Equal(t, want.Database, got.Database)
			require.Equal(t, want.RuntimeParams, got.RuntimeParams)
			assertTLSMode(t, got, tc.mode)
		})
	}
}

func TestParseConfigPreservesQueryKeyBoundaries(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"%73slmode=disable",
		" sslmode =disable",
		"sslmode=disable&%20sslmode%20=require",
		"sslmode=disable&sslmode+=require",
		"sslmode=disable&sslmode%2B=require",
		"sslmode=disable&%2573slmode=require",
		"sslmode=disable&application_name=sslmode%3Drequire%26sslmode%3Dprefer",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			prefix := "postgres://app@db.example/app?"
			want, err := pgx.ParseConfig(prefix + query)
			require.NoError(t, err)

			got, err := ParseConfig(prefix + strings.Replace(query, "=disable", "=%20DISABLE%20", 1))
			require.NoError(t, err)
			require.Equal(t, want.RuntimeParams, got.RuntimeParams)
			assertTLSMode(t, got, "disable")
		})
	}
}

func TestDSNRejectsAuthoritySSLModeDespiteEnvironment(t *testing.T) {
	t.Setenv("PGSSLMODE", "disable")

	// Even a regressed pool parser can only address this test-owned socket
	// directory; the canceled context distinguishes validation from dialing.
	host := t.TempDir()
	query := "?host=" + url.PathEscape(host) + "&port=5432&pool_min_conns=0"

	for _, authority := range []string{
		"[?sslmode=verify-full&x=]",
		"db1:5433,[?sslmode=verify-full&x=]:5434",
		"[?sslmode=verify-full&x=]:5433,db2:5434",
		"[segment/?sslmode=verify-full&x=],db2",
	} {
		t.Run(authority, func(t *testing.T) {
			dsn := "postgres://app@" + authority + "/app" + query
			inherited, err := pgx.ParseConfig(dsn)
			require.NoError(t, err)
			require.Equal(t, host, inherited.Host)
			require.Nil(t, inherited.TLSConfig)

			assertDSNRejectedBeforeConnect(t, dsn)
		})
	}
}

func assertDSNRejectedBeforeConnect(t *testing.T, dsn string) {
	t.Helper()

	cfg, err := ParseConfig(dsn)
	require.Error(t, err)
	require.Nil(t, cfg)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	pool, err := OpenPoolDSN(ctx, dsn, Options{})
	if pool != nil {
		t.Cleanup(pool.Close)
	}

	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, pool)
}

func TestDSNParseErrorsDoNotExposeCredentials(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		dsn  string
	}{
		{"malformed URL userinfo", "postgres://app:dummy%zzPASSWORD@localhost/app?sslmode=disable"},
		{"malformed URL host", "postgres://app:dummyPASSWORD@[bad/app?sslmode=disable"},
		{"malformed query password", "postgres://app@localhost/app?sslmode=disable&password=dummy%zzPASSWORD"},
		{"missing query separator", "postgres://app@localhost/app?sslmode=disable&dummyPASSWORD"},
		{"extra query separator", "postgres://app@localhost/app?sslmode=disable&password=dummy=PASSWORD"},
		{"empty query pair", "postgres://app@localhost/app?sslmode=disable&&password=dummyPASSWORD"},
		{"empty bracket host", "postgres://app:dummyPASSWORD@[]/app?sslmode=disable"},
		{"invalid bracket suffix", "postgres://app:dummyPASSWORD@[::1]junk/app?sslmode=disable"},
		{"query password pgx failure", "postgres://app@localhost/app?sslmode=disable&password=dummyPASSWORD&connect_timeout=bogus"},
		{"escaped quote keyword password", `host=localhost password='dummy\'PASSWORD' sslmode=disable connect_timeout=bogus`},
		{"escaped space keyword password", `host=localhost password=dummy\ PASSWORD sslmode=disable connect_timeout=bogus`},
		{"unterminated password", `host=localhost sslmode=disable password='dummyPASSWORD`},
		{"invalid escape password", "host=localhost sslmode=disable password=dummyPASSWORD\\"},
		{"parser error value", "postgres://app@localhost/app?sslmode=disable&connect_timeout=dummyPASSWORD"},
		{"invalid sslmode", "postgres://app@localhost/app?sslmode=dummyPASSWORD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			_, parseErr := ParseConfig(tc.dsn)
			_, poolErr := OpenPoolDSN(ctx, tc.dsn, Options{})

			for _, err := range []error{parseErr, poolErr} {
				if err == nil {
					t.Fatal("invalid DSN was accepted")
				}

				require.NotErrorIs(t, err, context.Canceled)

				for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
					if strings.Contains(text, "dummy") || strings.Contains(text, "PASSWORD") {
						t.Fatal("DSN error exposed credential material")
					}
				}
			}
		})
	}
}

func TestOpenPoolDSNPoolParseErrorIsSecretSafe(t *testing.T) {
	t.Parallel()

	_, err := OpenPoolDSN(t.Context(), `host=localhost password='dummy\'PASSWORD' sslmode=disable pool_max_conns=dummyPASSWORD`, Options{})
	if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "PASSWORD") || strings.Contains(err.Error(), "dummy") {
		t.Fatal("pool parse error must reject invalid options without exposing credentials")
	}
}

func TestDSNRequiresExplicitSSLModeDespiteEnvironment(t *testing.T) {
	t.Setenv("PGSSLMODE", "disable")

	host := t.TempDir()
	urlPrefix := "postgres://app@" + url.PathEscape(host) + "/app?"

	for _, dsn := range []string{
		"postgres://app@localhost/app",
		"postgres://app@localhost/app?ssl=true",
		"postgres://app@localhost/app?sslmode=",
		"postgres://app@localhost/app?sslmode=disable&sslmode=disable",
		urlPrefix + "sslmode=disable&%73slmode=disable",
		urlPrefix + "%73slmode=disable&ssl%6Dode=require",
		urlPrefix + "sslmode=disable& sslmode =require",
		urlPrefix + "%20sslmode%20=disable",
		urlPrefix + "%2573slmode=disable",
		urlPrefix + "sslmode+=disable",
		urlPrefix + "sslmode=req+uire",
		urlPrefix + "sslmode=%2Bdisable%2B",
		urlPrefix + "application_name=sslmode%3Ddisable",
		"host=localhost user=app",
		"host=localhost sslmode=''",
		"host=localhost sslmode=disable sslmode=disable",
	} {
		assertDSNRejectedBeforeConnect(t, dsn)
	}
}
