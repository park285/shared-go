# shared-go

Iris Stack의 Go 프로젝트들(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot` 등)이 공통으로 사용하는 유틸리티 라이브러리 전용 Go 모듈입니다.

본 라이브러리는 `cmd/` 실행 진입점이 없는 순수 라이브러리(Library-only) 모듈로 설계되었으며, 이를 호출하는 서비스들을 위해 일관되고 안정적인 API 사양을 유지합니다.

> **호환성 정책**: 이 모듈의 호환성 보장 대상은 iris-stack 내부 소비자(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot`)로 한정됩니다. 외부 사용을 막지는 않지만 외부 importer 호환성은 보장하지 않으며, 스택 내 소비자가 0인 exported API는 major 승격 없이 minor 릴리스에서 제거될 수 있습니다.

## 설치 (Installation)

```bash
go get github.com/park285/shared-go@latest
```

## 제공 패키지 목록 (Package Catalog)

| 패키지 경로 | 기능 및 역할 |
|---|---|
| `pkg/backoff` | 시도 횟수 및 상태를 기반으로 한 지수 백오프(Exponential Backoff) 계산 유틸리티 (대기 및 재시도 루프 제어는 호출부에서 직접 처리) |
| `pkg/db/pgxdb` | `jackc/pgx/v5` pgxpool 기반 PostgreSQL 연결 풀 생성 도구 (sslmode 명시 강제, 인증 실패·context 취소 시 즉시 중단하는 `OpenPoolWithRetry`, compose 서비스명 `postgres` 한정 localhost DNS 폴백) |
| `pkg/db/sqldb` | pgx 무의존 표준 `database/sql` `*sql.DB` 커넥션 풀 파라미터 적용 도구 |
| `pkg/dbmigrate` | embed.FS의 `manifest.txt` 순서대로 SQL 마이그레이션 파일을 실행하는 공통 처리 모듈 (`database/sql` 또는 pgx 실행 함수 주입 방식) |
| `pkg/envutil` | 환경 변수 로드 및 `*_FILE` 형태의 파일 경로로 보안 토큰/시크릿 값을 주입하는 도구 |
| `pkg/ginjson` | Gin 웹 프레임워크용 Sonic 라이브러리 기반 고성능 JSON 렌더링 및 바인딩 모듈 |
| `pkg/h3` | HTTP/3 전송 프로토콜 설정 도구 (자체 CA 번들 등록, TLS 상세 사양 정의) |
| `pkg/healthprobe` | 서비스 헬스체크 및 프로브(Readiness / Liveness Probe) 도구 |
| `pkg/httputil` | HTTP 클라이언트 커넥션 풀링 및 프로파일 구성 도구 |
| `pkg/json` | Sonic 엔진을 내장한 고성능 JSON 인코딩/디코딩 추상화 계층 |
| `pkg/jsonutil` | 텍스트 혹은 HTTP 응답 문자열로부터 유효한 JSON을 정규화하여 추출하는 헬퍼 유틸리티 |
| `pkg/llm` | LLM provider 클라이언트 추상화 (`JSONGenerator` 인터페이스, OpenAI Responses 호환 JSON 생성, Codex CLI 실행/로그인, 응답 redaction) |
| `pkg/logging` | Slog 기반의 구조화된 로깅 모듈 (비동기 처리, 민감한 키 정보 마스킹 및 실시간 로그 로테이션 지원) |
| `pkg/netguard` | 외부 HTTP 대상 URL 및 dial 주소를 fail-closed로 검증하는 네트워크 가드 (private/loopback/link-local/ULA 대역 차단, `Policy.AllowedHosts` allowlist 지원) |
| `pkg/obsmetrics` | `client_golang` 의존성 없이 Prometheus 평문 텍스트 exposition을 생성하는 메트릭 키트 (webhook/런타임 메트릭, prefix 네임스페이스 분리) |
| `pkg/outputguard` | LLM 생성 출력 가드 (구조화된 차단 사유, 출력·보호 텍스트 크기 제한, 역할/시크릿/보호 지침 중첩 탐지, bounded TTL index cache) |
| `pkg/promptguard` | source-aware 프롬프트 인젝션 가드 (embedded v3 한/영 baseline, optional rules-only overlay, bounded decoding, policy digest, TTL cache) |
| `pkg/retry` | `pkg/backoff`의 지연 값 계산을 사용해 context 취소를 존중하며 sleep·재시도·중단을 수행하는 재시도 루프(`WithRetry`) 구현체 |
| `pkg/runtime` | Go 런타임 최적화를 포함한 프로세스 부트스트랩 도구 (`automaxprocs`, 애플리케이션 라이프사이클 관리, HTTPServer) |
| `pkg/stringutil` | 범용 문자열 처리 유틸리티 |
| `pkg/telemetry` | OpenTelemetry 기반의 분산 트레이싱(Tracing) 정보 설정 및 컨텍스트 전파 유틸리티 |
| `pkg/workerconfig` | 개별 백그라운드 워커들의 동작 프로파일 설정 로드 모듈 |
| `pkg/workerpool` | 큐(Queue) 기반의 동시성 제어 워커 풀 구현체 |

