# shared-go

iris-stack Go 프로젝트(`hololive-bot`, `chat-bot-go-kakao` 등)가 공유하는 라이브러리 전용 Go 모듈입니다.
`cmd/` 없는 library-only 모듈이며, downstream 소비자를 위한 API 안정성을 유지합니다.

## 설치

```bash
go get github.com/park285/shared-go@latest
```

## 패키지

| 패키지 | 역할 |
|---|---|
| `pkg/backoff` | attempt/상태 기반 exponential backoff 계산 helper (sleep·retry loop는 호출부 책임) |
| `pkg/envutil` | 환경변수 로딩과 `*_FILE` 방식 secret 파일 주입 |
| `pkg/ginjson` | Gin용 sonic 기반 JSON 렌더링/바인딩 |
| `pkg/h3` | HTTP/3 클라이언트 transport 구성 (CA bundle, TLS) |
| `pkg/healthprobe` | HTTP 헬스 프로브 helper |
| `pkg/httputil` | HTTP client 풀·프로파일 구성 |
| `pkg/json` | sonic 기반 JSON 인코딩/디코딩 façade |
| `pkg/jsonutil` | 모델/HTTP 응답 텍스트에서의 JSON 추출 helper |
| `pkg/logging` | slog 기반 로깅 (비동기 writer, 민감값 sanitize, archive) |
| `pkg/runtime` | 프로세스 부트스트랩 (`automaxprocs`·`bootstrap`·`httpserver`·`lifecycle`) |
| `pkg/stringutil` | 문자열 유틸 |
| `pkg/telemetry` | OpenTelemetry tracing 설정·context 전파 |
| `pkg/workerconfig` | worker profile 설정 로딩 |
| `pkg/workerpool` | queue 기반 worker pool |

새 패키지는 `pkg/` 아래에 추가합니다.

## 검증

```bash
make lint
go test ./...
go build ./...
```

GitHub CI는 fast gate(`ci.yml`)와 비-PR 보안 스캔(`security.yml`)만 수행하고, 전체 테스트·race·의존성 검증은 push 전에 로컬에서 실행하는 것이 이 repo의 CI 정책입니다.
