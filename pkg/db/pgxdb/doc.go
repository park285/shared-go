// Package pgxdb는 jackc/pgx/v5 pgxpool 기반 PostgreSQL 연결 풀 생성 도구를 제공합니다.
//
// sslmode에는 기본값이 없다. Config와 DSN 경로는 disable, allow, prefer, require,
// verify-ca, verify-full만 허용하고 공백·대소문자를 정규화한다. 운영 verify-full 강제는
// 소비자 정책이다. OpenPoolDSN과 ParseConfig는 URL·keyword/value DSN에 sslmode가 정확히
// 한 번 명시되어야 하며 PGSSLMODE 환경 변수만으로 생략을 허용하지 않는다.
// URI의 실제 query는 pgx/libpq의 bracket·multi-host 경계로 찾으며 authority 안의
// sslmode는 인정하지 않는다. sslmode 값만 정규화하고 나머지 query의 raw 세미콜론,
// '+', percent-encoded 바이트는 보존한다. sslmode 값의 form-encoded 주변 공백은
// 허용하지만 query key의 '+'는 공백으로 바꾸지 않는다.
// ParseConfig는 단일 연결용 pgx.ConnectConfig 입력을 만든다. 파싱 실패는 원문 DSN이나
// 비밀번호가 담길 수 있는 하위 오류를 노출하지 않고 고정 문구로 반환한다.
//
// # sslrootcert 계약
//
// Config 경유 경로(OpenPool)에서 DSN의 sslrootcert는 Config.SSLRootCert 하나로만 채운다.
// 이 패키지는 POSTGRES_SSLROOTCERT env를 읽지 않으므로, CA 경로를 env로 받는 소비자는 자기 설정
// 로더에서 읽어 필드로 넘긴다(DEC-20260926-stack-shared-go-compat-api-retirement). 필드가 trim 후
// 비면 sslrootcert를 DSN에서 생략하고 pgx 기본 처리(libpq 호환 PGSSLROOTCERT env, 없으면 시스템
// 인증서 풀)에 맡긴다. OpenPoolDSN은 sslmode를 정규화하면서 나머지 DSN 옵션을 보존한다.
// verify-ca·verify-full posture에서는 필드나 DSN이 실제 CA 파일 경로를 가리켜야 한다.
//
// # Pool configuration
//
// OpenPool requires explicit positive MaxConns and nonnegative MinConns. Zero
// MinConns is valid. DefaultPoolConfig and DefaultOptions are opt-in factories,
// not an implicit maximum. Negative options are rejected before any defaults.
// Structured zero lifetimes use 1h maximum lifetime and 30m idle time, with
// zero jitter derived as lifetime/5. Zero HealthCheckPeriod and Pool.PingTimeout
// preserve the parsed pgx values. A parsed health-check period must be positive.
// Pool.PingTimeout bounds acquire health pings, independently of startup Ping.
// Native failed-connection cleanup may retain its slot for up to 15s after the
// health ping ends. Use AcquirePolicy for the total acquisition budget; a ping
// timeout alone is not a bound on replacement or pool shutdown.
// RuntimeParams override parsed session parameters without retaining the caller
// map. Statement budgets and application names belong to runtime callers, not
// shared defaults or migrators. CancelRequest is opt-in and snapshots its policy;
// nil retains upstream cancellation. Cancellation never adds a retry or rewrites
// native SQLSTATE/ambiguous transaction outcomes.
//
// # Opt-in control acquisition capacity
//
// Options.AcquirePolicy is copied at construction. Nil leaves native pool
// acquisition unchanged, including maintenance/migration bypass APIs. A policy
// requires Timeout > 0 and 0 <= ReservedConns < effective MaxConns; reserve one
// requires max two, but MinConns zero remains valid. There are no implicit raises.
// Ordinary pending/acquired leases share MaxConns-ReservedConns permits, acquired
// before physical pool acquisition. WithControlAcquire bypasses only this gate,
// never the physical pool maximum or database privileges. Mark only a bounded,
// short existing lease/renewal/settlement/finalization operation, never a worker,
// provider, service or request lifecycle. Admission, reads, batches and maintenance
// remain ordinary. Use the existing transaction connection for nested SQL rather
// than acquiring another ordinary lease while holding a transaction.
//
// One timeout covers gate waiting plus native pool/connection/health acquisition,
// respects earlier parent deadlines, and ends when Acquire returns. SQL continues
// to use its original context and cancellation/SQLSTATE behavior. Ordinary permits
// survive application-owned connection, rows, QueryRow Scan, batch and transaction
// lifetimes until Conn.Release starts, or are returned on failed acquisition.
// TraceRelease precedes native return/destruction: pending driver cleanup or
// release callbacks can still occupy physical capacity and delay control acquire.
// This gate is not an exclusive physical partition. Release of closed connections,
// Reset and pool Close do not retain lease bookkeeping. Close still requires the
// caller to release acquired connections, as in native pgxpool.
//
// This integration relies on pinned pgx v5.11 Acquire/Release tracing and preserves
// existing query, batch, copy, prepare, connect and pool tracing callbacks via
// multitracer. AcquireAllIdle bypasses acquire tracing and Conn.Hijack bypasses
// release tracing; neither is supported on policy-enabled pools. Audit actual
// callers before activation. Nil-policy migration/maintenance pools retain those
// upstream APIs. Reserved capacity does not protect against server/network failure,
// exhausted control capacity, leaked leases or reentrant ordinary acquisition.
//
// Native pgx Stat wait durations measure native pool acquisition only, not the
// outside-pool ordinary gate. Worker deadlines include gate waiting. Observe
// queued ordinary operations and successful control operations directly when
// validating contention; do not infer total admission latency from pool metrics.
//
// # QueryExecMode 적용 경로
//
// Config.QueryExecMode는 DSN의 default_query_exec_mode 파라미터 한 곳으로만 적용된다.
// pgx가 이 파라미터를 파싱해 ConnConfig.DefaultQueryExecMode를 채우고 RuntimeParams에서
// 제거하므로, 값 검증은 Config.Validate가, 적용은 pgx가 담당한다. OpenPoolDSN도 같은 경로다.
//
// OpenPoolDSN은 다르다: pgxpool.ParseConfig가 DSN의 pool_* 파라미터를 파싱하며 미지정 필드에
// pgx 자체 기본값(MaxConns=max(4,NumCPU), MinConns=0, ConnMaxLifetime=1h, ConnMaxIdleTime=30m,
// jitter=0)을 채우므로 "DSN 지정" 값과 "pgx 기본" 값을 구분할 수 없다. 그래서 OpenPoolDSN은
// shared-go 기본값을 덮어쓰지 않고 opts.Pool에서 0보다 큰 필드만 파싱 결과 위에 override 한다.
// 미지정 풀 파라미터는 pgx 기본값에 위임한다. shared-go 기본값을 원하면 opts.Pool에
// DefaultPoolConfig()를 명시해 넘겨라.
// Changing only lifetime preserves parsed DSN jitter. Zero overlay fields mean
// absent, so disabling a nonzero DSN setting requires changing that DSN.
//
// # 연결 재시도
//
// OpenPool·OpenPoolDSN은 재시도하지 않는다: 연결·ping 실패는 그대로 반환하므로 compose 기동
// 레이스 등의 재시도 정책은 호출자가 소유한다. 연결 시 ping 타임아웃은
// Options.Ping.PingTimeout으로 정하며 미설정 시 5초다.
// With MinConns > 0, ConnectTimeout is clamped to that startup budget (unless
// already shorter) to bound background construction and failure cleanup. This
// is a persistent pool-lifetime setting, including subsequent reconnects, not a
// startup-only timeout. Server-cancel deadline grace is separate from both ping
// budgets and can extend an interrupted operation until its bounded deadline.
//
// # 트랜잭션 정리
//
// Rollback과 RollbackDeferred는 호출자 취소·기한과 분리된 5초 예산으로 rollback을 보낸다.
// 이미 종료된 트랜잭션은 성공으로 처리하고 다른 오류는 보존한다. RollbackDeferred는
// 호출자의 반환 오류와 정리 오류를 합치며, commit 결과 불명을 재시도로 바꾸지 않는다.
//
// shared-go에서 pgx에 의존하는 유일한 패키지다.
package pgxdb
