# shared-go

Iris Stack의 Go 프로젝트들(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot` 등)이 공통으로 사용하는 유틸리티 라이브러리 전용 Go 모듈입니다.

본 라이브러리는 `cmd/` 실행 진입점이 없는 순수 라이브러리(Library-only) 모듈로 설계되었으며, 이를 호출하는 서비스들을 위해 일관되고 안정적인 API 사양을 유지합니다.

> **호환성 정책**: 이 모듈의 호환성 보장 대상은 iris-stack 내부 소비자(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot`)로 한정됩니다. 외부 사용을 막지는 않지만 외부 importer 호환성은 보장하지 않으며, 스택 내 소비자가 0인 exported API는 major 승격 없이 minor 릴리스에서 제거될 수 있습니다.

## 설치 (Installation)

```bash
go get github.com/park285/shared-go/v2@latest
```

## 제공 패키지 목록 (Package Catalog)

| 패키지 경로 | 기능 및 역할 |
|---|---|
| `pkg/backoff` | 시도 횟수 및 상태를 기반으로 한 지수 백오프(Exponential Backoff) 계산 유틸리티 (대기 및 재시도 루프 제어는 호출부에서 직접 처리) |
| `pkg/db/pgxdb` | `jackc/pgx/v5` pgxpool 기반 PostgreSQL 연결 풀 생성 도구 (sslmode 명시 강제, `Config.SSLRootCert` → `POSTGRES_SSLROOTCERT` env 순의 sslrootcert 폴백, 재시도는 호출자 소유) |
| `pkg/db/sqldb` | pgx 무의존 표준 `database/sql` `*sql.DB` 커넥션 풀 파라미터 적용 도구 |
| `pkg/dbmigrate` | embed.FS의 `manifest.txt` 순서대로 SQL 마이그레이션 파일을 실행하는 공통 처리 모듈 (`database/sql` 또는 pgx 실행 함수 주입 방식) |
| `pkg/envutil` | 환경 변수 로드 및 `*_FILE` 형태의 파일 경로로 보안 토큰/시크릿 값을 주입하는 도구 |
| `pkg/ginjson` | Go 1.27 `encoding/json/v2` JSON을 HTML-safe로 인코딩하는 Gin renderer와 response helper |
| `pkg/h3` | HTTP/3 전송 프로토콜 설정 도구 (자체 CA 번들 등록, TLS 상세 사양 정의) |
| `pkg/healthprobe` | 서비스 헬스체크 및 프로브(Readiness / Liveness Probe) 도구 |
| `pkg/httputil` | HTTP 클라이언트 커넥션 풀링 및 프로파일 구성 도구 |
| `pkg/jsonutil` | 텍스트 혹은 HTTP 응답 문자열로부터 유효한 JSON을 정규화하여 추출하는 헬퍼 유틸리티 |
| `pkg/kakaoformat` | Markdown 표현을 카카오 일반 채팅용 평문과 링크로 변환하는 formatter |
| `pkg/llm` | LLM provider 클라이언트 추상화 (`JSONGenerator`, OpenAI 호환 JSON 생성·가드, 진단 redaction) |
| `pkg/llm/openaipreset` | `pkg/llm`의 OpenAI 호환 JSON 생성 경로를 functional options로 구성하는 재사용 preset |
| `pkg/lockutil` | FNV-1a hash로 key를 고정 256개 shard에 배정하는 bounded keyed mutex |
| `pkg/logging` | Slog 기반의 구조화된 로깅 모듈 (비동기 처리, 민감한 키 정보 마스킹 및 실시간 로그 로테이션 지원) |
| `pkg/netguard` | 외부 HTTP 대상 URL 및 dial 주소를 fail-closed로 검증하는 네트워크 가드 (private/loopback/link-local/ULA 대역 차단, `Policy.AllowedHosts` allowlist 지원) |
| `pkg/obsmetrics` | `client_golang` 의존성 없이 Prometheus 평문 텍스트 exposition을 생성하는 메트릭 키트 (webhook/런타임 메트릭, prefix 네임스페이스 분리) |
| `pkg/outputguard` | LLM 생성 출력 가드 (구조화된 차단 사유, 출력·보호 텍스트 크기 제한, 역할/시크릿/보호 지침 중첩 탐지, bounded TTL index cache) |
| `pkg/panicguard` | callback panic을 복구해 구조화된 진단을 남기는 실행 경계 |
| `pkg/promptguard` | source-aware 프롬프트 인젝션 가드 (embedded v3 한/영 baseline, optional rules-only overlay, bounded decoding, policy digest, TTL cache) |
| `pkg/reflectutil` | interface에 담긴 nilable 값을 안전하게 판별하는 reflection helper |
| `pkg/retry` | `pkg/backoff`의 지연 값 계산을 사용해 context 취소를 존중하며 sleep·재시도·중단을 수행하는 재시도 루프(`WithRetry`) 구현체 |
| `pkg/runtime/automaxprocs` | container CPU quota에 맞게 `GOMAXPROCS`를 설정하는 process bootstrap helper |
| `pkg/runtime/bootstrap` | 프로세스 시작 입력을 검증하고 runtime 구성을 적용하는 bootstrap helper |
| `pkg/runtime/httpserver` | HTTP server 시작·실행·graceful shutdown 소유권을 단일화하는 lifecycle helper |
| `pkg/runtime/lifecycle` | 기동·주기 실행·종료 정리와 다중 close를 조정하는 runtime lifecycle helper |
| `pkg/sqlutil` | embed FS의 SQL asset을 읽고 공백을 제거하며 누락·빈 query에서 panic하는 loader |
| `pkg/stringutil` | 범용 문자열 처리 유틸리티 |
| `pkg/telemetry` | OpenTelemetry 기반의 분산 트레이싱(Tracing) 정보 설정 및 컨텍스트 전파 유틸리티 |
| `pkg/workercontract` | strict worker profile 로드, diagnostics registry, 공통 Prometheus exposition 모듈 |
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

## Benchmark와 결정적 guard 검증

Go benchmark 함수는 명시적인 성능 조사와 프로파일링을 위해 유지합니다. 필요할 때 표준 Go 명령으로 직접 실행합니다.

```bash
go test -run '^$' -bench . ./pkg/promptguard ./pkg/outputguard
```

Blocking CI는 머신별 baseline이나 상대 wall-clock 비교를 사용하지 않습니다. Prompt/output guard의 의미론·적대적 입력·fail-closed 계약은 일반 테스트가 소유하며, 운영상 명시적으로 보호하던 prompt guard allocation 상한은 `testing.AllocsPerRun` 기반 결정적 테스트로 검증합니다. `ns/op`와 `B/op`는 수동 조사 지표이며 CI 합격 판정으로 사용하지 않습니다.

Prompt guard는 v3 rulepack과 source/enforcement를 명시하는 `Check`만 지원합니다.

## 로컬 검증 (Verification)

개발 시 로컬에서 아래 명령어로 검증할 수 있습니다.

```bash
make lint
go test ./...
go build ./...
```

**CI 정책:** 본 리포지토리는 원격 깃허브 액션(GitHub Actions)이 실제 검증 주체입니다. `ci.yml`은 PR 및 `main` push마다 workflow secret 경계 검사, SQL ownership 검사, `gofmt`, `go vet`, `golangci-lint`, 경합 조건 검사를 포함한 테스트 슈트(`go test -race -count=1 ./...`)를 수행하며, `security.yml`은 `main` push·주간 스케줄·수동 dispatch 시 `govulncheck` 취약점 분석을 수행합니다.
