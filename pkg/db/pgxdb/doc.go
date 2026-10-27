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
// POSTGRES_SSLROOTCERT는 직접 읽지 않는다. 소비자가 환경 변수를 읽어 필드로 넘긴다.
// 필드가 공백이면 sslrootcert를 생략해 pgx 기본 처리(PGSSLROOTCERT, 없으면 시스템
// 인증서 풀)에 맡긴다. OpenPoolDSN은 sslmode 외 DSN 옵션을 보존한다.
// verify-ca·verify-full posture에서는 필드나 DSN이 실제 CA 파일 경로를 가리켜야 한다.
//
// # 풀 설정
//
// OpenPool은 MaxConns > 0, MinConns >= 0을 요구하며 음수 옵션은 기본값 적용 전에
// 거부한다. 라이브러리 기본값은 없으며 호출자가 풀 크기·수명과 ping 기한을 Options에 명시한다.
// 두 경로 모두 Ping.PingTimeout > 0이 필수다. 구조체 경로는 ConnMaxLifetime·ConnMaxIdleTime > 0이
// 필수이고 ConnMaxLifetimeJitter 0은 지터 없음이다.
// HealthCheckPeriod와 Pool.AcquirePingTimeout이 0이면 pgx 파싱값을 유지하며 검사 주기는 양수여야 한다.
// Pool.AcquirePingTimeout은 시작 Ping과 별개로 획득 시 상태 검사를 제한한다.
// 검사 종료 후에도 실패한 연결 정리가 최대 15초간 슬롯을 점유할 수 있으므로
// 전체 획득 기한에는 AcquirePolicy를 사용한다. Ping 기한만으로 교체·풀 종료는 제한되지 않는다.
// RuntimeParams는 호출자 맵을 보관하지 않고 세션 매개변수를 덮어쓴다.
// SQL 실행 기한·애플리케이션 이름은 공통 기본값이나 마이그레이터가 아닌 런타임 호출자가 정한다.
// CancelRequest는 생성 시 복사하며 nil이면 기본 취소를 유지한다.
// 취소는 재시도를 추가하거나 SQLSTATE·결과 불명 트랜잭션을 바꾸지 않는다.
//
// # 선택적 제어 작업 예약 용량
//
// Options.AcquirePolicy는 생성 시 복사하며 nil이면 유지보수·마이그레이션 우회 API를
// 포함한 기본 풀 동작을 유지한다. Timeout > 0, 0 <= ReservedConns < 유효 MaxConns가
// 필요하고 값을 암묵적으로 올리지 않는다. MinConns는 0이어도 된다.
// 일반 작업은 실제 연결 획득 전에 MaxConns-ReservedConns 허가를 얻는다.
// WithControlAcquire는 이 게이트만 우회하며 물리 풀 최대값이나 DB 권한은 바꾸지 않는다.
// 기한이 짧고 제한된 임대·갱신·정산·종결 작업에만 표시한다. 워커·공급자·서비스·요청의
// 전체 수명에는 표시하지 않으며 입장·조회·배치·유지보수는 일반 작업으로 남긴다.
// 트랜잭션 내 SQL은 추가 일반 연결을 획득하지 말고 기존 연결을 사용한다.
//
// 하나의 기한이 게이트 대기와 실제 풀·연결·상태 검사를 포함하며 부모의 더 이른 기한을
// 따른다. Acquire 반환 시 끝나며 SQL의 원래 컨텍스트·취소·SQLSTATE 동작은 유지한다.
// 일반 허가는 연결·행·QueryRow Scan·배치·트랜잭션 수명 동안 유지하다가
// Conn.Release 시작 또는 획득 실패 시 반환한다.
// TraceRelease 이후에도 드라이버 정리·반환 콜백이 물리 용량을 점유해 제어 획득을
// 지연할 수 있으므로 예약 용량은 전용 물리 파티션이 아니다.
// 닫힌 연결 반환·Reset·Close는 임대 기록을 남기지 않지만 Close 전 연결 반환은 호출자 책임이다.
//
// 고정된 pgx v5.11의 획득·반환 추적을 사용하며 기존 쿼리·배치·복사·준비·접속·풀 추적은
// multitracer로 보존한다. AcquireAllIdle은 획득 추적을, Conn.Hijack은 반환 추적을
// 우회하므로 정책 적용 풀에서는 지원하지 않는다. 활성화 전에 실제 호출부를 확인해야 한다.
// 정책이 없는 유지보수·마이그레이션 풀에서는 해당 API를 그대로 사용할 수 있다.
// 예약 용량은 서버·네트워크 장애, 제어 용량 소진, 임대 누수, 재진입 일반 획득을 막지 못한다.
//
// pgx Stat 대기 시간은 외부 게이트를 제외하지만 워커 기한은 포함한다.
// 경합 검증은 풀 메트릭으로 전체 지연을 추정하지 말고 일반 작업 대기와 제어 작업 성공을 직접 관측한다.
//
// # QueryExecMode 적용 경로
//
// Config.QueryExecMode는 DSN의 default_query_exec_mode 파라미터 한 곳으로만 적용된다.
// pgx가 이 파라미터를 파싱해 ConnConfig.DefaultQueryExecMode를 채우고 RuntimeParams에서
// 제거하므로, 값 검증은 Config.Validate가, 적용은 pgx가 담당한다. OpenPoolDSN도 같은 경로다.
//
// OpenPoolDSN은 pgxpool.ParseConfig의 DSN 값과 기본값을 유지하고 opts.Pool의
// 양수 필드만 덮어쓴다. 수명만 바꾸면 DSN 지터는 유지된다. 덮어쓸 필드의 0은 미지정이므로
// DSN의 0이 아닌 설정을 비활성화하려면 DSN을 바꿔야 한다.
//
// # 연결 재시도
//
// OpenPool·OpenPoolDSN은 재시도하지 않는다: 연결·ping 실패는 그대로 반환하므로 compose 기동
// 레이스 등의 재시도 정책은 호출자가 소유한다. 연결 시 ping 타임아웃은
// 필수인 Options.Ping.PingTimeout으로 정한다.
// MinConns > 0이면 백그라운드 연결 생성·실패 정리를 제한하도록 ConnectTimeout을
// 시작 예산 이하로 낮춘다. 이 제한은 재접속을 포함한 풀 수명 전체에 유지된다.
// 서버 취소 유예는 두 Ping 예산과 별개이며 중단된 작업이 해당 기한까지 이어질 수 있다.
//
// # 트랜잭션 정리
//
// Rollback과 RollbackDeferred는 호출자 취소·기한과 분리된 5초 예산으로 rollback을 보낸다.
// 이미 종료된 트랜잭션은 성공으로 처리하고 다른 오류는 보존한다. RollbackDeferred는
// 호출자의 반환 오류와 정리 오류를 합치며, commit 결과 불명을 재시도로 바꾸지 않는다.
//
// shared-go에서 pgx에 의존하는 유일한 패키지다.
package pgxdb
