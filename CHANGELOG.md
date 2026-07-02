# Changelog

## v1.24.1 - 2026-07-02

### Fixed

- `db/pgxdb`: `OpenPoolWithRetry` no longer retries permanent failures. It now
  pre-validates `cfg.Validate()` and the pool connection-count range before entering
  the retry loop (config errors such as an `sslmode` typo return immediately), and the
  in-loop retry predicate treats authentication failures (`pgconn.PgError` SQLSTATE
  `28000`/`28P01`) and parent-context cancellation/deadline as permanent (immediate
  return). Recoverable startup-race errors — database-not-found (`3D000`), connection
  refused, DNS failures, and ping timeouts while the parent context is still live —
  remain retryable. Previously these permanent errors were retried for ~30s under the
  default `RetryConfig`.
- `db/pgxdb`: unified the pool default fallback to a single source. `withPoolDefaults`
  now sources `MinConns`/`MaxConns`/`ConnMaxLifetime`/`ConnMaxIdleTime` from
  `DefaultPoolConfig()` (env-tunable `DB_POOL_MIN_CONNS`/`DB_POOL_MAX_CONNS`, default
  5/20) instead of a divergent hardcoded 2/10 that ignored the env vars;
  `ConnMaxLifetimeJitter` is still derived as `ConnMaxLifetime/5`. `OpenPool(Options{})`
  and `DefaultOptions()` now yield the same pool configuration. `OpenPoolDSN` keeps its
  overlay semantics (it never overwrites DSN-specified `pool_*` parameters and delegates
  unset parameters to pgx's own defaults); pass `opts.Pool` (e.g. `DefaultPoolConfig()`)
  to apply shared-go defaults. The exact contract of all three entry points is now
  documented in `db/pgxdb/doc.go`.

## v1.8.0 - 2026-06-10

### BREAKING

Removed dead public API surface. Every removed symbol was verified to have zero call
sites across all stack consumers (hololive-bot, chat-bot-go-kakao) by exhaustive
package-qualified grep (2026-06-10 T3 plan, "실측 결과 1"). This module is internal to
the iris-stack workspace with no external consumers, which is the basis for shipping
these removals in a minor release despite the repo's API-stability policy — the
policy's intent (zero consumer impact) is satisfied by evidence rather than by
retention.

- `httputil`: removed `timeout_preset.go` entirely (`TimeoutPreset`, `FetchTimeout`,
  `LongPollTimeout`, `ScraperTimeout`, `Duration`, `NewClientWithPreset`,
  `NewExternalAPIClientWithPreset`, `NewInternalServiceClientWithPreset`).
- `httputil`: removed `DefaultClient`; use `NewProfiledClient` /
  `NewExternalAPIClient` / `NewInternalServiceClient`.
- `httputil`: removed `AsAPIError`; use `errors.As` with `*APIError` or `IsStatus`
  (which now inlines the unwrap).
- `runtime/httpserver`: removed the concrete `StartHTTPServer` / `ShutdownHTTPServer`
  pair; the generic `Start` / `Shutdown` / `StartServerWithPrefix` surface is
  unchanged.
- `healthprobe`: unexported `ParseURL` (now internal `parseURL`); use `CheckURL` /
  `FetchURL`.
- `stringutil`: removed `StripLeadingHeader`.

`pkg/telemetry` is intentionally retained (kept-reusable helper per AGENTS.md, held
for the OTel rollout).

### Added

- `envutil.StringOrFile`: reads `$KEY`, falling back to the contents of the file at
  `$KEY_FILE` (OpenBao secret-mount pattern), then to the default.
- `envutil.List` / `envutil.ListWithFallback`: comma/whitespace-separated list parser
  with trimming and de-duplication, sourced through `StringOrFile`.
- `envutil.Map`: `k:v` / `k=v` entry parser (comma/newline/tab separated), sourced
  through `StringOrFile`.
- `envutil.Bool` now recognizes the canonical 3-way truth set: `{1, true, yes, y, on}`
  → true, `{0, false, no, n, off}` → false, anything unrecognized → default
  (previously a 2-way set where unrecognized values collapsed to false). Existing
  true-set behavior is preserved; `BoolStrict` is unchanged.
