// Package pgxdb는 jackc/pgx/v5 pgxpool 기반 PostgreSQL 연결 풀 생성 도구를 제공합니다.
//
// sslmode에는 기본값이 없다: 빈 값이면 Validate/DSN/OpenPool이 에러를 낸다. OpenPoolDSN도
// URL 및 keyword/value DSN 원문에서 비어 있지 않은 sslmode가 정확히 한 번 명시됐는지
// pgx parsing 전에 검사한다. 생략하면 pgx가 "prefer"로 조용히 대체해 호출자의 TLS
// posture(verify-full·disable 등)를 바꿔버리므로 호출자가 posture를 명시해야 한다.
//
// # 풀 기본값(fallback) 계약
//
// OpenPool·OpenPoolWithRetry는 opts.Pool에서 미설정(0 이하)인 필드를 DefaultPoolConfig()와
// 동일한 단일 소스로 채운다: MinConns/MaxConns는 env(DB_POOL_MIN_CONNS·DB_POOL_MAX_CONNS,
// 기본 5/20, 각 [1,100]·[1,200] clamp), ConnMaxLifetime=1h, ConnMaxIdleTime=30m이고,
// ConnMaxLifetimeJitter는 유효 ConnMaxLifetime/5로 파생한다. 따라서 OpenPool(ctx,cfg,Options{})와
// DefaultOptions() 경유가 동일한 풀 구성을 만든다.
//
// OpenPoolDSN은 다르다: pgxpool.ParseConfig가 DSN의 pool_* 파라미터를 파싱하며 미지정 필드에
// pgx 자체 기본값(MaxConns=max(4,NumCPU), MinConns=0, ConnMaxLifetime=1h, ConnMaxIdleTime=30m,
// jitter=0)을 채우므로 "DSN 지정" 값과 "pgx 기본" 값을 구분할 수 없다. 그래서 OpenPoolDSN은
// shared-go 기본값을 덮어쓰지 않고 opts.Pool에서 0보다 큰 필드만 파싱 결과 위에 override 한다.
// 미지정 풀 파라미터는 pgx 기본값에 위임한다. shared-go 기본값을 원하면 opts.Pool에
// DefaultPoolConfig()를 명시해 넘겨라.
//
// # OpenPoolWithRetry의 fail-fast
//
// OpenPoolWithRetry는 재시도 루프 진입 전 cfg.Validate()와 풀 conn 수 범위를 선검증하므로 설정
// 오류(sslmode 오타, int32 범위 밖 MaxConns 등)는 재시도 없이 즉시 반환한다. 루프 안에서는
// 영구 에러를 재시도하지 않는다: 인증 실패(pgconn.PgError SQLSTATE 28000·28P01)와 context
// 취소·만료는 즉시 중단한다. DB 미존재(3D000)·연결 거부·DNS 실패처럼 compose 기동 레이스에서
// 회복 가능한 에러는 계속 재시도한다. ping 타임아웃(자식 context 만료)은 부모 context가 살아
// 있으면 재시도 대상으로 남는다.
//
// # localhost 폴백(DNSFallback) 계약
//
// OpenPool은 opts.DNSFallback=true일 때만 127.0.0.1로 재연결을 시도한다. 다만 이 폴백은
// cfg.Host가 정확히 "postgres"(대소문자 무시)이고 최초 연결 실패가 그 host에 대한 DNS
// "no such host" 에러일 때에만 발동한다(errors.go의 ShouldFallbackToLocalhost·
// isFallbackEligibleHost). ""·"127.0.0.1"·"localhost"나 그 외 임의 host — 오타 난 "postgress"
// 같은 값 포함 — 는 DNSFallback=true여도 폴백 대상이 아니다. compose 서비스명 "postgres"를
// 로컬에서 직접 실행할 때의 이름 해석 실패만 좁게 구제하려는 의도된 제약이다.
//
// shared-go에서 pgx에 의존하는 유일한 패키지다. pgx 무의존 database/sql 풀 튜닝은 pkg/db/sqldb.
package pgxdb
