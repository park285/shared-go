# shared-go v1.27.0 Admin Surface Adoption Guide

## Scope

This guide maps the bot-side duplicate admin HTTP helpers to shared-go APIs added for v1.27.0.
Consumer repositories remain unchanged in this task.

## chat-bot-go-kakao

- `internal/admin/access.go`
  - Replace `crypto/subtle` token comparison with `httputil.ConstantTimeStringEqual(r.Header.Get(HeaderAdminToken), a.token)`.
  - Keep role parsing, principal lookup, `AuthorizeUser`, and status ordering local; those are app-specific and not promoted.
- `internal/admin/ratelimit.go`
  - Replace `NewRateLimiter(limit, window)` with `httputil.NewFixedWindowRateLimiter(limit, window, httputil.FixedWindowOptions{})`.
  - Replace `limiter.Allow(userID)` with the shared limiter's `Allow(userID)`.
- `cmd/healthcheck/main.go`
  - Already matches the shared entry point:
    `os.Exit(healthprobe.RunMain(os.Args, os.Stdout, os.Stderr))`.
- `internal/config/env.go`, `internal/config/load.go`
  - Replace local dotenv parser and `loadLocalDotEnvFileIfEnabled` with:
    `envutil.LoadDotenv(envutil.DotenvOptions{LocalEnableKey: "CHATBOTGO_LOAD_DOTENV", LocalPathKey: "CHATBOTGO_DOTENV_PATH"})`.
  - Keep `parseLogLevel` and service config assembly local.
- `internal/dbmigrate/dbmigrate.go`
  - Keep the repo-local `scripts/migrations.FS` import.
  - Implement `Manifest()` as `return dbmigrate.Manifest(migrations.FS)`.
  - Implement `Apply(ctx, db)` as `return dbmigrate.Apply(ctx, migrations.FS, dbmigrate.SQLExec(db))`.
- `cmd/db-migrate/main.go`
  - Keep advisory lock/session timeout logic in the command layer. The shared core intentionally only owns manifest ordering and SQL execution.
- logging call sites
  - `internal/runtimekit/logger.go` returns the file logger through `app.New`, but does not update `slog.Default()`. After this breaking change, add `slog.SetDefault(logger)` after `runtimekit.NewLogger`/`newLogger` succeeds if package-level `slog.*` calls should write to the file logger.

## twentyq-bot

- `internal/common/httputil/admin_auth.go`
  - Replace the file with type aliases or direct imports:
    `sharedhttputil.AdminAuthConfig`, `sharedhttputil.AdminAuthMiddleware`.
  - Map existing `Enabled` config to the inverted shared flag: `Disabled: !cfg.Enabled`. Disabled-auth pass-through is now explicit opt-in; `AdminAuthConfig{}` enforces auth and returns the empty-secret 503 path.
  - `X-API-Key`, Bearer fallback, empty-secret 503, and invalid/missing 401 semantics are preserved.
- `internal/common/httputil/ratelimit.go`
  - Parse proxies with `sharedhttputil.ParseTrustedProxies(cfg.TrustedProxies)`.
  - Build identity with:
    `sharedhttputil.RateLimitIdentity(r, sharedhttputil.APIKeyFromRequest(r), sharedhttputil.ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted, ForwardedMode: sharedhttputil.ForwardedHeaderLeftmost})`.
  - Use `sharedhttputil.NewFixedWindowRateLimiter(cfg.RequestsPerMinute, time.Minute, sharedhttputil.FixedWindowOptions{MaxIdentities: cfg.CacheSize, EntryTTL: time.Duration(cfg.CacheTTLSeconds) * time.Second})`.
  - Wrap with `sharedhttputil.FixedWindowRateLimitMiddleware`, `Skip` for `OPTIONS`, and `Reject: sharedhttputil.WriteRateLimitExceededJSON`.
