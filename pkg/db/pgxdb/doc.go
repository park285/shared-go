// Package pgxdb는 jackc/pgx/v5 pgxpool 기반 PostgreSQL 연결 풀 생성 도구를 제공합니다.
//
// sslmode에는 기본값이 없다: 빈 값이면 Validate/DSN/OpenPool이 에러를 낸다. OpenPoolDSN도
// URL 및 keyword/value DSN 원문에서 비어 있지 않은 sslmode가 정확히 한 번 명시됐는지
// pgx parsing 전에 검사한다. 생략하면 pgx가 "prefer"로 조용히 대체해 호출자의 TLS
// posture(verify-full·disable 등)를 바꿔버리므로 호출자가 posture를 명시해야 한다.
//
// # sslrootcert 이중 경로 계약
//
// Config.SSLRootCert와 POSTGRES_SSLROOTCERT env는 같은 DSN 파라미터(sslrootcert)를 채우는
// 두 경로다. buildDSN은 구조체 필드를 먼저 보고, trim 후 빈 값일 때에만 env로 폴백한다.
// 둘 다 비면 sslrootcert 자체를 DSN에서 생략해 pgx/libpq 기본 탐색
// 경로(~/.postgresql/root.crt 등)에 위임한다.
// 이 폴백은 Config 경유 경로(OpenPool)에만 적용된다: OpenPoolDSN은 호출자가 준 DSN 원문을
// 그대로 쓰므로 sslrootcert도 그 DSN에 직접 써야 한다. verify-ca·verify-full posture에서 CA를
// env로만 주는 배포는 env 이름 오타나 미주입이 곧 검증 실패로 이어지므로 양쪽 중 하나는
// 반드시 실제 파일 경로를 가리켜야 한다.
//
// # 풀 기본값(fallback) 계약
//
// OpenPool은 opts.Pool에서 미설정(0 이하)인 필드를 DefaultPoolConfig()와
// 동일한 단일 소스로 채운다: 풀 필드에 대해서는 env를 읽지 않고 호출자가 준 값만 사용한다.
// DefaultPoolConfig()는 정적 기본값 MinConns=0(pgx 기본, 풀 최소 크기 없음), MaxConns=20,
// ConnMaxLifetime=1h, ConnMaxIdleTime=30m을 반환하고, ConnMaxLifetimeJitter는 유효
// ConnMaxLifetime/5로 파생한다. MinConns 기본이 0이므로 소비자가 명시한 MinConns=0은
// 기본값 대체 없이 그대로 pgx에 전달된다(MaxConns=0은 pgx에 유효하지 않아 20으로 대체된다).
// DB_POOL_MIN_CONNS·DB_POOL_MAX_CONNS 등 풀 크기 env의 소유·검증·clamp는 소비자 책임이다.
// 따라서 OpenPool(ctx,cfg,Options{})와 DefaultOptions() 경유가 동일한 풀 구성을 만든다.
// MinConns가 MaxConns보다 크면 두 경로 모두 연결 전에 에러를 낸다(둘 다 0보다 클 때만 판정하므로
// overlay의 "미설정=0" 의미는 유지된다).
//
// HealthCheckPeriod만은 예외로 0 이하일 때 DefaultPoolConfig()가 아니라 pgx가 채운 값
// (ParseConfig 기본 1분, DSN의 pool_health_check_period가 있으면 그 값)에 위임한다.
// pgxpool이 이 값으로 time.NewTicker를 만들어 0이면 panic하기 때문이다.
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
//
// # 연결 재시도
//
// OpenPool·OpenPoolDSN은 재시도하지 않는다: 연결·ping 실패는 그대로 반환하므로 compose 기동
// 레이스 등의 재시도 정책은 호출자가 소유한다. 이 패키지가 소유하는 값은
// ping 타임아웃(Options.Ping.PingTimeout, 미설정 시 5초)뿐이다.
//
// shared-go에서 pgx에 의존하는 유일한 패키지다. pgx 무의존 database/sql 풀 튜닝은 pkg/db/sqldb.
package pgxdb
