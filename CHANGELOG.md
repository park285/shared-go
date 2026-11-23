# 변경 이력

출시 항목은 실제 Git tag를 기준으로 작성합니다. tag는 소비 저장소가 쓸 변경을 확정해 모듈을 게시할 때만 만들고,
그 사이 병합한 변경은 `미출시`에 누적합니다. 소비 저장소는 게시된 버전을 검증한 뒤 `go.mod`·`go.sum`을 갱신합니다. 발행 PR에서는 다음 release 섹션 하나만 앞에 준비할 수 있으며,
release 섹션은 최신 순으로 배치합니다. 이 문서는 `미출시`와 최근 세 버전 구간만 유지하고 발행 PR에서는 준비하는 release 섹션을 그 첫 번째로 세며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- 의존성을 최신 안정판으로 올렸습니다. `golang.org/x/net` v0.63.0, `openai/openai-go/v3` v3.105.0(`openai` 모듈) 등 Go 모듈과, promptguard 규칙 팩의 YAML 파서를 `go.yaml.in/yaml/v4` v4.0.0으로 옮겼습니다. CI의 `actions/setup-go`·`actions/setup-python`은 v7.1.0, uv는 0.13.9, govulncheck 핀은 v1.10.0입니다. 공개 Go API는 바뀌지 않습니다.
- 통합 시험과 `pgstore` 릴리스 게이트의 PostgreSQL 이미지를 `postgres:19.0-alpine`(digest 고정)으로 올렸습니다. 공개 Go API는 바뀌지 않습니다.
- depguard로 `unsafe` 패키지 가져오기를 기본 금지했습니다. 공통 린트 행렬도 명시적 `unsafe` import와 cgo 사용을 거부하며 해당 파일의 경로를 출력합니다.
- 로컬·CI·릴리스 게이트에서 golangci-lint와 govulncheck를 스택 공통 resolver로 해석합니다. 두 버전 핀은 `scripts/ci/pre-push-gate.sh`에 선언하며 도구 경로 재정의는 거부합니다. 오프라인 설치와 캐시 검증도 공통 resolver가 담당합니다. Make는 실행 파일과 고정 버전을 공통 행렬에 함께 전달합니다. 행렬은 루트 `.golangci.yml`로 각 구성의 모듈 전체와 테스트를 검사하고, 단독 생성기도 검사해 실제 로더 입력으로 커버리지를 확인합니다. `make fmt`는 선택한 모듈의 현재 빌드 구성에 자동 수정을 적용합니다.
- CI는 형제 모듈 변경을 함께 검사하도록 체크아웃 안에 소스 `go.work`를 생성해 모듈별 린트에 전달합니다. Make와 독립 Go 게이트는 `GOWORK=off`를 기본값으로 강제해 상위 스택의 `go.work`가 검사 결과를 바꾸는 문제를 수정했습니다. CI의 소스 통합 검사에는 체크아웃 내부에서 생성하고 검증한 workspace를 명시적으로 전달합니다. CI·보안 행렬은 모든 모듈을 자동 발견하고 릴리스 대상 다섯 모듈의 경로와 모듈 선언을 확인합니다. 로컬 릴리스 게이트도 같은 계약 검사를 거쳐 발견한 모든 모듈을 검사하며, 상위 Make 설정을 지우고 각 단계에 대상 모듈을 명시합니다. Make 명령줄의 `MODULES`는 상속한 CI 모듈 선택보다 우선합니다.
- Windows, WASI(`wasip1/wasm`), JavaScript(`js/wasm`)에서는 파일 0640·디렉터리 0750 권한을 보장할 수 없어 파일 로깅이 경로 생성 전에 `errors.ErrUnsupported`를 반환합니다. 그 밖의 플랫폼에서는 지원 여부에 따라 `O_NOFOLLOW` 또는 배타적 생성(`O_EXCL`)을 사용하고, 생성과 권한 변경 후 파일과 디렉터리의 실제 모드를 확인합니다. 로그 디렉터리의 setgid·sticky 비트는 권한을 맞출 때도 보존을 요청해 수집 그룹 상속을 유지합니다. 프로세스가 디렉터리 그룹의 구성원이 아니어서 커널이 setgid를 지우면 오류로 보지 않습니다. Plan 9에서는 압축 백업의 archive 이동이 디렉터리 간 rename을 필요로 해 `errors.ErrUnsupported`를 반환합니다(이전에도 동작하지 않았습니다). 제한된 umask를 검증하는 테스트는 Unix에서 실행하며, lifecycle 시그널 테스트는 모든 구성에서 빌드합니다.
- Make와 게이트의 모듈 선택에서 `CDPATH`를 제거하고, pre-push와 CI가 상위 Make의 dry-run 설정으로 검사를 건너뛰는 문제를 수정했습니다. CI는 로그·메트릭 테스트를 Linux·Darwin·Windows amd64 대상으로 빌드하고 Windows에서 파일 로깅 거부와 메트릭 출력을 검사합니다.
- `httputil` 테스트는 소켓을 열지 않고 설치된 dialer와 기한 만료 동작을 확인합니다.
- **주석 금지:** 기계 지시문을 뺀 Go·shell·Python 주석과 doc 주석을 지웠습니다. shell heredoc으로 실행하는 Python 본문도 같은 검사를 받으며, 파일별 MIT 머리글은 지우고 라이선스 고지는 루트 `LICENSE`가 유지합니다. 새 `scripts/ci/pre-push-gate.sh`와 `scripts/ci/release-gate.sh`가 `bash scripts/ci/verify-tools.sh scan`과 `formal`을 실행합니다.
- **형식 검증:** `verification/tla/InboxClaim/`이 inbox claim·lease·완료와 수락 순번 선두, 같은 파티션의 단일 처리권, 재예약 뒤 키 간 선발 순서를 불변식 8개로 검사합니다. `verification/lean/BoundedQueue/`가 bounded queue 연산을 증명하며 같은 명세를 `bounded_queue_trace_fuzz_test.go`가 구현에 대어 봅니다.
- **테스트:** 실제 시간을 기다리던 `time.Sleep` 9곳을 synctest·잠금 관측 동기화로 바꾸고 `httputil` body 상한 fuzz 대상을 추가했습니다.
- 결정 기록 목록을 폐기했습니다. 과거 결정은 Git 이력에서 확인할 수 있습니다.
- 스택 ast-grep 규칙 `.ast-grep/rules/go-postgres-conventions.yml`을 받았습니다. `verify-tools.sh scan`이 운영 Go 코드의 인라인 SQL, `pgx.BeginFunc`·`BeginTxFunc`, 트랜잭션 직접 `Rollback`, `pgxpool.New`를 거부합니다. `postgres/pgxdb`는 롤백과 풀 생성을 소유하므로 두 규칙에서 제외합니다. 현재 위반은 없습니다.

