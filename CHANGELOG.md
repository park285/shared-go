# Changelog

## Unreleased (v1.27.0)

### Removed (Breaking)

- `logging/archive`: internalized under `pkg/logging/internal/archive`; file logging behavior and
  log archive/prune semantics are unchanged through `pkg/logging`.
- `workerconfig`: unexported zero-consumer Iris worker-profile detail types
  (`BotPoolWorkerProfile`, `BotWebhookReceiveWorkerProfile`, `IrisWebhookDeliveryWorkerProfile`,
  `IrisBotWebhookWorkerProfileValidation`) and the zero-consumer
  `DefaultIrisBotWebhookWorkerProfile` helper.
- `json`: removed zero-consumer stdlib compatibility re-exports `Decoder` and `Number`; use
  `NewDecoder` or the concrete decoder returned from it.
- `logging`: unexported zero-consumer handler/plumbing helpers (`SanitizeHandler`,
  `OTelHandler`, `NewSanitizeHandler`, `NewOTelHandler`, `Component`, `JobID`, `NewID`,
  `ParseLevel`, and `*FromContext`). Public logger construction, operation logging,
  context enrichment, and file logging entry points remain.

### Changed

- `logging`: a literal `key` field or `?key=` query parameter no longer triggers redaction by
  name alone; `api_key`, `apikey`, token/password/secret variants, and suffix rules continue to
  redact.

### Docs

- Removed generated public-surface and internal-helper inventories from package `doc.go` files,
  keeping package overviews and usage examples.
- Annotated `REFACTORING_PLAN_20260602.md` with the closed P1/P3 dispositions for this wave.

## v1.26.0 - 2026-07-03

### Removed (Breaking)

- `retry`: removed `DefaultRetryOptions` — declaration-only surface with no callers across the
  stack; construct `RetryOptions` literals instead.
- `workerconfig`: removed `DecodeIrisBotWebhookWorkerProfile` — consumers decode through
  `DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics`, which remains the only entry point.
- `envutil`: unexported `SecretFile` (now `secretFile`) — external consumers read secret files
  through `StringOrSecretFile` / `FirstStringOrSecretFile` only.

## v1.25.0 - 2026-07-02

(Entry backfilled on 2026-07-03; the tag shipped without a changelog entry.)

### Added

- `obsmetrics`: labeled metric vectors (`Labels`, `CounterVec` / `GaugeVec` / `HistogramVec`
  with `NewCounterVec` / `NewGaugeVec` / `NewHistogramVec`) and labeled exposition writers
  (`WriteCounterWithLabels` / `WriteGaugeWithLabels` / `WriteHistogramWithLabels`).
- `h3`: `ServerOptions` plus `NewServerWithOptions` / `NewServerWithTLSConfigAndOptions`.

## v1.24.2 - 2026-07-02

### Fixed

- `runtime/lifecycle`: `RunCloseSteps` now recovers a panic from an individual
  close step, converts it to an error (preserving `errors.Is` identity when the
  panic value is an error) so it is aggregated via `errors.Join`, and continues
  running the remaining steps. Previously a panicking step aborted the whole
  shutdown and skipped every later resource-cleanup step.

### Changed

- `llm/openaipreset`: `WithReasoningEffort` now stores the whitespace-trimmed value
  instead of the raw input, preserving the normalization consumers (twentyq) applied
  before passing it in. Blank/whitespace-only input is still ignored.
- `.gitguardian.yaml`: narrowed the blanket `**/*_test.go` secret-scan exclusion to
  the three packages whose tests must embed synthetic secrets to verify redaction /
  output-guard / secret-file handling (`pkg/logging`, `pkg/outputguard`,
  `pkg/envutil`). The `pgxdb` test fixtures were renamed to placeholders in ab42ac6
  and no longer rely on a path exclusion, so all other `*_test.go` files are scanned
  again.

### Docs

- `runtime/lifecycle`: added `doc.go` documenting the non-obvious `RunCloseSteps`
  contract (slice order, not reverse; every step runs even after a failure; steps
  run even under an already-cancelled context; `errors.Join` aggregation;
  panic-to-error conversion).
- `retry`: documented the context-error precedence contract — when context
  cancellation and a prior `fn` error coexist, `WithRetry` returns the operational
  (last `fn`) error; the wrapped `context error: <ctx.Err()>` is returned only when
  no prior `fn` error exists. Behavior unchanged.
- `db/pgxdb`: documented that `DNSFallback=true` triggers the localhost fallback only
  when the configured host is exactly `postgres` (case-insensitive) and the connect
  error is a DNS "no such host" error for that host.

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
