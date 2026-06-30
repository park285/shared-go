# shared-go

Iris Stack의 Go 프로젝트들(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot` 등)이 공통으로 사용하는 유틸리티 라이브러리 전용 Go 모듈입니다.

본 라이브러리는 `cmd/` 실행 진입점이 없는 순수 라이브러리(Library-only) 모듈로 설계되었으며, 이를 호출하는 서비스들을 위해 일관되고 안정적인 API 사양을 유지합니다.

## 설치 (Installation)

```bash
go get github.com/park285/shared-go@latest
```

## 제공 패키지 목록 (Package Catalog)

| 패키지 경로 | 기능 및 역할 |
|---|---|
| `pkg/backoff` | 시도 횟수 및 상태를 기반으로 한 지수 백오프(Exponential Backoff) 계산 유틸리티 (대기 및 재시도 루프 제어는 호출부에서 직접 처리) |
| `pkg/envutil` | 환경 변수 로드 및 `*_FILE` 형태의 파일 경로를 통한 보안 토큰/시크릿 값 주입 도구 |
| `pkg/ginjson` | Gin 웹 프레임워크를 위한 Sonic 라이브러리 기반 고성능 JSON 렌더링 및 바인딩 모듈 |
| `pkg/h3` | HTTP/3 전송 프로토콜 설정 도구 (자체 CA 번들 등록, TLS 상세 사양 정의) |
| `pkg/healthprobe` | 서비스 헬스체크 및 프로브(Readiness / Liveness Probe) 도구 |
| `pkg/httputil` | HTTP 클라이언트 커넥션 풀링 및 프로파일 구성 도구 |
| `pkg/json` | Sonic 엔진을 내장한 고성능 JSON 인코딩/디코딩 추상화 계층 |
| `pkg/jsonutil` | 텍스트 혹은 HTTP 응답 문자열로부터 유효한 JSON을 정규화하여 추출하는 헬퍼 유틸리티 |
| `pkg/llm` | LLM provider 클라이언트 추상화 (`JSONGenerator` 인터페이스, OpenAI Responses 호환 JSON 생성, Codex CLI 실행/로그인, 응답 redaction) |
| `pkg/logging` | Slog 기반의 구조화된 로깅 모듈 (비동기 처리, 민감한 키 정보 마스킹 및 실시간 로그 로테이션 지원) |
| `pkg/obsmetrics` | `client_golang` 의존성 없이 Prometheus 평문 텍스트 exposition을 생성하는 메트릭 키트 (webhook/런타임 메트릭, prefix 네임스페이스 분리) |
| `pkg/outputguard` | LLM 생성 출력 가드 (시스템 프롬프트·시크릿 누출 패턴 차단, confusable 문자 정규화) |
| `pkg/promptguard` | 룰팩 기반 프롬프트 인젝션 방어 가드 (가중치 임계값 매칭, dampen/block 정책, base64/confusable 디코딩, 한/영 룰팩, TTL 캐시) |
| `pkg/runtime` | Go 런타임 최적화를 포함한 프로세스 부트스트랩 도구 (`automaxprocs`, 애플리케이션 라이프사이클 관리, HTTPServer) |
| `pkg/stringutil` | 범용 문자열 처리 유틸리티 |
| `pkg/telemetry` | OpenTelemetry 기반의 분산 트레이싱(Tracing) 정보 설정 및 컨텍스트 전파 유틸리티 |
| `pkg/workerconfig` | 개별 백그라운드 워커들의 동작 프로파일 설정 로드 모듈 |
| `pkg/workerpool` | 큐(Queue) 기반의 동시성 제어 워커 풀 구현체 |

새로운 공통 기능이 필요할 경우, `pkg/` 하위에 신규 패키지 형식으로 추가해 주십시오.

## 로컬 검증 (Verification)

개발 시 로컬에서 아래 명령어를 수행하여 검증을 진행할 수 있습니다.

```bash
make lint
go test ./...
go build ./...
```

**CI 정책:** 본 리포지토리의 원격 깃허브 액션(GitHub Actions)은 빠른 구문 검사(`ci.yml`) 및 보안 검사(`security.yml`)만을 수행합니다. 실제 테스트 슈트 실행 및 경합 조건(Race Condition) 검사, 전체 의존성 분석은 로컬 push 전 단계에서 완벽하게 완료하는 것을 원칙으로 합니다.
