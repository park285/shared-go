# shared-go 리팩토링 계획 (2026-06-02)

> cross-cutting 마스터: `iris-stack/docs/REFACTORING_PLAN_20260602.md`
> 범위: `pkg/{logging,httputil,runtime/*,json,jsonutil,telemetry,backoff,workerpool,workerconfig,stringutil,ginjson,envutil}` (~3.5K LOC, v1.5.0)
> 소비자: cbgk 69파일 + hololive-bot 295파일. **breaking change는 비싸므로 additive 우선.**

## 0. 요약

대체로 응집도 높은 공용 라이브러리입니다. `runtime/*`(lifecycle/httpserver/bootstrap/automaxprocs) 스켈레톤과 `backoff`/`envutil`/`jsonutil`/`json`은 깔끔합니다. 결함은 (1) 로깅 마스킹 정확성, (2) workerpool panic 안전성, (3) 응집도 일탈 2건(ginjson·workerconfig)입니다.

## 1. 검증으로 정정된 1차 주장

| 주장 | 결과 | 근거 |
|---|---|---|
| ginjson가 gin을 cbgk로 전이 비대화 | **정정** | cbgk `go.sum` gin 0건(module graph pruning). shared-go 응집도 냄새로만 강등. |
| httpserver `listenErrorPrefixServer.errCh` dead 필드 | **폐기** | nil-guard로 사용(`start_http_server.go:47`), 테스트 존재. |

## 2. Findings

### [P0] `SanitizeHandler`가 로그 `Message` 문자열 미마스킹 〔검증〕
- **증거**: `pkg/logging/sanitize.go:29` — `slog.NewRecord(..., record.Message, ...)` verbatim 복사. regex는 `record.Attrs()`에만.
- **누설 예**: `log.Error("... GET https://api?token="+tok)` → `?token=...`가 `querySecretRegex`(attr 전용)를 우회해 평문 기록.
- **수정**: `Handle`에서 `record.Message`에 `bearerTokenRegex`·`querySecretRegex` 적용 후 복사. 테스트 추가(message에 `Bearer`/`?token=`).
- **Risk/Effort**: 중간/Small. Breaking 없음.

### [P0] `QueuedPool.worker()` panic recover 부재 〔검증〕
- **증거**: `pkg/workerpool/queued_pool.go:148-157` — `for task := range p.queue { task() }`, recover 없음.
- **영향**: pooled task panic → 워커 goroutine 사망 → **프로세스 전체 크래시**. cbgk(webhookTaskPool/commandPool/drawPool), hololive-bot(dispatcher pools) 전부 노출. cbgk jamo DoS(마스터 P0-1)의 증폭 경로.
- **수정**:
  ```go
  for task := range p.queue {
      if task == nil { continue }
      safeRun(task)
  }
  func safeRun(task func()) { defer func(){ if r:=recover(); r!=nil { /* slog.Error */ } }(); task() }
  ```
  생성자에 optional `*slog.Logger` 주입해 recover 로깅(현재 pool은 logger 미보유).
- **Risk/Effort**: 낮음/Small. v1.5.x patch.

### [P1] `isSensitiveKey`가 bare `"key"` exact-match 〔검증〕
- **증거**: `pkg/logging/sanitize.go:88` `"key": true`. 영향 65개 비테스트 호출처(hololive 57 + cbgk 8): Valkey/Redis key name, lock key, template key, session index key 등 **비밀 아닌 식별자**가 `***REDACTED***`로 출력 → cache miss·lock 경합·prune 디버깅 정보 유실.
- **수정**: `"key"` 라인 삭제. `_api_key` suffix rule + `api_key`/`apikey` exact가 실제 키 커버. (이전에 가려지던 필드가 드러남 — 의도된 방향.)
- **Risk/Effort**: 낮음/Small(1줄).
- **상태(2026-07-03)**: 완료 — bare `key` query/field 특수 처리를 제거하고 `api_key`/`apikey` 마스킹 회귀 테스트를 유지.

