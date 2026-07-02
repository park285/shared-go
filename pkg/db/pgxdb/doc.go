// Package pgxdb는 jackc/pgx/v5 pgxpool 기반 PostgreSQL 연결 풀 생성 도구를 제공합니다.
//
// sslmode에는 기본값이 없다: 빈 값이면 Validate/DSN/OpenPool이 에러를 낸다. DSN에서 생략하면
// pgx가 "prefer"로 조용히 대체해 호출자의 TLS posture(verify-full·disable 등)를 바꿔버리므로,
// 호출자가 posture를 명시하도록 강제하는 숨은 계약이다.
//
// shared-go에서 pgx에 의존하는 유일한 패키지다. pgx 무의존 database/sql 풀 튜닝은 pkg/db/sqldb.
package pgxdb
