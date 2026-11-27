# 변경 이력

출시 항목은 실제 Git tag를 기준으로 작성합니다. tag는 소비 저장소가 쓸 변경을 확정해 모듈을 게시할 때만 만들고,
그 사이 병합한 변경은 `미출시`에 누적합니다. 소비 저장소는 게시된 버전을 검증한 뒤 `go.mod`·`go.sum`을 갱신합니다. 발행 PR에서는 다음 release 섹션 하나만 앞에 준비할 수 있으며,
release 섹션은 최신 순으로 배치합니다. 이 문서는 `미출시`와 최근 세 버전 구간만 유지하고 발행 PR에서는 준비하는 release 섹션을 그 첫 번째로 세며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **비호스트 lint 범위 축소와 CI lint 캐시:** lint 행렬의 호스트 외 설정(plan9·windows·darwin·illumos·wasip1)은 모듈 전체 대신 그 설정을 요구한 파일이 있는 패키지만 검사합니다. 호스트 설정은 그대로 모듈 전체를 검사하고, 커버리지도 371/371로 같습니다. 플랫폼과 무관한 패키지는 호스트 외 플랫폼에서 더 이상 타입 검사하지 않습니다. `module-gate`는 lint 행렬 캐시를 `actions/cache` v6.1.0으로 복원하고, main push에서만 저장합니다. 빈 캐시에서 `make lint MODULES=.`는 로컬 측정으로 90.1초에서 59.8초가 됐습니다.
- **릴리스 게이트 lint 통합:** `release-gate.sh`가 선택한 모듈의 lint를 한 번에 실행하고 각 모듈은 `go build`로 빌드합니다. 단독 `make build`는 계속 lint를 먼저 실행합니다.

## sqlite/v1.0.0 - 2026-11-28

- **새 `sqlite` 모듈(`github.com/park285/shared-go/sqlite`):** `sqlitedb`가 CGO 없는 `ncruces/go-sqlite3` v0.38.1로 SQLite 파일 하나를 엽니다. `<경로>.lock` flock으로 한 프로세스만 `Open`하고, 쓰기 연결 1개(`BEGIN IMMEDIATE` `WriteTx`, 콜백이 panic해도 롤백)와 `query_only` 읽기 풀, WAL·`synchronous=FULL`·`foreign_keys=ON`, 내장 마이그레이션 원장(`sqlitedb_migrations`), 결과 불명 COMMIT의 `ErrCommit`을 제공합니다. 주기 WAL checkpoint는 writer를 기다리게 하지 않는 PASSIVE이고 닫을 때만 TRUNCATE합니다. `Attach`는 잠금 파일 없이 기존 파일에 붙는 운영 명령용입니다.
- `sqlitestore`는 irisdurable inbox·nonce 계약의 SQLite 구현입니다. 만료된 lease를 자동 회수하지 않으며(`pgstore`의 `InboxExplicitRecovery=true`에 해당) 소비자가 `ApplyInboxRecovery`로 정리합니다. 커밋이 실패하거나 롤백되면 prune·claim 해제 건수는 0입니다.
- 릴리스 workflow·번들 스크립트·계약 모듈 목록에 `sqlite/v1.*`를 추가했습니다. 첫 소비자는 chat-bot-go-kakao와 hololive-bot입니다.

## v2.26.1 - 2026-11-26