새로운 공통 기능이 필요할 경우, `pkg/` 하위에 신규 패키지 형식으로 추가해 주십시오.

## Prompt guard 계약

`Check` 호출은 `Source`와 `Enforcement`를 모두 명시해야 합니다. `EnforcementUnspecified`와 알 수 없는 값은 탐지나 cache 조회 전에 `ErrInvalidCheckRequest`로 거부됩니다.

| `Enforcement` | `Allow` | `Review` | `Block` |
|---|---|---|---|
| `EnforcementObserve` | 허용 | 허용 | 허용 |
| `EnforcementInteractive` | 허용 | 허용 | 거부 |
| `EnforcementPersistent` | 허용 | 거부 | 거부 |

지원 source는 `user_prompt`, `prompt_bundle`, `retrieved_memory`, `memory_candidate`, `session_patch`, `simulation_state`, `law_context`, `session_context`, `chat_log`, `web_search_result`, `image_prompt`로 고정됩니다. 저장·요약·재사용되는 데이터에는 `EnforcementPersistent`를 사용해야 합니다.

`UseEmbeddedDefaults=true`는 `pkg/promptguard/rulepacks`의 v3 baseline을 먼저 로드합니다. `RulepacksDir` 또는 `RulepackFS`를 함께 지정하면 v3 `kind: rules` overlay 하나만 추가할 수 있으며, policy 변경과 baseline rule ID 중복은 시작 단계에서 거부됩니다. `PolicyDigest()`는 engine version과 최종 유효 policy/rules의 결정론적 digest이며 사용자 입력을 포함하지 않습니다. Runtime override 없이 v3 policy 문서만 threshold를 소유합니다.

## Benchmark gate와 baseline

`make perf-gate`와 `make guard-perf-gate`는 fail-closed bridge입니다. 각 target은 안정된 서로 다른 `--gate-id`로 candidate를 수집한 뒤 기존 baseline과 비교합니다. baseline이 없거나 읽을 수 없으면 exit 2로 실패하며, blocking entrypoint는 baseline을 생성·복사하지 않고 bootstrap 또는 legacy opt-out flag도 사용하지 않습니다.

v2 evidence는 현재 Prepare 단계입니다. candidate manifest는 repository, selection gate, gate ID, policy, `harness_files`, collector command, Git 상태, host/environment 및 result digest를 함께 고정합니다. `check-v2`는 candidate와 baseline의 strict sidecar·provenance가 모두 호환될 때만 읽으며, write·fallback 경로가 없습니다. 일반 gate는 compatible v2 baseline이 provision될 때까지 이 fail-closed bridge를 유지합니다.

baseline provision은 별도 승인된 작업입니다. approved `main`의 clean worktree를 고정 host에서 수집한 뒤에만 `bootstrap-baseline --approved-sha <40-hex-sha>`로 생성하거나 복원할 수 있습니다. candidate에서 baseline을 만들 수 없으며, 승인된 fixed-host evidence가 없으면 상태는 `BLOCKED_BASELINE`입니다.

Prompt guard는 v3 rulepack과 source/enforcement를 명시하는 `Check`만 지원합니다.

## 로컬 검증 (Verification)

개발 시 로컬에서 아래 명령어로 검증할 수 있습니다.

```bash
make lint
go test ./...
go build ./...
```

**CI 정책:** 본 리포지토리는 원격 깃허브 액션(GitHub Actions)이 실제 검증 주체입니다. `ci.yml`은 PR 및 `main` push마다 workflow secret 경계 검사, SQL ownership 검사, `gofmt`, `go vet`, `golangci-lint`, 경합 조건 검사를 포함한 테스트 슈트(`go test -race -count=1 ./...`), perf gate(벤치마크 회귀 검사)를 수행하며, `security.yml`은 `main` push·주간 스케줄·수동 dispatch 시 `govulncheck` 취약점 분석을 수행합니다.