- `internal/common/httpx/ratelimit_identity.go`
  - Replace `ParseTrustedProxies`, `RateLimitIdentity`, `ClientIP`, and `RateLimitKeyHash` with the same-named shared-go `pkg/httputil` functions.
- `cmd/healthcheck/main.go`
  - Already matches `healthprobe.RunMain(os.Args, os.Stdout, os.Stderr)`.
- `internal/common/config/dotenv.go`
  - Replace `LoadDotenvIfPresent(paths ...string)` internals with:
    `envutil.LoadDotenv(envutil.DotenvOptions{ServiceName: "twentyq", LocalPaths: paths})`.
  - Remove `github.com/joho/godotenv` from twentyq after no other call sites remain.
- `internal/common/config/env.go`
  - Existing wrappers around `envutil.StringOrSecretFile`, `FirstStringOrSecretFile`, `StringAny`, and list parsing can be shortened incrementally; no behavior change is required for this wave.
- `internal/dbmigrate/dbmigrate.go`
  - Keep repo-local migration name constants and `scripts/migrations.FS`.
  - Implement `Manifest()` as `return dbmigrate.Manifest(migrations.FS)`.
  - Keep pgx advisory lock acquire/release local or wrap it around:
    `dbmigrate.Apply(ctx, migrations.FS, func(ctx context.Context, query string) error { _, err := conn.Exec(ctx, query); return err }, dbmigrate.WithOnly(...))`.
- logging call sites
  - `cmd/twentyq/main.go` calls `slog.SetDefault(logger)` before the file logger exists. After this breaking change, `internal/common/bootstrap/entrypoint.go` must call `slog.SetDefault(logger)` after `EnableFileLoggingWithOTel` succeeds and `logger` is replaced with `fileLogger`, or the process default silently stops being the file logger.

## hololive-bot

- `hololive/hololive-shared/pkg/server/middleware/auth.go`
  - Replace `constantTimeEqualSecret` with `httputil.ConstantTimeStringEqual`.
  - Replace `APIKeyAuthMiddleware` and `NoRouteAuthHandler` bodies with wrappers around `github.com/park285/shared-go/pkg/httputil/ginauth`.
  - Preserve the local `APIKeyHeader` re-export only if downstream imports it from `middleware`.
- `admin-dashboard/backend/internal/auth/crypto.go`
  - Replace `ConstantTimeStringEqual` with `httputil.ConstantTimeStringEqual`.
  - Session HMAC, CSRF, random ID, and truncation helpers stay local.
- `admin-dashboard/backend/internal/auth/rate_limiter.go`
  - Replace `LoginRateLimiter` with `httputil.LoginFailureRateLimiter`.
  - Use `httputil.NewDefaultLoginFailureRateLimiter()` for current defaults, or `NewLoginFailureRateLimiter` if tests need an injected clock.
- `admin-dashboard/backend/internal/app/middleware.go`
  - Replace `clientIP`, `ipInTrustedProxy`, `forwardedClientIP`, and `clientIPFromXFF` with:
    `httputil.ClientIP(req, httputil.ClientIPOptions{TrustForwarded: cfg.TrustedForwarders, TrustedProxies: cfg.TrustedProxyCIDRs, ForwardedMode: httputil.ForwardedHeaderRightmostNonTrusted})`.
  - Convert config CIDRs to `[]netip.Prefix` via `httputil.ParseTrustedProxyCSV`.
- bootstrap/logging call sites
  - `hololive-api/cmd/hololive-api/main.go` passes the file logger through `runtime/bootstrap.Run`; that helper does not set `slog.Default()`. Add `slog.SetDefault(logger)` after `EnableFileLoggingWithOptions` succeeds if package-level `slog.*` calls should use the file logger.
  - `hololive-api/internal/planes/bot/cmd/warm_member_cache/main.go` currently uses the returned `logger` for normal logs and package-level `slog.Error` before/around logger initialization. Add `slog.SetDefault(logger)` after `EnableFileLoggingWithLevel` succeeds only if later package-level `slog.*` calls should use the file logger.