- **평문 서식 모사 퇴역:** `kakaoformat.Render`가 강조·기울임을 수학용 Unicode 알파벳이나 괄호로 바꾸고 결합 취소선을 붙이던 처리를 제거했습니다. 제목 장식, 단계별 특수 글머리표, 인용 장식, 코드 박스와 표의 열별 장식도 제거했습니다. 평문은 원래 문자와 URL을 유지하고, 목록·할 일 상태·인용·표는 일반 문자로 구조를 보존합니다. 코드 본문은 원문 공백과 줄바꿈을 보호하며 넓은 fence 안의 내부 fence·언어 이름을 재작성하지 않습니다. 위·아래 첨자 등 수식의 의미 표기는 유지합니다.
- `Render` 등 공개 API와 소비자의 방 유형·RichText 선택 정책은 유지합니다. Markdown 파서, CJK 경계 처리, 코드 범위 탐색과 이스케이프 API는 평문·리터럴 보호에 계속 필요합니다. 네이티브 RichText는 원문을 Iris에 전달하며 이 평문 변환을 먼저 적용하지 않습니다. 새 출력 계약은 소비자가 다음 릴리스로 pin을 갱신하거나 변경된 shared-go 소스를 workspace에서 사용할 때 적용됩니다.
- 원문 Markdown만 저장하는 소비자는 재시도 때 최종 본문이 달라질 수 있으므로, 새 렌더러 적용과 롤백 전에 기존 미완료 요청을 이전 버전의 확인·복구 절차로 처리합니다. cbgk의 inbox·답변 outbox·멘션 claim 전환 조건은 README의 연결 문서를 따릅니다.

## v2.26.0 - 2026-11-26

- **SSE 프레임 reader:** 새 `pkg/sse`의 `Events(r, Limits)`가 `text/event-stream` 응답을 `Event{Type, Data}` 반복자로 돌려줍니다. 이벤트 데이터 바이트·`event` 길이·데이터 줄 수 상한을 넘거나 0 이하 상한을 받으면 오류로 멈추며, 상한 초과는 `ErrEventTooLarge`로 구분합니다. 필드 값은 SSE 규격대로 앞 공백 하나만 떼고, 주석·`id`·`retry` 줄은 무시하며, 마지막 이벤트는 빈 줄 없이 끝나도 전달합니다. chat-bot-go-kakao의 OpenAI Responses·이미지 스트림과 Anthropic Messages grounding이 각자 두던 파서 대신 이 reader를 씁니다. 기존 공개 API는 바뀌지 않습니다.
- **완료된 채택 문서 퇴역:** 소비자 채택이 끝난 `docs/adoption/shared-go-v1.27.0-admin-surface.md`를 지웠습니다.
- 의존성을 최신 안정판으로 올렸습니다. `golang.org/x/net` v0.63.0, `openai/openai-go/v3` v3.105.0(`openai` 모듈) 등 Go 모듈과, promptguard 규칙 팩의 YAML 파서를 `go.yaml.in/yaml/v4` v4.0.0으로 옮겼습니다. CI의 `actions/setup-go`·`actions/setup-python`은 v7.1.0, uv는 0.13.9, govulncheck 핀은 v1.10.0입니다. 공개 Go API는 바뀌지 않습니다.
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
- 운영 Go 코드의 PostgreSQL 규칙을 스택 golangci-lint forbidigo 규칙으로 받습니다. `pgxpool`·`pgx`·`pgxdb.Querier`의 `Exec`·`Query`·`QueryRow`와 `pgx.Batch.Queue`를 직접 부르지 못하게 해 SQL은 `pgxdb.Statement`로만 실행됩니다. `pgx.BeginFunc`·`BeginTxFunc`, 트랜잭션 직접 `Rollback`, `pgxpool.New`·`NewWithConfig`도 거부합니다. `postgres/pgxdb`의 실행 래퍼와 `OpenPool`만 줄 단위 `nolint`로 예외를 둡니다. 이 규칙이 스택 ast-grep 규칙 `go-postgres-conventions.yml`을 대신하므로 그 사본을 지웠습니다.
- **형식 검증 동기화:** `InboxClaim`의 동기화 커밋을 `00182e5`로 옮겼습니다. `pgxdb.Statement` 이관 전후의 렌더한 SQL이 같아 모델은 바꾸지 않았고, 정상 모델 통과와 음성 대조 두 건의 위반을 다시 확인했습니다.
- **TwentyQ 퇴역:** 렌더한 `.golangci.yml`에서 twentyq 경계 규칙을 지우고 README의 소비자 목록과 예시에서 twentyq-bot을 뺐습니다. 공개 API는 바뀌지 않습니다.
- 루트 모듈은 `v2.25.0` 이후 위 변경을 담습니다. `core`·`postgres`·`transport`·`openai` 모듈은 이번에 게시하지 않습니다.