## postgres/v1.0.3 - 2026-11-19

- **Inbox 수락 순서(`postgres` 모듈):** 같은 `(scope, ordering_key)`의 선두를 수락 시각이 아니라 키 잠금 아래에서 발급한 수락 순번(id)으로 고릅니다. DB 시계가 역행해도 뒤에 수락한 행이 앞 행의 정산 전에 처리권을 얻지 못합니다. 서로 다른 키 사이의 선발 순서 `(available_at, created_at, id)`와 공개 Go API는 그대로입니다. Fallback delta: none.
  - 소비자는 전환 전에 부분 인덱스 `idx_iris_webhook_inbox_head_by_id (scope, ordering_key, id) WHERE status IN ('pending', 'processing')`를 추가합니다. 기존 `idx_iris_webhook_inbox_head`는 유지합니다.
  - 구·신 claim 구현을 동시에 실행하지 않습니다. 전환과 롤백은 수락·claim·만료 복구를 멈춘 상태에서 README "Inbox 수락 순서"의 확인 SQL이 0행일 때만 합니다. 이 조회는 만료 복구로 `pending`이 된 재시도 행까지 포함해 두 순서의 선두가 다른 파티션을 찾습니다.
  - 참조 스키마(`postgres/pgstore/testdata/schema.sql`)의 주석을 지웠습니다.
- `postgres` 모듈의 Go·SQL 소스에서 기계 지시문을 뺀 주석과 doc 주석을 지웠습니다. 동작 변경은 위 항목뿐입니다.

## transport/v1.0.1 - 2026-11-18

- 루트 `v2.25.0`의 `httputil.ReadAllAndClose`가 healthprobe body 정리를 단독 소유합니다. 성공·실패·panic에서 body를 한 번만 닫으며 중복 backstop과 별도 정리 구현은 두지 않습니다. body 상한과 오류 분류는 유지합니다. Fallback delta: 없음.

## v2.25.0 / postgres/v1.0.2 / openai/v1.0.1 - 2026-11-18

- `httputil.ReadAllAndClose`와 `DrainAndClose`가 read·drain panic에서도 body를 정확히 한 번 닫고 원래 panic을 전파합니다. read·drain·close 오류의 원인은 함께 보존합니다.
- 스택 내 호출자가 없는 `ReadAllAndCloseWithDrainLimit`를 비공개화합니다. 기본 close API와 close 소유권을 호출자가 유지하는 `ReadAllAndDrain`은 유지합니다.
- 분리 모듈에 이관된 미사용 테스트 helper와 API 이름 존재 여부만 검사하던 reflection 시험을 제거합니다. 릴리스 스크립트는 ShellCheck가 source 파일의 위치를 해석하도록 경로를 명시합니다.
- PostgreSQL SCRAM은 pgx의 Unicode 3.2 미할당 문자 원문 보존 규칙을 따릅니다. pool timeout의 두 설정 시험은 서로 다른 listener에서 실행합니다.
- OpenAI 완료 텍스트 선택은 제외한 미완료 message를 fallback에서 다시 사용하지 않고, status 생략 응답의 집계와 tool envelope 제외 동작을 유지합니다. Fallback delta: 제외 대상의 재등장 제거.
