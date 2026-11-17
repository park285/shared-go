# shared-go 의존성·전환 비용·로그 안전성 리팩토링안

## 결론과 필수 전제

**보안을 완화하지 않고, 변경의 영향 범위와 검증 책임을 줄인다.** 권고 순서는 로그의 미지원 값 통과 차단과 공급자 중립 타입 정리 → 경량 유틸리티 분리 → 무거운 어댑터 분리 → 검증을 공유하는 인프라 사전 점검 → 중복 운영 코드 축소다. 인프라 사전 점검은 모듈 이동과 독립적으로 진행할 수 있다.

- 인증·인가·TLS·SSRF·credential/privacy 마스킹·DB 최소권한·멱등성·보존·결과 미상 처리는 유지하거나 강화한다.
- 검사 생략, 느슨한 모드, 원문 로그 병행, 자동 직접 DB fallback, timeout 무시, 예약 용량 자동 조정으로 전환을 쉽게 만들지 않는다.
- `invalid`·`unavailable`·`blocked`·`outcome_unknown`은 성공으로 바꾸지 않는다. 보안 동등성을 입증하지 못한 대체 구현은 도입하지 않는다.
- 아래 조사·설계는 구현 전 기준선이다. 사용자 구현 요청에 따라 로컬 소스 전환을 진행했으며 실제 결과는 마지막 「구현 진행」 절에 기록한다. 릴리스·원격 게시, 운영 전환, 기존 로그 데이터 이동·삭제는 실행하지 않았다. 게시·운영 승인은 보안 완화를 뜻하지 않는다.

## 현재 코드에서 확인한 사실

분석 기준은 현행 `go.mod`와 `CHANGELOG`의 v2.22.2다. 사용자가 제시한 번호형 인용의 원문은 제공되지 않았으므로 현재 저장소를 근거로 판단했다.