### [P1] `pkg/workerconfig`는 generic lib 내 Iris 도메인
- **증거**: `worker_profile.go:19` `EnvProfileFile="IRIS_BOT_WEBHOOK_WORKER_PROFILE"`, `IrisBotWebhookWorkerProfile` 구조체, `workers.webhook.webhookPipeline` JSON 경로.
- **소비자**: hololive-shared `pkg/config`, cbgk `internal/config`(둘 다 `DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics`). `LoadIrisBotWebhookWorkerProfileFromEnv`는 downstream 미사용.
- **문제**: 도메인 schema 변경이 v1.5.0 전체 소비자에 영향. `hololive-shared`로 이동 시 cbgk→hololive 역의존 발생(불가); `iris-client-go`로 이동이 적절(둘 다 이미 import)하나 major bump.
- **현실안**: 당장은 "Iris-specific domain" 주석 + 차기 v2에서 `iris-client-go`로 이전.
- **상태(2026-07-03)**: 완료 — `worker_profile.go` 상단에 Iris worker-profile 도메인 계약 주석 추가.

### [P2] 기타
- `archive.Trigger()`가 `MoveAndPrune`(ReadDir/Rename/Remove)를 로그-write 핫패스에서 동기 실행 — `archive.go:27,60`. `running` 가드 내 goroutine화. (compress=true에서만 영향)
- `bootstrap.Run`이 `BuildTimeout==0`일 때 `context.WithTimeout(ctx,0)` → 즉시 만료 ctx — `bootstrap.go:55`. `>0` 가드 추가. (현 caller 모두 양수라 무영향, 미래 footgun)
- `workerconfig`가 `encoding/json` 사용(`DisallowUnknownFields` 필요로 의도적) — 주석 1줄로 명시. `worker_profile.go:6`.
- `StopAndWaitContext` ctx 우선 cancel 시 `workerWG.Wait()` goroutine 임시 잔존(worker 종료까지). 영구 누수 아님. `queued_pool.go:113`.

### [P3]
- `EnableFileLogging*`의 `slog.SetDefault` 전역 side effect — `logging.go:74,114`. AGENTS.md "no global side effects"와 상충. 호출자에게 위임.
- **상태(2026-07-03)**: 보류 — cross-repo logging wave에서 호출자 전환과 함께 처리.
- `stringutil.TrimSpace`/`ContainsString` = stdlib 1:1 래퍼 — `stringutil.go:17,22`. `//deprecated`.
- **상태(2026-07-03)**: closed-won't-do — 143 live uses(테스트 제외)라 Deprecated 표시는 소비자 lint noise만 만든다.
- `pkg/telemetry` **소비자 0**(hololive 6모듈·cbgk·iris-mcp-server 전수 grep) — otel sdk/otlptracegrpc/grpc 26개 indirect를 go.sum에 적재(binary pruning으로 컴파일은 안 됨). 활성 계획 없으면 별도 모듈 분리 또는 삭제. cbgk는 `internal/observability/tracing.go`로 동일 기능 자체 구현.
- **상태(2026-07-03)**: resolved-as-live — `twentyq-bot/internal/common/bootstrap/entrypoint.go:71`이 `telemetry.NewProvider`와 `telemetry.Config`를 사용.

## 3. Top refactors (ranked)
1. `SanitizeHandler.Handle` Message 마스킹(P0).
2. `QueuedPool` recover(P0) — cbgk DoS 방어심화.
3. `isSensitiveKey` `"key"` 제거(P1).
4. `bootstrap.BuildTimeout==0` 가드 + `archive.Trigger` 비동기화(P2).
5. `workerconfig` 주석/차기 이전 + `telemetry` 거취 결정.

## 4. Deep-read (opus 2차)
`QueuedPool` 동시성: RWMutex+`stopOnce`로 TrySubmit/SubmitWait vs StopAndWait race·double-Stop·send-to-closed 모두 안전(유일 결함은 recover 부재). `lifecycle.Run` graceful shutdown 순서(runtime cancel → 독립 shutdownCtx)는 정확. `OTelHandler→SanitizeHandler→tint` 체인 정확(WithAttrs까지 sanitize 전파).