| 지적 | 판정과 설계상 의미 | 근거 |
|---|---|---|
| envutil을 쓰면 DB·HTTP/3 코드도 함께 빌드된다 | 수정 필요. Linux의 `go list -deps`에서 envutil/stringutil은 표준 라이브러리만 사용한다. 단일 모듈의 버전 선택·전체 그래프·릴리스 관리 결합은 남지만 이를 일괄 컴파일·링크·다운로드와 동일시하지 않는다 | `GOWORK=off GOPROXY=off go list -mod=readonly -deps …` 관측, [go.mod](../../../go.mod) |
| 현재 shared-go가 Gin도 소유한다 | 현재는 아님. v2.18.0에서 ginjson/Gin을 hololive 쪽으로 옮겼고 v2.20.0에서는 telemetry/OTel을 제거했다 | [CHANGELOG:45–60](../../../CHANGELOG.md#L45) |
| 빠른 버전 상승이 소비자 동시 이전을 만든다 | 일부는 확인. v2.17.0·v2.18.0은 실제 coordinated minor다. 다만 모든 minor/patch가 파괴적 변경은 아니며 버전 숫자만으로 비용을 계산할 수 없다 | [CHANGELOG:57–103](../../../CHANGELOG.md#L57), [호환성 정책](../../../README.md#L7) |
| 중립 LLM 타입도 공급자 SDK에 결합한다 | 확인. `llm.go`의 오류 상수 → `internal/openaidiag` → OpenAI SDK가 실제 import closure를 만든다. TwentyQ의 Vertex 구현도 `pkg/llm`을 사용한다 | [llm.go:7–13](../../../pkg/llm/llm.go#L7), [diagnostics.go:9–12](../../../pkg/llm/internal/openaidiag/diagnostics.go#L9), [Vertex 소비자](../../../../twentyq-bot/internal/common/llmrest/vertex_client.go) |
| 모든 계층이 일률적으로 fail-closed다 | 일반화 불가. pgxdb는 sslmode 명시를 요구하지만 운영 verify-full은 소비자 책임이다. 인증서 갱신 실패 시 기존 snapshot 유지도 명시된 계약이다 | [pgxdb/doc.go:3–20](../../../pkg/db/pgxdb/doc.go#L3), [certificate_reloader.go:34–35](../../../pkg/h3/certificate_reloader.go#L34) |
| struct 등의 로그 원문 통과 가능성이 있다 | 확인. 비민감 키 아래 미지원 KindAny와 깊이 초과 map은 통과한다. 민감한 최상위 키는 타입과 무관하게 가린다. 운영 유출 사고를 확인한 것은 아니다 | [sanitize.go:417–487](../../../pkg/logging/sanitize.go#L417), [깊이 제한:540–581](../../../pkg/logging/sanitize.go#L540), [깊이 9 보존 테스트:330–344](../../../pkg/logging/sanitize_privacy_test.go#L330) |

`iris-client-go`의 major는 별도 축이다. 현재 `/v5`는 Iris 10 인증 계약과 서버/클라이언트 동시 전환을 요구한다. shared-go를 나눈다고 이 프로토콜 전환이 없어지지는 않는다([SDK README:11–29](../../../../iris-client-go/README.md#L11)). SDK의 `go.mod`에는 shared-go 의존성도 없다.

## 1. 모듈 경계: 경량 출구를 먼저 만들고 어댑터만 분리

### 권고 구조

새 저장소를 만들지 않고 같은 shared-go 저장소에 하위 모듈을 둔다. 다음 경로는 제안이며 아직 생성하지 않았다. 한 번에 패키지별 모듈 수십 개로 쪼개지 않는다.

| 모듈 경로 | 소유할 코드 | 분리 이유·제약 |
|---|---|---|
| `github.com/park285/shared-go/core` | 현재 envutil·stringutil | 작은 도구가 루트 모듈 없이 사용할 경로. 처음에는 이 두 패키지만 옮긴다. 기존 의미·보안 파일 열기·플랫폼별 거부를 그대로 보존한다 |
| `github.com/park285/shared-go/v2` | logging·netguard·runtime·workercontract·workerpool·irisdurable 계약·dbmigrate·공급자 중립 llm·prompt/output guard·나머지 공통 패키지 | 기존 범용/스택 계약의 소유자. 새로운 어댑터 모듈에 역의존하지 않는다. 잔여 YAML·문자 정규화·rotation 의존성은 인정한다 |
| `github.com/park285/shared-go/postgres` | pgxdb·irisdurable/pgstore 구현 | pgx·SCRAM·pool·PostgreSQL 구현의 버전과 DB 검증 분리. 추상 durable 계약과 서비스 DDL은 옮기지 않는다 |
| `github.com/park285/shared-go/transport` | h3·healthprobe | QUIC 업데이트를 독립시킨다. healthprobe가 h3를 import하므로 같이 옮긴다. stdlib httputil·runtime/httpserver까지 무조건 옮기지 않는다 |
| `github.com/park285/shared-go/openai` | openaipreset·openaicfg/openaidiag 구현 | OpenAI SDK 릴리스와 중립 Message/Usage 타입의 분리. provider-free llm은 루트에 남긴다 |

의존 방향은 `postgres/transport/openai → shared-go/v2`다. core는 루트나 어댑터를 요구하지 않고, 소비자가 필요한 모듈만 선택한다. 루트가 새 어댑터를 다시 require/import하면 분리 효과가 사라지므로 생산 코드뿐 아니라 테스트 의존성도 확인한다.

**경계별 필수 작업**

- `llm`은 기존 공개 오류 값의 단일 정의를 소유하고 SDK 진단 코드가 이를 참조하도록 방향을 뒤집는다. 오류를 양쪽에서 별도로 만들어 `errors.Is`를 깨뜨리지 않는다. refusal 오류가 empty/refusal 두 sentinel에 모두 일치하고 ChatBotGo의 breaker 제외 분류도 유지되는지 검증한다. 단순 이름 변경은 하지 않는다. `openaidiag`만 이동하고 llm의 import를 남기는 순환 의존은 허용하지 않는다.
- `core`의 생산 코드는 현재처럼 stdlib-only를 목표로 한다. 기존 envutil 테스트에는 testify가 있으므로 **시험 의존성까지 0개라고 주장하지 않는다**. 무거운 pgx/QUIC/OpenAI 또는 루트 모듈이 시험 경유로 다시 들어오지 않아야 한다.
- `pkg/internal/testsupport`는 새 import prefix에서 접근할 수 없다. 필요한 작은 fixture를 각 모듈의 테스트 소유로 옮기거나 표준 testing 기능으로 대체한다. 이를 해결하려고 루트 의존성이나 공개 testkit 모듈을 추가하지 않는다.
- promptguard·outputguard·`pkg/internal/guardtext`는 당장 함께 루트에 남긴다. 공통 decoder를 복제하거나 양쪽에 별도 버전을 두지 않는다. logging도 obsmetrics·bootstrap과 연결되어 있으므로 이번 분할의 별도 모듈로 만들지 않는다.
- envutil의 Unix no-follow/nonblocking open과 비Unix의 unsupported 거부를 보존한다. 범용 파일 열기로 교체하지 않는다.
- 이전 시 기존 패키지는 삭제하고 모든 생산/테스트 import를 전환한다. forwarding package·type alias·re-export·영구 replace로 구경로를 유지하지 않는다.

### 버전 동기화 비용을 줄이는 정책

1. 신규 모듈은 최초 안정 버전부터 모듈별 태그(`core/v1.0.0` 등)를 사용한다. 루트는 기존 `v2.*` 태그를 유지하며 숫자를 서로 맞추지 않는다.
2. 최초 경로 이전은 승인된 소비자 동시 전환 한 묶음으로 끝낸다. 현재 root의 coordinated-minor 관행을 신규 모듈의 상시 파괴 정책으로 복제하지 않는다. 이후에는 호환 추가·수정 우선, 사용 중 API의 파괴는 명시적인 major 전환으로 관리한다. 이는 현행 정책 변경 제안이며 승인 없이 적용하지 않는다.
3. 무관한 비보안 릴리스 때문에 모든 봇의 pin을 일괄 최신화하지 않는다. 변경된 패키지·모듈과 직접/전이 소비자를 기준으로 갱신한다. 여러 봇이 같은 보안 계약을 쓰면 동시 검증 비용은 제거 대상이 아니다.
4. 보안 수정은 관련 소비자 전체에 반영한다. 모듈 분할을 이유로 취약한 pin을 방치하거나 기존 취약점 검사를 줄이지 않는다. 도달성 미확인을 안전으로 간주하지 않는다.
5. 단순 export 정리·이름 취향·표준 함수 wrapper 때문에 소비자 이전을 만들지 않는다. 소비자가 없는 API 정리는 현재 계약 안에서 기능 변경과 구분한다.

### CI·게시까지 포함한 전환

현재 `release.yml`과 `prepare-release-bundle.sh`는 루트 `vX.Y.Z` 태그만 처리한다. 하위 모듈을 만드는 것만으로는 릴리스가 완성되지 않는다.

- 기존 CI의 PR/main 트리거·race·lint·tidy·주간 보안 검사를 유지하며 모듈별 작업 디렉터리를 명시한다. Go의 `./...`는 하위 모듈을 모두 포함하지 않는다.
- 기존 release 흐름에 모듈/태그 조합의 명시적 허용 목록을 추가한다. 태그 문자열을 임의 shell 경로나 산출물 이름으로 쓰지 않고 모듈명·버전·source ref를 구분한다.
- 각 모듈의 `go.mod`·`go.sum`, LICENSE/NOTICE 포함 여부, SBOM 범위, 정확한 tag/commit·서명 주체·attestation·immutable release 검증을 함께 전환한다. 아티팩트 검사와 게시 스크립트도 같은 규칙을 사용한다.
- SQL/실제 PostgreSQL 검증은 DB 소유 모듈, Unicode 생성·guard 검증은 루트에 남기되 기존 검증 범위를 누락하지 않는다. 기존 직접 스크립트를 수정하며 별도 gate 프레임워크·결과 캐시·문서/소스 텍스트 검사를 만들지 않는다.
- 로컬 workspace 검사 다음에는 소비자별 실제 pin과 `GOWORK=off`로 검증한다. 임시 sibling replace가 필요한 기존 Hololive 구성과 게시 의존성을 구분하며, 개발용 새 replace가 릴리스에 남지 않게 한다.
- 최초 게시 순서는 root의 provider-free 계약 준비 → 이를 요구하는 어댑터 게시 → 소비자 pin 이전이다. 구패키지 삭제 시점과 새 어댑터의 최소 root 버전을 하나의 전환 묶음으로 설계한다. 원격 게시 전에는 소스 통합 검증일 뿐 게시 성공을 주장하지 않는다.

## 2. 로그: 정적 타입만으로 해결하지 말고 sink에서 원문 통과를 닫기

### 선택할 설계

**기존 `...slog.Attr` API + 서비스의 명시적 필드 projection + 단일 sanitizer의 지원 타입 제한**을 사용한다. 새 Logger façade, reflection 전체 순회, JSON 재직렬화, Secret 타입 도입만으로 문제를 해결하지 않는다.

1. 민감 키/group은 `LogValuer.Resolve`보다 먼저 마스킹한다. 어차피 버릴 값의 사용자 메서드를 실행하지 않는다.
2. 일반 값은 native slog scalar·nil·group 및 명시된 error 진단만 지원한다. LogValuer 결과도 동일 규칙으로 다시 검사한다. Go 구조체 전체, pointer, 임의 map/slice, named aggregate, custom marshaler가 formatter로 넘어가지 않게 한다.
3. 현재 map 재귀 경로도 최종적으로 없애고 서비스가 필요한 필드만 group으로 투영한다. 임시 원문 통과 옵션은 두지 않는다. 기존 민감 키를 무해한 이름으로 바꾸거나 JSON 문자열로 평탄화해 마스킹을 우회하는 전환은 금지한다.
4. 미지원 값·해소 실패·깊이 초과는 원문 대신 고정된 차단 표시와 구별 가능한 사유로 남긴다. Handler 오류 반환만으로 보고하지 않는다(slog 호출자는 해당 오류를 받지 못할 수 있다). 원문 `Error()`/`String()`/marshal 호출을 대체 경로로 쓰지 않는다.
5. group에도 유한 깊이를 적용한다. 현재 map 한도 8을 출발점으로 실제 소비자의 필요한 깊이와 정확한 계산 기준을 정하고, 초과 시 값 전체를 차단한다. 한도를 넘긴 나머지 원본을 남기지 않는다. 신뢰된 LogValuer의 임의 실행을 샌드박싱한다는 보장은 하지 않는다.
6. 기존 credential 규칙·서비스 PrivacyKeys·message/string/error 정제·상관관계 ID 계약은 유지한다. 직접 error 진단은 기존 패턴 기반 보호의 한계를 명시하고 서비스에서 안정적인 code/type와 승인된 설명을 우선한다. 무표식 비밀을 포함한 임의 문자열까지 완전히 탐지한다는 보장은 하지 않는다.
7. `sanitizer → JSON → 동기 file / 정제된 bytes의 async stdout` 순서를 유지한다. 큐에 원본 객체를 넣거나 stdout만 가리고 파일은 원문을 저장하는 구성은 금지한다.

### 소비자 이전과 검증

- LSP 참조로 logger factory/default 주입·Any·WithAttrs·WithGroup·error 사용을 확인한다. 메시지·event·attr/group 이름은 정적인 템플릿을 사용하며 실제 secret/log 원문을 수집하지 않는다.
- 먼저 각 소비자의 aggregate 기록을 최소 필드·개수·식별자의 typed group으로 바꾼다. 기존 PrivacyKeys와 마스킹 의미를 그대로 보존한다. 그 뒤 단일 강화 정책으로 전환한다.
- 현재 깊이 9 값과 일부 aggregate 보존을 기대하는 테스트는 변경되는 계약에 맞춰 갱신한다. 문자열 문구·구현 모양을 다시 고정하는 테스트로 바꾸지 않는다.
- synthetic sentinel로 struct/pointer/map/slice/named type, 중첩 조합, LogValuer의 scalar/group/Any·실패, marshaler의 정상/오류/panic, 깊이 경계·순환을 검사한다. 실제 JSON/file/stdout 어디에도 sentinel이 없어야 한다. 기존 가려진 값도 계속 없어야 한다.
- WithAttrs/WithGroup, 정상/축약/포화 stdout, sink 오류, caller-owned 자료 불변성을 포함한다. 정상 record와 성공적으로 기록한 파일 lane에서는 차단 사유·request/error 식별자를 보존한다. 축약 stdout은 기존 7개 필드 보존 계약만 따르며 임의 error/차단 필드 보존을 약속하지 않는다. 포화·sink 오류로 stdout 사본이 사라지는 경우에는 기존 유실/축약 counter와 파일 기록을 확인하며, 파일 쓰기 실패도 별도로 관측한다.
- 기존 clean sanitizer의 0-allocation 계약을 유지한다. 변경 경로만 기존 benchmark로 동일 장비·toolchain에서 비교한다. 미지원 payload 전체를 탐색하거나 복사하지 않는다. 임의 wall-clock 합격선은 만들지 않는다.

**한계:** Go는 문자열의 비밀 여부나 외부 logger·직접 stderr 출력을 타입만으로 증명하지 못한다. 이 설계의 보장은 ‘구성된 sink가 미지원 객체를 그대로 직렬화하지 않는다’와 ‘기존 마스킹을 유지한다’이지 모든 비밀의 완전 탐지가 아니다.

## 3. Fail-closed 유지: 거절을 늦게 발견하는 비용을 줄이기

### 검증 소유권과 사전 점검

- pgxdb의 기존 config/DSN 준비·pool overlay·옵션·예약 검증을 부작용 없는 내부 준비 함수로 모은다. 실제 OpenPool 경로와 소비자의 무접속 preflight가 같은 함수를 사용하도록 필요한 최소 검증 API만 제공한다. Config.Validate만 재호출하고 전체 검증을 했다고 표시하지 않는다.
- structured 경로의 필수 양수 값과 DSN overlay의 `0=미지정`을 유지한다. 오류는 필드·제약을 식별할 수 있게 하되 DSN·비밀번호를 출력하지 않는다. 연결·TLS·인증·acquire·SQL의 오류와 SQLSTATE/context 원인은 보존한다.
- 서비스는 env/profile을 지역 값에 decode → 전체 관계 검증 → DB 옵션 검증 → 자원 생성 순서로 조립한다. 중간 decode가 실패한 destination은 전부 폐기하며 listener/worker를 먼저 시작하지 않는다.
- shared-go는 `cmd/`를 만들지 않는다. 기존 서비스의 profile/config 검사 진입점에 같은 검증을 연결한다. **무접속 검사는 CA 파일·설정 등의 로컬 조건만 확인하며 원격 TLS/권한/프로토콜 호환성을 증명하지 않는다.**

| 문제 | 안전한 개선 | 하지 않을 것 |
|---|---|---|
| SNAT/HBA 불일치 | 인프라 소유의 실제 출발지·경로·HBA·양 구간 TLS 검증을 승인된 전환 절차에 포함 | shared-go의 사설망 차단 해제, 주소 자동 추정·라우팅 변경 |
| `statement_timeout` / 08P01 | RuntimeParams가 startup packet에 들어감을 명시. 정확한 pooler 버전/기능의 추적 지원과 timeout의 실제 적용을 격리 환경에서 검증 | 08P01을 단일 원인으로 오분류, parameter 삭제/무시, `AfterConnect SET`으로 세션 보장 대체 |
| 제어 연결 예약 충돌 | 유효 `MaxConns`·reserve·일반 용량을 함께 사전 검사. frontend pool과 backend/user/database/role cap·replica 합계를 구분 | reserve 자동 절삭, pool 자동 증설, 전체 worker에 control 표식 부착 |
| transaction pooling | prepared statement·backend 교체·cancel·timeout 적용/격리의 실제 제품 검증 | simple protocol 일괄 강제, pooler 장애 시 직접 PG 우회 |
| migration | 기존 직접 PG maintenance 연결과 session advisory lock·timeout·불확실 연결 폐기 유지 | transaction-pool frontend를 고정한 것만으로 backend 세션도 고정됐다고 가정 |

시작 ping, 일반 gate+acquire, pooler backend 대기, SQL 실행, CancelRequest 유예, rollback 정리는 서로 다른 예산이다. control 예약은 로컬 일반 획득만 제한하며 물리 연결 상한이나 pooler backend를 보장하지 않는다. Acquire 성공 뒤 SQL이 기다릴 수 있고, 취소 성공도 COMMIT 결과 확정을 뜻하지 않는다.

**완료 조건:** 동일한 로컬 입력·파일·환경 snapshot에서 preflight와 OpenPool이 공유하는 **준비 단계의 설정 수용·거절**이 일치하고 invalid 설정에서는 dial이 0회다. 유효한 설정에서도 OpenPool의 연결·TLS·인증·Ping은 실패할 수 있으므로 별도 결과로 보존한다. 원격 CA/SAN 오류는 TLS 연결 성립 전에 거절한다. 일반 부하 포화 중 제어 작업, 부모 deadline, rows/transaction 수명, SQLSTATE·outcome_unknown, migration unlock/eviction을 실제 DB·승인된 격리 pooler로 확인한다. 기존 통합 검사가 skip되면 해당 수용 조건은 미검증이다.

사용자가 보고한 SNAT·08P01·예약 충돌은 경험 사실로 수용한다. 당시 설정이나 운영 상태를 다시 조사·재현하지 않았으므로 전부 shared-go 결함이라고 단정하지 않는다. 별도 PgBouncer 작업 트리의 준비 문서를 운영 전환 완료로 간주하지도 않는다.

## 4. 자체 구현: 없앨 것과 유지할 보안 경계를 구분

| 구성요소 | 결정 | 유지/축소 경계와 완료 조건 |
|---|---|---|
| 인증서 reloader | 작은 adapter로 유지 | crypto/tls 파싱·불변 snapshot·호출자 join을 유지. 발급/배포 세대/갱신 주기/만료 readiness는 서비스 소유. X509KeyPair는 키 일치 검사이지 SAN/체인/유효 수명 전체 검증이 아니다. 초기 invalid, 갱신 중 불일치, 실패·복구, 동시 handshake, 만료 경계를 검증한다. 기존 snapshot 유지 계약을 새 무기한 stale 허용 정책으로 확대하지 않는다 |
| SSRF dialer | 공통 enforcement 유지 | net/http에는 해당 정책이 없다. netguard의 guarded 경로에서 DNS의 차단 주소 혼합 거부·검증 IP literal dial·proxy 차단·TLS hostname·설정된 redirect credential 차단을 보존한다. 허용 host/prefix는 서비스가 소유하며 기존 opaque transport 예외를 확대하지 않는다. h3의 선택적 DialGuard와 내부 주소를 허용하는 healthprobe를 같은 보장으로 간주하지 않는다 |
| prompt/output guard | 구현·판정 계약 유지, 서비스 정책 분리 | bounded decode/정규화·보호 index·미완료 사유를 유지. 보호문·출력 schema·업무 enforcement는 서비스 소유. 새 보안 엔진으로 대체하지 않는다. 동일 corpus와 실제 send/store 차단을 비교하며 이름만 boolean ‘safe’로 통합하지 않는다 |
| 로그 archive 이동·추가 pruning | 중복 축소의 우선 후보, 무조건 삭제는 아님 | rotation/gzip은 이미 lumberjack이 소유한다. archive 레이아웃이 불필요하고 기존 데이터까지 권한·보존·수집·실패 계측을 동등 이상 인수할 때만 이동/이중 pruning을 없앤다. 그렇지 않으면 현행 유지. 새 라이브러리를 기본 대안으로 추가하지 않는다 |

로그 아카이버 변경은 sanitizer 강화와 별도 변경으로 분리한다. `archive/`의 기존 파일, 디렉터리 0750/파일 0640, age/count 보존, 수집기 경로, stdout 장애와 파일 lane 격리를 먼저 확인한다. 경로의 symlink·비정규 파일 거부와 원본이 남은 미완성 gzip의 비이동도 인수 조건이다. archive 실패 진단과 파일 write/short-write counter를 구분하며, 이동·pruning을 없앤다는 이유로 `permissions`·`AwareWriter`까지 일괄 삭제하지 않는다. 실제 rotate→gzip 해제 가능한 백업과 실패 경로를 검증한다. 현재 `Close`가 lumberjack 압축 완료·내구 저장을 보장하지 않는 한계도 숨기지 않는다. 인수 계획 없이 기존 archive를 지우지 않는다.

### 소비자 보장 불일치 후보

ChatBotGo의 `ValidateDecodedStructuredText`는 `decode_incomplete`만 있고 rule hit가 없으면 허용한다([generation_protection.go:75–103](../../../../chat-bot-go-kakao/internal/prompt/generation_protection.go#L75)). reply/storage/artifact 검증이 이 함수를 사용한다([output_validation.go](../../../../chat-bot-go-kakao/internal/bot/answer/output_validation.go)). 독립 검토에서는 JSON decode·필수값·크기·UTF-8 등 선행 형식 검사를 확인했지만, 이것이 최종 표시 문자열의 보안 decode 완료를 증명하지는 않는다. 주석의 strict schema·필드별 선행 보장 전체는 이번 조사에서 입증하지 않았다.

이는 **확정된 운영 유출 사건이 아니라 확인할 보장 불일치**다. 모듈 이동 전에 선행 경로가 요구한 검사를 실제 완료하는지 반증 검토한다. 완료를 입증하지 못하면 incomplete를 거부하는 소비자 변경을 별도 승인 범위에 포함한다. 해당 예외를 공통 guard의 새 허용 정책으로 승격하거나 ‘기존 호환’이라는 이유만으로 복제하지 않는다. promptguard의 기존 observe/interactive/persistent 계약과 outputguard의 차단 계약은 서로 구분한다.

## 5. 실행 순서·소유자·전환 단위

아래는 앞으로 할 작업이다. 계획 작성 중 실행한 것으로 해석하지 않는다.

| 순서 | 변경과 소유자 | 선행 조건·수용 기준 |
|---|---|---|
| 1 | shared-go + 각 소비자: 보안 기준선·logger projection·provider-free llm | 로그 보존/차단 계약 승인. 기존 마스킹 유지와 추가 차단 증거. llm import closure에서 OpenAI SDK 제거, 오류 정체성과 응답 상한·부분 출력 거부 유지 |
| 2 | shared-go: 모듈별 release/CI 지원, core 추출. 소비자: import 전환 | 새 경로/호환 정책 승인. envutil·stringutil 실제 소비자와 테스트 이전. core 경유 pgx/QUIC/OpenAI/루트 역의존 없음. 플랫폼별 secret-file 거부 보존 |
| 3 | shared-go: postgres·transport·openai 순차 추출. 소비자별 pin 전환 | 루트→어댑터 순환 없음, internal/test 경계 정리, 각 모듈 GOWORK=off 검사와 게시 검증. DB·H3·OpenAI 소비자 회귀. 분할과 런타임 정책 변경을 한 변경에 섞지 않음 |
| 병행 | shared-go + 서비스 + 인프라: 동일 검증 기반 preflight | 기존 옵션 의미·최소권한·TLS·timeout 보존. 순수 설정 오류와 실제 endpoint 실패를 구분. PgBouncer 가동은 별도 승인 |
| 4 | logging 소유자 + 로그 운영 소유자: archive 중복 제거 여부 결정 | 권한·보존·기존 파일·수집 경로·실패 관측의 인수 증거가 없으면 삭제하지 않음. 다른 세 축의 개선을 이 결정에 묶지 않음 |

**소비자 범위:** ChatBotGo, TwentyQ, iris-console, Hololive의 API·alarm-worker·youtube-collector·hololive-shared·hololive-dbtest까지 8개 모듈에서 shared-go import를 확인했다. 테스트/fixture import도 포함된다. iris-client-go는 별도 프로토콜 계약만 대조했으며 shared-go 소비 모듈로 세지 않았다. Console의 h3 사용은 이번 검색에서 테스트/fixture였으므로 이를 운영 H3 의존으로 과장하지 않는다.

- ChatBotGo: core·DB·H3/healthprobe·OpenAI 전환, 로그 projection, 출력 미완료 선행 검증.
- TwentyQ: core·DB·H3/healthprobe·중립 llm·로그/guard. 현행 Vertex 경로에 OpenAI adapter를 새로 추가하지 않는다.
- Hololive 5개 모듈: 각 실제 import와 내부 sibling replace를 따라 이전. migration harness인 dbtest도 빠뜨리지 않는다.
- iris-console: stringutil·테스트 H3 이전. 별도 GOWORK=off 검증. Android 변경은 범위 밖이다.

**복구 원칙:** 배포된 바이너리·profile·endpoint·역할 한도·인증서의 호환 조합을 유지한다. DB 변경 없는 코드 추출과 DB 정책 전환을 분리한다. 문제가 생기면 보호 동작을 끄지 말고 활성화를 중단한다. 강화 전 취약 로그 정책으로 자동 rollback하지 않으며 보안 기준을 충족하는 버전만 복구 후보로 둔다. 로그 파일 이동·삭제, DB/인프라 롤백은 별도 데이터 보존·복구 승인 범위다.

## 6. 검증 계획과 완료 판정

| 변경 표면 | 구현 후 수행할 최소 검증 | 보안 비완화 증거 |
|---|---|---|
| 모듈/릴리스 | 모듈별 tidy diff·build/lint/test/race/vulncheck, workspace와 실제 pin의 GOWORK=off 소비자 검사, 기존 release gate의 모듈별 적용 | 기존 CI 트리거·attestation·취약점 검사·DB 실제 검증 누락 없음. 깨끗한 임시 소비 모듈에서 새 경로 해석·예상 외 의존 확인 |
| logging | 기존 logging 제품 테스트·race·할당 상한 및 변경 경로 benchmark, 실제 임시 file/stdout 출력 | 기존/신규 synthetic secret 모두 부재, 원문 fallback 없음, 오류·차단·유실 구별 |
| pgxdb/dbmigrate | 옵션 검증·예약 경합·취소·rows/tx 수명·session lock/eviction 기존 테스트, 실제 PG 및 승인된 pooler 시나리오 | 로컬 invalid 설정이면 dial/DDL 없음. 실제 연결·TLS·인증 실패는 별도 거절. 최소권한/timeout·제어 작업 진행·unknown 보존 |
| envutil/core | 기존 secret-file·dotenv·입력 오류 테스트 및 지원 플랫폼 검사 | KEY/KEY_FILE 충돌·symlink/FIFO·unsupported 플랫폼 거부가 동일 |
| h3/netguard | 인증서 갱신 race·TLS handshake·실제 local dial/redirect fixture를 각 정책별로 검사 | netguard guarded 경로의 mixed DNS/IP·proxy·credential 차단 보존. h3의 실제 dial/TLS와 내부 healthprobe의 제한된 신뢰 경계를 별도로 확인 |
| guards | 기존 정상/적대 corpus·경계/할당 검사, 변경 decoder에 필요한 bounded fuzz, 소비자 발송/저장 경로 | outputguard의 미완료 차단과 promptguard의 기존 enforcement별 거절 보존. 허용 경로 확대 없음. 새 정상 입력 지원은 검사 완료 증거 필요 |
| archive | 실제 rotation/gzip·권한·보존·수집·오류/close 시나리오 | 기존 파일 유실·권한 확대·마스킹 우회 없음. 압축 완료/내구 저장을 과장하지 않음 |

검증은 바뀐 표면에 맞춰 기존 명령을 선택한다. 모든 작은 변경마다 전체 스택을 반복 실행하지 않는다. 반대로 모듈 경로·릴리스 경계가 바뀌는 변경은 해당 소비자와 release-sensitive 검증까지 필요하다. 새 source grep gate·문서 동기화 gate·체커 자가테스트는 추가하지 않는다.

**효과 판정:** core 소비자는 무거운 어댑터 없이 해석·빌드 가능해야 한다. OpenAI-only 변경은 무관한 소비자의 필수 pin 변경을 만들지 않아야 한다. 사전 점검에서 설정 오류를 자원 생성 전에 설명해야 한다. 로그/네트워크/DB/guard의 기존 차단 사례는 계속 거절돼야 한다. 감소율이나 일정은 아직 측정하지 않았으므로 약속하지 않는다.

## 이번에 수행한 조사와 남은 한계

- 코드·README·변경 이력·소비자 import·기존 테스트 내용을 정적으로 확인했다. 1차 3개 영역 분석과 2차 독립 반증 검토, 총 2개 wave·4개 에이전트를 사용했다. 독립 검토의 preflight/OpenPool 결과 혼동을 수정했고, archive 보안 인수·stdout 관측 범위·LLM sentinel·H3 정책 구분도 반영했다.
- `GOWORK=off GOPROXY=off go list -mod=readonly -deps`로 envutil/stringutil의 현재 Linux import closure를 확인했다. `go list -m all`과 35개 패키지의 `go list -json ./pkg/...`로 모듈 그래프와 내부 의존 방향을 확인했다. 다운로드 양·바이너리 크기·실행 성능은 측정하지 않았다.
- 부모 LSP에서 llm의 `ErrOpenAIEmptyOutput`과 `Usage` 참조를 확인했다. 자식 환경에는 LSP가 없어 일부 경계는 소스 추적 근거이며, 모든 소비자의 logger/출력 경로 참조를 완전히 열거한 것은 아니다.
- 최초 `GOTOOLCHAIN=local` 조회는 설치 기본 Go 1.27.3과 모듈 요구 1.27.4 불일치로 실패했다. 이후 기존 캐시의 자동 선택 Go 1.27.4로 위 메타데이터 조회를 완료했다. toolchain 업그레이드나 모듈 변경은 하지 않았다.
- 테스트·빌드·lint·race·benchmark·실제 DB/네트워크/운영 재현은 실행하지 않았다. 위 검증 표는 후속 구현의 완료 조건이지 통과 기록이 아니다.
- 운영 로그/비밀·배포 상태를 조회하지 않았다. 실제 로그 유출, 인증서 만료 정책, archive 수집 의존, 당시 PgBouncer 기능/설정, 출력 미완료 예외의 선행 보장 전체는 미확인이다. 계획 완료와 이 운영 사실들의 검증 완료를 구분한다.

## 구현 진행

- 실행: `ultracode` 1차 wave 7개 독립 구현 작업, 부모 통합·검증. Go 1.27.4 현대화 및 Go 보안 스킬을 적용했다. 별도 작업 트리 `refactor-shared-go-v2.23.0`과 기존 Hololive `go.work.sum` 변경은 건드리지 않았다.
- 모듈: `core/{envutil,stringutil}`, `postgres/{pgxdb,pgstore}`, `transport/{h3,healthprobe}`, `openai/openaipreset`으로 이동했다. 루트는 SDK·pgx·QUIC 어댑터를 요구하지 않는다. 신규 모듈별 LICENSE·고지 및 기존 의존성 pin을 유지했고 구경로 forwarding·alias는 없다.
- 소비자: ChatBotGo, TwentyQ, Console, Hololive 5개 모듈의 생산·테스트 import와 선언된 pin을 전환했다. 게시 준비 중 기존 `v2.23.0` immutable release를 확인하여 사용자 추가 승인으로 root `v2.24.0`, 신규 모듈 `v1.0.0`을 대상으로 바꿨다. go.sum에 미게시 모듈의 checksum을 만들어 넣지 않는다.
- 로그: 지원 타입 제한·민감 값 선차단·8단계 group 상한·해소/타입/깊이 차단 사유와 소비자 typed projection을 구현했다. archive 이동·추가 pruning은 인수 증거가 없어 유지했다.
- DB: preflight와 실제 pool 생성은 동일 준비 함수를 사용하며 서비스의 기존 config/profile 검사 및 자원 생성 전 경로에 연결했다. 순수 설정 수용과 실제 연결·TLS·인증·SQL 결과를 구분한다.
- 출력: ChatBotGo의 decode_incomplete-only 허용을 제거하고 실제 발송·outbox·session 거부 사례를 추가했다. 검증 중 Responses 거절이 empty 오류로만 축소되는 경로가 확인돼 terminal refusal과 부분 delta를 분리하는 수정을 적용했다.
- CI/릴리스: 모듈별 source tidy·lint·race·취약점·실제 PG 검사와 태그/SBOM/attestation 범위를 전환했다. 최초 `go.work use`만으로는 미게시 모듈 메타데이터를 조회해 실패했으며, 검증용 workspace에 정확한 버전별 로컬 replace를 적용한 뒤 소스 검사가 진행됐다. 릴리스 `GOWORK=off` 실제 pin 검증과 별개다.
- 최신 main `14c7079`의 미사용 OpenAI 클라이언트·옵션·스트림·내부 진단과 `UsageReporter`·`NoopUsageReporter`·`ErrNilContext` 제거를 새 모듈에도 보존했다. 새 실제 소비자인 ChatBotGo의 refusal sentinel은 유지하며 openai 모듈은 사용 중인 응답 변환 도우미만 제공한다. 통합 후 llm·openai 전체 race, ChatBotGo refusal 회귀, root/openai lint·source tidy가 통과했다. 최종 API를 사용한 8개 소비 모듈의 소스 build도 통과했다(Console은 기존 임시 modfile의 로컬 replace 조건이며 실제 게시 pin 증거가 아니다).
- 추가 승인 범위는 shared-go PR/main 통합·루트 `v2.24.0` 및 네 하위 모듈 `v1.0.0` 게시, 소비자 로컬 실제 pin 검증, 합성 데이터·loopback 전용 일회용 pooler 검증이다. 소비자 Git 게시·운영 활성화·운영 비밀/로그 접근은 제외한다.
- 선택 커밋의 286개 경로를 staging-scope-guard가 차단했다. 사용자는 `IRIS_STACK_ALLOW_BULK_STAGE=1` 일회 예외를 승인하지 않고 커밋·게시 보류를 선택했다. guard·다른 hooks를 우회하거나 커밋을 쪼개 우회하지 않았다. 이후 공유 라이브러리 게시 및 이에 의존하는 실제 pin 검증은 보류한다.

### 검증 결과와 납품 상태

로컬 소스 구현·소비자 이관과 승인된 격리 pooler 검증은 완료했다. 커밋·새 버전 게시·실제 pin·서명 검증은 사용자 선택에 따라 보류했다.

| 표면 | 실제 실행 결과 |
|---|---|
| 루트·core | `release-gate.sh . core` 통과: tidy·Unicode·할당 상한·SQL ownership·lint/build·전체 race·vulncheck |
| postgres | 실제 일회용 PostgreSQL의 pgxdb·pgstore race 게이트, 필수 사례 pass·전체 skip=0. 최종 준비 함수 정리 후 pgxdb 전체 race와 Preflight/Open 옵션 parity도 skip=0 통과 |
| transport·openai | 전체 제품 테스트·race와 lint/build·govulncheck 통과 |
| 모듈 경계 | 5개 모듈 source tidy 통과. 루트 생산/시험 closure에 어댑터 없음. llm은 SDK 없이 해석. 별도 임시 core 소비 실행은 root·pgx·QUIC·OpenAI 없이 성공. Windows envutil 시험 바이너리 교차 컴파일 성공(Windows 실행은 하지 않음) |
| logging | 실제 JSON·file·stdout, 포화/EPIPE/파일 오류·깊이·LogValuer·입력 불변성 제품 테스트와 race 통과. clean 0 B/op·0 allocs/op 유지 |
| ChatBotGo | 전체 production/cmd/internal 패키지 race 통과. 출력 미완료의 발송·저장 거부, terminal refusal의 이중 sentinel·breaker 제외·부분 delta 거부, nil config·자원 생성 전 거절 회귀 통과. lint/build 통과 |
| TwentyQ | 전체 race·lint/build 통과. 실제 `--check-worker-profile` 바이너리가 존재하지 않는 endpoint의 유효 로컬 설정은 수용하고, 누락 CA는 runtime/log 디렉터리 생성 없이 거부 |
| Hololive 5모듈 | 전체 build와 제품 테스트 실행. 변경된 DB/profile/logging 경로의 race와 전체 모듈 lint 통과. audit evidence 배열 기대를 count group 계약으로 갱신. API의 생산 dependency 검사도 임시 modfile의 `GOWORK=off` 소스 조건에서 통과 |
| Console | 임시 modfile의 `GOWORK=off`로 전체 build·vet·race·tidy 통과. 실제 게시 pin 검증과는 구분 |
| 스크립트·공급망 | 기존 Pair Python 10개 사례와 native 배포/복구 fixture, Bash/Python syntax, stack DB volume 계약, shared-go/Hololive workflow 보안 정책 통과. 릴리스 태그→모듈/산출물 매핑과 경로 탈출 태그 거부 실행 확인 |
| 격리 pooler | PostgreSQL 18.7·PgBouncer 1.26.1-guc1 amd64, rootless `network=none`/loopback에서 실제 새 postgres 모듈을 실행. verify-full·상호 TLS, CA/SAN 거부, startup GUC 격리, backend 교체와 prepared statement, statement/lock timeout, CancelRequest `57014`, reserved renew/complete·drain, 장애 시 `outcome_unknown`·직접 DB fallback 없음 통과 |

### 적대적 리뷰 후속 수정

사용자의 이슈 해결 요청에 따라 정적 리뷰의 P1 2건·P2 4건을 수정했다. 커밋·게시·운영 변경 보류는 유지한다.

| 이슈 | 적용한 수정 |
|---|---|
| F1: named volume 거부 | 설치된 `.volume` 선언과 native volume metadata로 기존 저장 경로를 확인해 읽기 전용 bind로 전달한다. 자동 생성·copy-up 없이 idmap/UserNS를 보존한다 |
| F2: 선택 변수 생략 거부 | 미정의 `${VAR}`는 빈 문자열, `$$`는 리터럴 달러로 처리하며 치환 결과를 재확장하지 않는다. 필수값은 기존 설정 소유자가 검증한다 |
| F3: 휘발성 env 의존 | 기존 prestart에 공유 `--render-env` 모드를 추가했다. 현재 Service 환경에서 검사별 0700/0600 출력을 만들고 정리하며 운영 `/run`을 읽거나 덮어쓰지 않는다. retained API 버전과 환경 우선순위를 보존한다 |
| F4: CI checkout 불일치 | Hololive 보안 workflow의 shared-go ref를 목표 `v2.24.0`에 맞추고 소비자 README의 이전 목표 버전 표기도 정정했다. 태그 게시 전제와 원격 CI 미실행을 구분한다 |
| F5: 개발 이미지 소스 누락 | ChatBotGo workspace stage에 네 하위 모듈의 manifests·source·local replace를 연결했다. 운영 `bot`의 게시 pin 정책은 변경하지 않았다 |
| F6: handoff 상관관계 소실 | count와 내부 원장 식별자 `delivery_ids.first_id`를 함께 기록한다. Iris request ID나 raw aggregate는 복원하지 않았다 |

후속 검증은 부모가 실행했다.

- `bash -n`과 `admin-bind_test.sh` 통과. 정상/preview 환경 일치, absent/stale runtime, 필수값·문법·여러 줄 값 거부, retained version, 0700/0600·비덮어쓰기·운영 부작용 경계를 검사했다.
- 고정 Python 3.15.2 runner의 `worker-profile-pair_test.py` 22개 통과. 실제 c/b unit과 native Quadlet 생성도 포함한다. 첫 실행의 API 합성 fixture에 필수 `HOLOLIVE_H3_SERVER_NAME`이 빠져 4개가 실패했으며, fixture를 보완한 뒤 전체 22개가 통과했다. Podman inspect/run 경계는 합성 fixture이므로 실제 rootful mount·idmap 수용 증거는 아니다.
- `TestYouTubeHandoffLogsRetainDeliveryIdentity`와 기존 `TestYouTubeHandoffReconciler*`를 실제 격리 PostgreSQL로 `-race -count=1` 실행해 skip=0 통과했다. 조회 지연·quarantine의 실제 JSON 로그와 DB 상태, 내부 ID 보존·Iris ID 비노출을 확인했다. 기록: `/tmp/shared-go-handoff-race.pDyVkS.json`.
- 변경된 `youtubedispatch` 패키지의 golangci-lint는 새 회귀 시험의 공백 지적을 정리한 뒤 0 issues였다.
- rootless Podman으로 `workspace-build` stage를 실제 빌드했다. bot 산출물의 `go version -m`에서 root·core·postgres·transport·openai가 모두 sibling 소스로 연결됨을 확인했다. 기록: `/tmp/shared-go-workspace-build.fIrY4S/`. 최종 runtime 이미지 기동이나 운영 artifact 승급은 하지 않았다.
- Hololive workflow 정책 검사 2파일 통과. 실제 게시 tag/checksum 검증과 원격 CI 실행, 운영 배포·rollback·TLS/권한 검증은 수행하지 않았다.

### 변경 경로 benchmark

같은 Go 1.27.4·장비에서 각 1회 측정한 관측값이다. wall-clock 합격선이나 일반적인 성능 개선율을 주장하지 않는다. map 경로는 재귀 정제에서 차단으로 의미가 달라졌다.

| BenchmarkSanitizeHandler 접미사 | 변경 전 ns/op | 변경 후 ns/op | 변경 전 allocs/op | 변경 후 allocs/op |
|---|---:|---:|---:|---:|
| Clean | 708.4 | 603.2 | 0 | 0 |
| Sensitive | 4774 | 4204 | 18 | 18 |
| GroupNoSecret | 1294 | 1056 | 6 | 6 |
| WideClean | 1177 | 1050 | 1 | 1 |
| WidePrivacy | 1336 | 1173 | 3 | 3 |
| PrivacyKeys | 708.8 | 629.1 | 0 | 0 |
| PrivacyMap | 1117 | 617.1 | 4 | 2 |
| PrivacyGroup | 244.4 | 240.1 | 0 | 0 |
| GroupWithSecret | 1240 | 1199 | 7 | 7 |

### 게시 진행과 남은 검증 한계

- ChatBotGo 전체 race 명령은 제품 패키지들이 통과했지만 `scripts`의 `TestVerifyProductionModuleMetadataMatchesReviewedModules`·`TestVerifyProductionModuleMetadataWithoutGoCache` 두 실제 pin/checksum 사례에서 실패했다. 이 작업에서 새 모듈을 게시하지 않았고 정식 checksum이 없기 때문이다. 검사를 건너뛰거나 checksum을 만들지 않았으며, bot 산출물 검사는 신규 네 모듈도 요구하도록 확장했다. 부정 산출물 거부 시험은 통과했다.
- 어댑터·8개 소비자의 **실제 게시 pin** `GOWORK=off` 검증, 실제 tag/commit 번들 roundtrip·서명·attestation·immutable release는 커밋 guard 예외 거절에 따라 보류했다. 임시 modfile/workspace 검증을 이 증거로 대체하지 않는다. 재개 시 루트 게시 후 실제 checksum을 확정·커밋한 뒤 어댑터 태그를 만든다. 하위 모듈이 repository Latest를 덮어쓰지 않도록 `--latest=false`를 적용했다.
- 운영 endpoint·원격 CA/SAN·권한·실제 SNAT/HBA 경로와 archive 수집 인수는 미검증이다. 격리 pooler는 `/tmp/shared-go-pooler-acceptance.hHLgGM/acceptance.log`에서 실제 실행·정리를 확인했다. idle-in-transaction/transaction timeout은 GUC 격리만 확인했고 만료·crash·COMMIT 응답 유실은 주입하지 않았다. 운영 PgBouncer 가동·전환·기존 로그 이동/삭제는 수행하지 않았다.
- shared-go의 로컬 `refactor/shared-go-boundaries-v2.24.0` 브랜치와 소비자 checkout에 미커밋 변경을 보존했다. 이 작업의 commit·태그·원격 push·배포는 없다. Fallback delta: ChatBotGo의 decode 미완료 허용 예외를 제거했으며 새로운 fallback·재시도·보안 완화는 없다.
- 임시 fixture 정리는 도구의 `rm -rf` 차단 정책으로 실행되지 않았다. 우회하지 않고 `/tmp/shared-go-baseline.rTJdKg`, `/tmp/shared-go-manifests.qKFJBP`, `/tmp/shared-go-validation.NjSqbC`, `/tmp/shared-go-pooler-acceptance.hHLgGM`와 `/tmp/omp-shared-go-baseline-path`를 보존했다. 이 작업이 만든 합성 시험 자료다. pooler 시험 컨테이너는 모두 제거되었고 volume·network·pod는 만들지 않았다.

### 게시 재개 승인

- 사용자가 게시 재개와 이번 shared-go 286개 경로 선택 커밋에 한정한 `IRIS_STACK_ALLOW_BULK_STAGE=1` 예외를 명시적으로 승인했다. 이전 보류 기록은 당시 판단으로 보존하며, 다른 hooks·비밀 검사·품질 게이트는 우회하지 않는다.
- 재개 점검 시 로컬 HEAD는 `8952628`, 원격 main은 `14c7079`였고 목표 다섯 태그는 없었다. 기존 staged 변경·실행 중 Git 프로세스·Git 및 writer guard 잠금은 없었다. 선택 범위의 미기록 import 이관·시험 fixture 정리·공백 변경도 소유권을 검토했다.
- 승인 범위는 `park285/shared-go`의 PR/main 통합, root `v2.24.0`과 네 하위 모듈 `v1.0.0` 게시, 소비자 로컬 실제 pin 검증이다. 소비자 저장소 게시·운영 배포·운영 데이터 및 비밀 접근은 포함하지 않는다.
- 승인 뒤 선택 커밋을 시도했으나 eval의 `env()`와 persistent Bash의 `export` 모두 commit 도구의 hook subprocess에 예외 값을 전달하지 못했다. 두 시도 모두 같은 286개 경로의 staging guard에서 종료됐고 임시 환경값은 복원했다. commit 공개 인터페이스에는 환경변수 전달 인자가 없다. 도구 문제를 신고했으며 직접 `git commit`, `no_verify`, hook 변경으로 우회하지 않았다. 커밋·PR·태그·릴리스·소비자 실제 pin 검증은 이 도구 제약으로 미완료다.
- 재개 시점의 root에 `GOMAXPROCS=4 bash scripts/ci/release-gate.sh .`를 실행해 독립 tidy·Unicode·할당 상한·SQL ownership·lint/build·전체 race·govulncheck를 통과했다(0 lint issues, 발견된 취약점 없음). 기록은 `/tmp/shared-go-release-root.RKm1BK.log`다. 변경 없는 core의 이전 독립 gate 결과는 재사용하며, 실제 게시 pin·서명·원격 CI 증거로 대체하지 않는다.
- 사용자가 이번 286개 경로 커밋에 한해 대체 절차를 추가 승인했다. 별도 임시 index의 동일 후보에 정상 pre-commit·commit-msg hooks를 먼저 실행하고 모두 통과하며 후보가 바뀌지 않은 경우에만 선택 커밋 도구의 `no_verify=True`를 1회 사용한다. 서명·reference-transaction·push hooks는 유지하고 사용자 index·hook 파일은 수정하지 않는다.
- 정상 pre-commit·commit-msg hooks를 임시 index에서 통과했다(변경 159문서와 메시지 1문서 비밀 검사 포함). 동일 후보를 `e1e2f0ad67`로 커밋했고 승인된 `no_verify` 예외는 이 커밋에서 소진했다. 기존 main `14c7079` 통합에서는 이미 이전한 응답 도우미의 구경로를 복원하지 않고, 실제 소비 중인 refusal sentinel과 provider-free 루트 의존성을 보존했다. 병합 뒤 root의 독립 tidy 검사는 통과했다.
- 선택 커밋 도구가 병합 부모를 보존하지 않아 `6aac7c1041`은 단일 부모 커밋으로 생성됐다. 원본 checkout에 이 작업의 merge에서 생긴 staged 항목 4개(`CHANGELOG.md` 수정, `README.md`·`go.sum`·`pkg/llm/llm.go` 삭제)가 남았으며 제품 파일은 삭제되지 않았다. 이 상태와 기존 두 커밋은 원본에서 보존한다.
- `stack-worktree.sh`로 공개 main `14c7079` 기반의 `refactor/shared-go-boundaries-release-v2.24.0` worktree를 만들었다. 선형 통합 후보 전체가 소유·검증한 `6aac7c1041` 소스 트리와 일치함을 확인했다. 사용자는 이 270개 경로의 통합 커밋에 한해 정상 hooks 선실행 후 `no_verify=True`를 1회 더 승인했다. 새 worktree의 이 작업 전용 index만 정리했으며 원본 checkout·다른 작업트리·운영 runtime은 변경하지 않는다.
- 최신 main 기반 선형 통합을 `3b09c75ec0`으로 커밋했다. push의 필수 ShellCheck에서 릴리스 helper의 동적 source 경로를 해석하지 못하는 SC2154를 발견해 세 호출 스크립트에 정확한 source 지시문을 추가했다. 같은 `--norc -x --severity=warning` 조건으로 세 스크립트가 통과했으며 검사를 비활성화하거나 실행 동작을 바꾸지 않았다.
- PR #108의 다섯 모듈 CI와 GitGuardian 검사는 통과했지만 main ruleset이 요구하는 `fast-gate`와 matrix 상태 이름이 달라 병합이 거부됐다. 원격 정책은 바꾸지 않고 matrix를 `module-gate`로 분리한 뒤 모든 결과가 성공이어야 통과하는 `fast-gate` 집계 job으로 기존 계약을 복원했다. 변경 workflow의 공급망 정책 검사는 통과했다.
- 복원한 `fast-gate`를 포함한 PR CI `44067275915`가 통과했고 PR #108을 `689a608a54097d7ebce086f2aeef4af7480b270e`로 squash 병합했다. main CI `44067855280`과 보안 검사 `44067855443`도 통과했다. ruleset 우회나 정책 변경은 없었다.
- 같은 main commit에 annotated `v2.24.0`을 생성·게시했다. release run `44068222286`의 독립 root gate·번들·SBOM·asset attestation은 통과했고 immutable release `448492203`이 생성됐다. 게시 1초 뒤 `gh release verify`는 attestation 없음으로 실패했다. 재게시·workflow 재실행 없이 이후 동일 릴리스의 immutable attestation, 네 다운로드 asset의 provenance, archive에 결합된 SPDX와 source commit·archive 바이트 대조를 모두 통과했다. 실제 원격 run의 실패 상태는 보존한다. 로컬 증거는 `/tmp/shared-go-release-evidence.7ZnauH/`에 있다.
- 후속 게시에서는 자동 생성 release attestation의 조회 공백만 5초 간격·120초 기한으로 기다리도록 publisher를 수정했다. 정확한 tag object와 release predicate만 조회하며 API 오류·잘못된 응답·기한 초과는 실패다. 게시·암호학적 검증은 재시도하지 않는다. `bash -n`, 기존 ShellCheck 옵션과 실제 root tag object의 release-filtered API 조회가 통과했다. 이 읽기 전용 가용성 대기가 유일한 추가 제품 코드 retry이며 runtime fallback은 없다.
- 실제 root pin 준비는 처음에 `GOTOOLCHAIN=local`이 호스트의 Go 1.27.3을 선택해 중단됐고, 저장소 버전 `go1.27.4`로 고정한 뒤에는 공식 checksum DB의 `unknown revision v2.24.0`으로 중단됐다. Go 공식 mirror 문서의 게시 전 조회 음성 캐시 최대 30분 계약에 따라 제한된 읽기 전용 대기를 실행했다. 세 차례의 60초 간격 대기 뒤 version info의 Origin은 정확한 main commit으로 확인됐지만 checksum DB TCP timeout에서 중단됐다. 다른 오류를 계속 재시도하거나 checksum 검증을 끄거나 proxy를 바꾸지 않았다. canonical 어댑터·소비자 checksum은 아직 확정하지 않았다.
- PR #109의 필수 CI `44071688646` 통과 뒤 `7481503b4761d15baa3d0c5f8e32fd7051b977eb`로 병합했다. main CI `44071937262`·보안 검사 `44071937438`도 통과했다. root 의존성이 없는 `core/v1.0.0`을 이 commit에 annotated tag로 게시했다.
- core release run `44072427522`도 독립 gate·번들·서명·릴리스 생성은 통과했지만 게시 직후 availability API가 빈 배열이 아니라 HTTP 404를 반환해 실패했다. 실제 기존 tag object의 200/count=1과 attestation이 없는 기존 commit의 404/status를 확인했다. 이제 이 정확한 readiness 조회의 404도 대기하며, 다른 HTTP/API 오류·잘못된 응답과 암호학적 검증 실패는 즉시 실패한다. 태그나 릴리스를 재생성하지 않는다.
- root의 공식 checksum 검증은 게시 후 30분 이내에 통과했고 Origin이 `689a608a54097d7ebce086f2aeef4af7480b270e`와 일치했다. 임시 어댑터 modfile 세 개의 실제 `GOWORK=off` tidy가 모두 통과했으며 go.mod 변경 없이 root의 검증된 checksum 두 행만 추가됐다. 이 값만 각 canonical go.sum에 반영했다. 세 어댑터의 독립 release gate와 게시, 소비자 실제 pin 검증은 아직 남아 있다.
- 게시된 core의 다운로드 번들·source commit/archive 바이트·네 asset provenance·SPDX·immutable release attestation을 모두 검증했고 repository Latest가 `v2.24.0`임을 확인했다. 공식 core checksum과 Origin의 `Subdir=core`, commit `7481503b4761d15baa3d0c5f8e32fd7051b977eb`도 확인했다. core의 원격 release run 실패 기록은 보존한다.
- canonical checksum 반영 뒤 `GOWORK=off GOTOOLCHAIN=go1.27.4 GOMAXPROCS=4 bash scripts/ci/release-gate.sh postgres transport openai`를 한 번 실행해 세 모듈 모두 통과했다. tidy·SQL ownership·lint/build·race·govulncheck, PostgreSQL 필수 사례와 skip=0을 포함한다. 기록은 `/tmp/shared-go-published-adapter-gate.iGbD6Y.log`다. 404 분류 수정의 `bash -n`·ShellCheck도 통과했다.
