# 변경 이력

이 문서는 실제 Git tag를 기준으로 작성합니다. 기존 상세 기록은 모두 보존해 한국어로
옮겼고, 기록이 없던 릴리즈는 해당 tag 범위의 commit으로 보완했습니다.

## 미출시

## v1.36.1 - 2026-07-28

### 성능

- `httputil.LoginFailureRateLimiter`가 identity 상한에 도달한 뒤 새로운 identity마다 전체
  map을 순회하지 않도록 최소 만료 시각을 추적합니다. 만료 전 포화 요청은 `O(1)`로
  fail-closed 거부하고, 실제 만료 경계에서만 정리하여 기존 capacity 회수 의미를 유지합니다.

## v1.36.0 - 2026-07-28

### 보안

- `httputil.FixedWindowRateLimiter`가 configured window보다 짧은 entry TTL로 quota를 조기
  초기화하지 않도록 TTL을 window 이상으로 고정했습니다. LRU admission/eviction 의미는 유지하면서
  전체 map을 매 요청마다 순회하던 prune/eviction을 ordered bookkeeping으로 교체했습니다.
- `httputil.LoginFailureRateLimiter`에 기본 10,000개의 identity 상한을 추가했습니다. 새 identity는
  `IsAllowed`에서 원자적으로 slot을 예약하며, 직접 `RecordFailure`를 호출하는 경로도 같은 상한을
  적용합니다.
- strict `envutil` parse error에서 raw 환경 변수 값을 제거하고 syntax/range 분류만 보존했습니다.
  공용 logging과 runtime bootstrap은 bearer/query/userinfo/secret assignment가 포함된 진단 오류를
  stderr 또는 structured error attr에 쓰기 전에 마스킹합니다.

### 변경

- `healthprobe.RunMain`의 다중 target 검사를 하나의 4초 overall context 아래 병렬 실행하도록 바꿨습니다.
  기존 exported fetch/check 함수 signature와 target SSRF/redirect 검사는 유지됩니다.

## v1.35.0 - 2026-07-24

### 추가

- `pkg/httputil`에 범용 JSON 응답 writer `WriteJSON(w, status, v)`를 추가했습니다. HTML escape를
  적용하지 않으며(`SetEscapeHTML(false)`), 기존 `WriteErrorJSON`은 이 함수를 경유하도록 정리해
  응답 인코딩 로직을 단일 소스로 수렴했습니다. `WriteErrorJSON`의 출력·트림 동작은 동일합니다.
  commonization P1.3의 "WriteJSON 계열" 승격 잔여분으로, twentyq-bot이 첫 소비자입니다.

### 제거 (호환성 변경)

- 스택 소비자 0건인 exported 심볼을 lockstep 정책에 따라 정리했습니다. `pkg/json`의
  `MarshalEscapeHTML`(및 내부 `htmlEscapingAPI`), 그리고 sonic 오류 타입 별칭
  `SyntaxError`/`UnmarshalTypeError`를 제거했습니다. HTML escape 응답이 필요한 소비처는
  `pkg/httputil.WriteJSON`/`WriteErrorJSON`이 이미 `SetEscapeHTML(false)` 정책을 소유합니다.
- `pkg/llm/openaipreset`의 소비자 0건 `(*Client).RunInto`를 제거했습니다. 단일 프롬프트
  decode-into 경로는 `GenerateJSONInto`(layered prompt) 또는 `GenerateJSON`(text)로 대체됩니다.

## v1.34.0 - 2026-07-24

### 변경 (호환성)

- `pkg/db/pgxdb`의 `DefaultPoolConfig()`가 더 이상 `DB_POOL_MIN_CONNS`·`DB_POOL_MAX_CONNS`
  env를 읽거나 `[1,100]`·`[1,200]`로 clamp하지 않습니다. 정적 기본값
  `MinConns=0`(pgx 기본, 풀 최소 크기 없음), `MaxConns=20`, `ConnMaxLifetime=1h`,
  `ConnMaxIdleTime=30m`을 반환합니다. env 미설정 기준 구 기본값은 `MinConns=5`였으므로,
  `MinConns`를 지정하지 않던 호출자의 풀 최소 크기 기본이 5에서 0으로 바뀝니다.
  `OpenPool`·`OpenPoolWithRetry`의 fallback은 여전히
  이 단일 소스로 미설정 필드를 채우지만, 라이브러리는 풀 필드에 대해 env를 읽지 않고
  호출자가 준 값만 사용합니다. 풀 크기 env의 소유·검증·clamp는 소비자 책임으로 수렴했습니다. `MinConns` 기본이
  0이므로 소비자가 명시한 `MinConns=0`은 이제 기본값 대체 없이 그대로 pgx에 전달됩니다(이전에는
  env 재독+clamp로 1이 되어 operator 의도를 덮었음). in-stack 소비자(hololive-bot,
  chat-bot-go-kakao)는 항상 양수 `MinConns`/`MaxConns`를 넘기거나 `OpenPoolDSN` 경로를 쓰므로
  실질 동작 변화가 없고, twentyq-bot은 `MinConns=0` 전달이 이제 존중됩니다.

### 제거 (호환성 변경)

- v1.32.0에서 지원 중단을 예고한 소비자 0건의 `retry.ComputeBackoffDelay`를 제거했습니다.
  `backoff.ComputeExponentialBackoff`를 사용하십시오.

### 테스트

- 순수 이중 Base64로만 감싼 injection이 decode 예산 안에서 복원되어 모든 enforcement에서
  Block으로 거부되는 것을 회귀 검증합니다.
- `pgxdb.DefaultPoolConfig`가 `DB_POOL_*` env를 무시하고 정적 기본값을 반환하는 것과,
  `withPoolDefaults`가 명시 `MinConns=0`을 보존하는 것을 env 조합(unset/0/정상값/상한 초과)으로
  회귀 검증합니다.

## v1.33.0 - 2026-07-23

### 보안

- `promptguard`의 `decode_incomplete`(decode 예산 부족)는 더 이상 무조건 hard block하지
  않습니다. 예산 내 복호화된 표면에서 rule 매칭이 없으면 Review로만 표시되어, Interactive
  소비처는 통과하고 Persistent 소비처(세션 메모리 등)에서만 fail-closed로 거부됩니다. 어느
  한도가 걸렸는지 `Evaluation.DecodeLimits`와 `decode_incomplete:<limit>` rule 라벨로
  노출합니다.

## v1.32.6 - 2026-07-23

### 보안

- `golang.org/x/text`를 invalid input에서 infinite loop가 발생할 수 있는 `GO-2026-5970` 수정
  버전 `v0.39.0`으로 갱신했습니다.

### 수정

- `promptguard`가 표준 Base64·hex 치환 경계를 가로질러 완성되는 짧은 decode surface를 rule
  기여가 확인될 때만 expansion-only queue에 보존해, fully decoded rule literal이 치환 경계
  뒤에서만 나타나는 injection 우회를 차단합니다. 공개 API와 decode budget·fail-closed 계약은
  유지합니다.
- `promptguard`의 전역 decode candidate budget을 실제 rule 판정에 기여하는 후보만 소비하도록
  바꿔, benign Base64 context가 많은 prompt bundle이 무관한 후보 때문에 fail closed되던 오탐을
  제거했습니다. 악성 nested·encoded prompt 탐지와 decode-depth fail-closed 계약은 유지합니다.
- 대용량 표준 transform은 bounded window에서 평가하여 정상 입력 오탐을 줄이고, 영속 출력 제어와
  위조 역할·중단형 출력 공격 규칙을 보강했습니다. regex literal branch prefilter는 유지합니다.

### CI

- machine-local baseline과 상대 성능 비교에 의존하던 benchmark gate를 폐기했습니다. 빌드,
  lint, race test, vulnerability scan과 deterministic allocation 상한은 기존 저장소 gate에서
  계속 검증합니다.

## v1.32.5 - 2026-07-22

### 수정

- `promptguard`가 Cargo manifest처럼 Base64·hex 후보가 많은 정상 기술 설정을 검사할 때
  후보마다 전체 입력 문맥 예산을 중복 차감하여 `decode_incomplete`로 차단하던 문제를
  수정했습니다. rule literal 경계를 완성할 수 있는 후보만 owner 문맥 검사로 승격하며,
  평문과 Base64·hex 중간 조각을 합친 injection 우회는 계속 차단합니다.

### 테스트

- 실제 Cargo workspace manifest 정상 입력과 같은 입력 뒤의 encoded injection, Base64·hex로
  단어 중간만 숨긴 injection을 회귀 검증합니다.

## v1.32.3 - 2026-07-22

### 수정

- `outputguard`가 무해한 Base64 메타데이터와 HTTP URL path를 rule 기여 후보로 잘못
  계산해 `decode_incomplete`로 차단하던 문제를 수정했습니다. 후보·byte·depth·scan 한도와
  인코딩된 restricted rule 및 protected text의 fail-closed 검사는 유지됩니다.
- `promptguard`가 대형 Factorio blueprint와 확인된 binary data URI를 rule decode 예산에
  포함해 정상 prompt bundle을 `decode_incomplete`로 차단하던 문제를 수정했습니다. 읽을 수
  있는 payload와 미확인 binary는 보수적으로 검사하며, 중첩 Base64·hex 우회는 기존 depth·scan
  한도 안에서 계속 확장해 차단합니다.

### 테스트

- 반복된 citation 메타데이터, 대형 Factorio blueprint, binary data URI 정상 경로와 뒤쪽의
  인코딩된 restricted/protected text 공격, 미확인 zlib, 중첩 transform, 실제 decode budget
  소진 경로와 guard 성능 회귀를 검증합니다.

## v1.32.0 - 2026-07-20

### 추가

- `envutil`에 parse 실패를 호출자에게 반환하는 `IntE`, `Int64E`, `FloatE`, `BoolE`와
  first-non-empty 변형 `IntAnyE`, `Int64AnyE`, `BoolAnyE`를 추가했습니다.
- `healthprobe.RunMain`이 다중 URL, `--api-key-env`, `--body`, `--body-api-key-env` 경로를
  지원하여 운영 healthcheck wrapper가 공용 CLI를 그대로 사용할 수 있습니다.
- `runtime/httpserver.Run`이 server 시작, context 취소 대기, bounded graceful shutdown을
  단일 blocking lifecycle로 제공합니다.
- 소형 중복 회수를 위해 `lockutil.KeyedMutex`, `stringutil.HashForLog`·`TruncatedHash`·
  `TruncatedLogHash`, `sqlutil.MustQuery`, `retry.Sleep`, `pgxdb.IsDuplicateKey`를 추가했습니다.

### 변경 (호환성 변경)

- `healthprobe`의 URL 대상·redirect SSRF 검사를 `netguard.Policy`와
  `netguard.RedirectPolicy`로 일원화했습니다. 이에 따라 CGNAT `100.64.0.0/10`, benchmark
  `198.18.0.0/15`, reserved `240.0.0.0/4`, documentation, multicast 대역도 차단됩니다.
  기존 `healthprobe` error sentinel은 유지되며 underlying `netguard` sentinel에도
  `errors.Is`로 매칭됩니다.

### 지원 중단 예정

- `retry.ComputeBackoffDelay`는 이번 release에서 처음 지원 중단을 알리며,
  `backoff.ComputeExponentialBackoff`로 내부 계산을 직접 연결했습니다. 다음 minor에서 제거합니다.

### 제거 (호환성 변경)

- stack local HEAD 전체에서 소비자 0건을 확인한 `jsonutil.ExtractWithLimit`,
  `h3.ServerOptions`, `h3.NewServerWithOptions`, `h3.NewServerWithTLSConfigAndOptions`,
  `workerconfig.DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics`를 제거했습니다.
- 소비자 0건인 `httputil/ginauth` package와
  `outputguard.ReasonProtectedInputOversize`를 제거했습니다.

### 수정

- `pgxdb.OpenPoolWithRetry`가 PostgreSQL 재접속 지연에 half-jitter(절반~전체 범위 무작위화)를
  적용하여 여러 프로세스의 동시 재시도가 한 시점에 집중되지 않도록 했습니다.

### 테스트

- `logging`의 async drop summary 테스트가 stalled target으로 queue overflow를 결정적으로
  유발하여 scheduler timing에 따른 skip 없이 종료 요약을 검증합니다.

## v1.31.0 - 2026-07-17

### 제거 (호환성 변경)

- `netguard.Policy.AllowPrivateNetworks`를 제거했습니다. 스택 내 소비자가 없는 deprecated
  필드였으며, 허용할 대역은 `AllowedIPPrefixes`로 명시해야 합니다.
- `outputguard.CheckRequest.ProtectedTexts`를 제거했습니다. protected text 검사는
  `Guard.Bind`로 요청 단위 `BoundGuard`를 만들어 수행해야 합니다. 이에 따라
  `Guard.Check`의 호환 경로가 사라져 `ReasonProtectedInputOversize`는 더 이상 생산되지
  않는 상수로 남습니다(다음 minor 제거 후보). `ReasonProtectedInputInvalid`는 nil
  `BoundGuard.Check`의 fail-closed 경로에서 계속 생산됩니다.

- `httputil/ginauth`의 deprecated 별칭 `APIKeyAuthMiddleware`/`NoRouteAuthHandler`를
  제거했습니다. `AuthMiddleware(AuthConfig{...})` / `NoRouteHandler(AuthConfig{...})`를
  사용하십시오. 스택 내 소비자 0건을 재확인했습니다.

### 지원 중단 예정 (다음 minor에서 제거)

- `jsonutil.ExtractWithLimit` — `Extract`를 사용하십시오.
- `h3.NewServerWithOptions` / `h3.NewServerWithTLSConfigAndOptions` — `NewServer` /
  `NewServerWithTLSConfig`를 사용하십시오.
- `workerconfig.DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics` —
  `DecodeRuntimeWorkerProfileEnvelope`를 사용하십시오.

### 추가

- `outputguard`가 지원하는 Base64·hex 조각을 decode할 때 주변 평문을 보존하며,
  protected text 비교에는 제한된 짧은 조각 확장을 적용합니다.
- `promptguard`가 Base64·hex 조각을 주변 평문에 다시 삽입한 surface를 검사하여,
  injection rule을 평문과 인코딩 조각으로 나눈 우회를 차단합니다.
- `workerpool.ManagedPool`이 reservation, concurrency, timeout, panic, 지연 완료와 종료 상태를
  진단할 수 있는 bounded finalizer scheduler를 소유합니다.
- `workerpool.ManagedPool.TrySubmitResult`가 admission과 finalizer ownership을 반환하여
  거부된 durable work를 호출자가 정확히 한 번 복구할 수 있습니다.

### 변경 (호환성 변경)

- `workerpool.ManagedPool.TrySubmit`은 거부된 job의 `Finalize` callback ownership을 호출자에게
  남깁니다. 거부된 finalization도 pool에 위임하려면 `TrySubmitResult`를 사용해야 합니다.

## v1.30.0 - 2026-07-13

### 수정

- `workerpool.ManagedPool`이 queue 가용성 대기에 condition variable을 사용합니다. burst가
  들어오면 설정된 worker들이 모두 깨어나며, capacity-one notification의 interleaving 때문에
  실행이 worker 하나로 축소되지 않습니다.
- queue 만료와 종료 처리가 admission 직후 모든 job에 적용됩니다. stale reaper로부터 job을
  숨기거나 pool 종료를 지연할 수 있었던 admission callback 상태를 제거했습니다.

### 제거 (호환성 변경)

- `workerpool.JobSpec.OnAdmitted`를 제거했습니다. admission은 메모리 내부의 ownership 결정으로
  유지해야 합니다. 진행 답변과 기타 외부 I/O는 dequeue 시점의 job context, timeout cause,
  expiry, finalizer, shutdown 계약이 적용되는 `Run` 시작부에서 수행해야 합니다.

## v1.29.0 - 2026-07-12

### 추가

- managed worker runtime과 strict worker-profile envelope를 추가했습니다.
- prompt injection 강제 경계를 v3로 강화하고 LLM instruction purpose layer와 provider profile을
  추가했습니다.
- `dbmigrate` manifest 검증, advisory-lock 격리, migration session timeout을 강화했습니다.

### 수정

- OpenAI Responses JSON tool envelope 판별과 안전한 진단, `httputil` JSON 단일 값·크기 상한,
  제한된 `umask`에서의 log 권한 계약을 보강했습니다.

## v1.28.4 - 2026-07-07

### 변경

- `dbmigrate` ledger API가 parameterized execution을 요구하며 SQL을 embedded asset으로
  이동했습니다.

## v1.28.3 - 2026-07-05

### 변경

- `netguard`의 `staticcheck` 예외를 로컬·원격 검사 도구 양쪽에 맞췄습니다.

## v1.28.2 - 2026-07-05

### 변경

- `netguard.DialTLS`의 `staticcheck` 예외 형식을 정리했습니다.

## v1.28.1 - 2026-07-05

### 변경

- `grpc`를 `v1.82.0`으로 갱신하고 CI action과 `govulncheck` 버전을 고정했습니다.
- benchmark gate가 Git worktree에서도 repository 이름을 올바르게 판별하도록 했습니다.

## v1.28.0 - 2026-07-04

### 추가

- `dbmigrate` advisory lock과 ledger, `netguard` SSRF guard, `httputil` JSON request decoder를
  추가했습니다.

### 수정

- `netguard.GuardedTransport`에서 deprecated `DialTLS` 처리를 제거했습니다.

## v1.27.0 - 2026-07-04

### 추가

- `httputil`: bot의 `X-API-Key`/Bearer fail-closed 의미와 일치하는 공용 admin API key 인증
  helper(`HeaderAPIKey`, `AdminAuthConfig`, `AdminAuthMiddleware`, `APIKeyFromRequest`,
  `ConstantTimeStringEqual`, `WriteErrorJSON`)를 추가했습니다. `AdminAuthConfig`는 명시적인
  `Disabled` opt-out을 사용하므로 zero value에서도 인증을 강제합니다.
- `httputil/ginauth`: base `httputil` package에 gin을 연결하지 않고 hololive 방식의 API key
  middleware와 인증된 `NoRoute` 처리를 제공하는 gin adapter를 추가했습니다.
- `httputil`: twentyq와 admin-dashboard의 forwarded-header 계약을 포괄하는 fixed-window rate
  limiting, login-failure lockout limiting, API-key/IP rate-limit identity, trusted-proxy client IP
  helper를 추가했습니다.
- `dbmigrate`: `fs.FS` 입력, `Execer` function seam, `SQLExec` adapter, bot-local migration
  wrapper용 `WithOnly` filtering을 지원하는 `manifest.txt` runner core를 추가했습니다.
- `envutil`: third-party dotenv dependency 없이 service-prefix OpenBao 경로
  (`/run/<svc>/<svc>.env`)와 opt-in local dotenv file을 읽는 helper를 추가했습니다.
- `healthprobe`: `RunMain`을 문서화된 healthcheck command entry point로 정하고, 내부 URL-check
  seam을 통해 exit-code 동작을 고정하는 test를 추가했습니다.
- `llm/openaipreset`: message-list request용 non-streaming OpenAI Responses completion core와,
  streaming 또는 image 경로를 자체 유지하는 stack-local client용 공용 response text/usage
  parsing hook을 추가했습니다.
- `docs/adoption/shared-go-v1.27.0-admin-surface.md`: 다음 consumer migration guide를
  추가했습니다.

### 제거 (호환성 변경)

- `logging/archive`: `pkg/logging/internal/archive` 아래로 internalize했습니다.
  `pkg/logging`을 통한 file logging과 log archive/prune 동작은 바뀌지 않습니다.
- `workerconfig`: 사용처가 없는 Iris worker-profile detail type
  (`BotPoolWorkerProfile`, `BotWebhookReceiveWorkerProfile`, `IrisWebhookDeliveryWorkerProfile`,
  `IrisBotWebhookWorkerProfileValidation`)과 `DefaultIrisBotWebhookWorkerProfile` helper를
  비공개로 전환했습니다.
- `json`: 사용처가 없는 stdlib compatibility re-export `Decoder`, `Number`를 제거했습니다.
  `NewDecoder` 또는 이 함수가 반환하는 concrete decoder를 사용해야 합니다.
- `logging`: 사용처가 없는 handler·plumbing helper(`SanitizeHandler`, `OTelHandler`,
  `NewSanitizeHandler`, `NewOTelHandler`, `Component`, `JobID`, `NewID`, `ParseLevel`,
  `*FromContext`)를 비공개로 전환했습니다. public logger 생성, operation logging, context
  enrichment, file logging entry point는 유지됩니다.

### 변경

- `logging` (호환성 변경): `EnableFileLogging*`이 더 이상 `slog.SetDefault`를 호출하지
  않습니다. process default logger가 필요한 호출자는 반환된 logger로 직접
  `slog.SetDefault(logger)`를 호출해야 합니다.
- `logging`: literal `key` field 또는 `?key=` query parameter라는 이름만으로는 redaction하지
  않습니다. `api_key`, `apikey`, token/password/secret 변형과 suffix 규칙은 계속
  redaction합니다.
- `httputil`: zero-value `FixedWindowOptions`에 pruning 기본값
  (`MaxIdentities=10000`, `EntryTTL=2m`)을 적용합니다. 오른쪽 forwarded-header parsing은 plain
  IP hop만 허용하며, `LoginFailureRateLimiter.Stop`은 `Start` 전이나 반복 호출에도 안전합니다.

### 문서

- package `doc.go`에서 생성된 public-surface와 internal-helper 목록을 제거하고 package 개요와
  사용 예시는 유지했습니다.
- `REFACTORING_PLAN_20260602.md`에 이 작업의 완료된 P1/P3 결정을 기록했습니다.

## v1.26.1 - 2026-07-03

### 수정

- log archiver가 `Close` 뒤 `Trigger`를 거부하여 log directory 정리와의 경합을
  차단했습니다.

## v1.26.0 - 2026-07-03

### 제거 (호환성 변경)

- `retry`: stack 전체에 호출자가 없던 declaration-only API `DefaultRetryOptions`를
  제거했습니다. 대신 `RetryOptions` literal을 생성해야 합니다.
- `workerconfig`: `DecodeIrisBotWebhookWorkerProfile`을 제거했습니다. consumer는 유일한
  entry point로 유지되는 `DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics`를 통해
  decode해야 합니다.
- `envutil`: `SecretFile`을 비공개 `secretFile`로 전환했습니다. 외부 consumer는
  `StringOrSecretFile` 또는 `FirstStringOrSecretFile`을 통해서만 secret file을 읽습니다.

## v1.25.0 - 2026-07-03

이 항목은 tag가 changelog 없이 배포된 뒤 2026-07-03에 보완했습니다.

### 추가

- `obsmetrics`: label metric vector(`Labels`, `CounterVec` / `GaugeVec` / `HistogramVec`와
  `NewCounterVec` / `NewGaugeVec` / `NewHistogramVec`) 및 label exposition writer
  (`WriteCounterWithLabels` / `WriteGaugeWithLabels` / `WriteHistogramWithLabels`)를
  추가했습니다.
- `h3`: `ServerOptions`와 `NewServerWithOptions` / `NewServerWithTLSConfigAndOptions`를
  추가했습니다.

## v1.24.2 - 2026-07-02

### 수정

- `runtime/lifecycle`: `RunCloseSteps`가 개별 close step의 panic을 recover하고 error로
  변환합니다. panic 값이 error이면 `errors.Is` identity를 보존하고 `errors.Join`으로
  집계한 뒤 나머지 step도 계속 실행합니다. 이전에는 panic 하나가 전체 shutdown을
  중단하여 뒤의 resource-cleanup step을 모두 건너뛰었습니다.

### 변경

- `llm/openaipreset`: `WithReasoningEffort`가 raw input 대신 whitespace를 trim한 값을
  저장합니다. twentyq consumer가 전달 전에 적용하던 정규화를 보존하며, blank 또는
  whitespace-only 입력을 무시하는 동작은 유지됩니다.
- `.gitguardian.yaml`: 모든 `**/*_test.go`를 제외하던 secret-scan 규칙을 synthetic secret이
  필요한 세 package(`pkg/logging`, `pkg/outputguard`, `pkg/envutil`)로 좁혔습니다. `pgxdb`
  test fixture는 `ab42ac6`에서 placeholder로 바뀌어 path 제외가 필요하지 않으며, 그 밖의
  `*_test.go` 파일은 다시 검사합니다.

### 문서

- `runtime/lifecycle`: `RunCloseSteps`의 비명시적 계약을 설명하는 `doc.go`를 추가했습니다.
  순서는 slice 순서이며 reverse가 아니고, 실패 뒤에도 모든 step을 실행하며, 이미 취소된
  context에서도 step을 실행하고, `errors.Join`으로 집계하며 panic을 error로 변환합니다.
- `retry`: context error 우선순위 계약을 문서화했습니다. context 취소와 이전 `fn` error가
  함께 존재하면 `WithRetry`는 마지막 `fn`의 operational error를 반환합니다. 이전 `fn`
  error가 없을 때만 `context error: <ctx.Err()>`로 wrap한 error를 반환합니다. 동작은
  바뀌지 않았습니다.
- `db/pgxdb`: `DNSFallback=true`가 설정 host가 대소문자 구분 없이 정확히 `postgres`이고,
  connect error가 그 host의 DNS `no such host`일 때만 localhost fallback을 실행한다고
  문서화했습니다.

## v1.24.1 - 2026-07-02

### 수정

- `db/pgxdb`: `OpenPoolWithRetry`가 permanent failure를 재시도하지 않습니다. retry loop 전에
  `cfg.Validate()`와 pool connection-count range를 검증하여 `sslmode` 오타 같은 config
  error는 즉시 반환합니다. loop 안에서는 authentication failure(`pgconn.PgError` SQLSTATE
  `28000`/`28P01`)와 parent-context cancellation/deadline을 permanent로 분류해 즉시
  반환합니다. parent context가 유효한 동안의 database-not-found(`3D000`), connection
  refused, DNS failure, ping timeout 같은 복구 가능한 startup race는 계속 재시도합니다.
  이전 기본 `RetryConfig`는 permanent error도 약 30초 동안 재시도했습니다.
- `db/pgxdb`: pool 기본 fallback의 출처를 하나로 통합했습니다. `withPoolDefaults`는 env로
  조정 가능한 `DefaultPoolConfig()`의 `MinConns`/`MaxConns`/`ConnMaxLifetime`/
  `ConnMaxIdleTime`을 사용하며 기본값은 5/20입니다. env를 무시하던 별도 hardcoded 2/10은
  제거했고 `ConnMaxLifetimeJitter`는 계속 `ConnMaxLifetime/5`로 계산합니다.
  `OpenPool(Options{})`와 `DefaultOptions()`는 이제 같은 pool configuration을 만듭니다.
  `OpenPoolDSN`은 DSN에 명시된 `pool_*` parameter를 덮어쓰지 않고 미설정 parameter를 pgx
  기본값에 맡기는 overlay 의미를 유지합니다. shared-go 기본값을 적용하려면
  `opts.Pool`에 `DefaultPoolConfig()` 등을 전달해야 합니다. 세 entry point의 계약은
  `db/pgxdb/doc.go`에 문서화했습니다.

## v1.24.0 - 2026-07-02

### 추가

- `pkg/runtime/lifecycle`에 순서가 보장되는 종료 helper `CloseStep`과 `RunCloseSteps`를
  추가했습니다.

## v1.23.0 - 2026-07-02

### 추가

- `pkg/db/pgxdb`와 `pkg/db/sqldb`를 추가하여 pgxpool과 `database/sql` pool 생성을
  공용화하고 `sslmode` 명시를 강제했습니다.

## v1.22.0 - 2026-07-02

### 추가

- `pkg/llm/openaipreset`에 OpenAI-compatible functional-options preset인 `GenerateJSON`과
  `RunInto`를 추가했습니다.

## v1.21.0 - 2026-07-02

### 추가

- hololive의 공용 구현을 승격한 `pkg/retry.WithRetry` 재시도 loop를 추가했습니다.

## v1.20.0 - 2026-07-02

### 제거

- stack 전체에서 consumer가 없는 dead code를 제거했습니다.

## v1.19.0 - 2026-06-26

### 변경

- `healthprobe.FetchURLWithOptions`를 secure-by-default로 전환하고 private network 접근은
  `FetchOptions.AllowPrivateNetworks`로 명시하게 했습니다.

## v1.18.0 - 2026-06-23

### 변경

- `healthprobe` SSRF 방어를 secure-by-default로 전환하고 JSON decoder에 hard cap을
  적용했습니다.

## v1.17.2 - 2026-06-22

### 추가

- `envutil` strict secret-file API와 `obsmetrics` webhook decode-latency histogram을
  추가했습니다.

## v1.17.1 - 2026-06-22

### 변경

- `golang.org/x` indirect dependency를 갱신하고 lint gate를 정리했습니다.

## v1.17.0 - 2026-06-22

### 추가

- `healthprobe` SSRF dial guard와 `h3.DialGuard` option을 추가했습니다.

## v1.16.0 - 2026-06-22

### 변경

- dependency minor version을 갱신했습니다.

## v1.15.0 - 2026-06-21

### 변경

- secret, network, logging 경계를 중심으로 보안을 강화했습니다.

## v1.14.0 - 2026-06-20

### 수정

- log archiver goroutine을 closer에 연결하여 종료 시 drain되도록 했습니다.

## v1.13.0 - 2026-06-20

### 변경

- `check-workflow-secrets.sh`를 stack 정본과 동기화했습니다.

## v1.12.0 - 2026-06-17

### 테스트

- `healthprobe.RunMain`의 URL check 분기와 exit-code 동작을 보강했습니다.

## v1.11.0 - 2026-06-17

### 추가

- `client_golang`에 의존하지 않는 공용 webhook metric kit `obsmetrics`를 추가했습니다.

## v1.10.0 - 2026-06-14

### 추가

- 공용 prompt/output guard package를 추가했습니다.

## v1.9.0 - 2026-06-11

### 변경

- 성능, I/O, 신뢰성, 보안에 걸친 NFR 18건을 정비했습니다.

## v1.8.0 - 2026-06-10

### 제거 (호환성 변경)

dead public API를 제거했습니다. 제거한 모든 symbol은 2026-06-10 T3 계획의 실측 결과에
따라 stack consumer인 hololive-bot과 chat-bot-go-kakao 전체에서 package-qualified 검색으로
호출자가 없음을 확인했습니다. 이 module은 외부 consumer가 없는 iris-stack 내부 module이므로,
API 안정성 정책의 의도인 consumer 영향 없음이 보존된다는 근거로 minor release에서
제거했습니다.

- `httputil`: `timeout_preset.go` 전체(`TimeoutPreset`, `FetchTimeout`, `LongPollTimeout`,
  `ScraperTimeout`, `Duration`, `NewClientWithPreset`, `NewExternalAPIClientWithPreset`,
  `NewInternalServiceClientWithPreset`)를 제거했습니다.
- `httputil`: `DefaultClient`를 제거했습니다. `NewProfiledClient`, `NewExternalAPIClient`,
  `NewInternalServiceClient`를 사용해야 합니다.
- `httputil`: `AsAPIError`를 제거했습니다. `*APIError`에 `errors.As`를 사용하거나 unwrap을
  내부화한 `IsStatus`를 사용해야 합니다.
- `runtime/httpserver`: concrete `StartHTTPServer` / `ShutdownHTTPServer`를 제거했습니다.
  generic `Start` / `Shutdown` / `StartServerWithPrefix` API는 유지됩니다.
- `healthprobe`: `ParseURL`을 내부 `parseURL`로 전환했습니다. `CheckURL` 또는 `FetchURL`을
  사용해야 합니다.
- `stringutil`: `StripLeadingHeader`를 제거했습니다.

`pkg/telemetry`는 AGENTS.md의 재사용 가능 helper로 분류되어 향후 OTel rollout을 위해
의도적으로 유지했습니다.

### 추가

- `envutil.StringOrFile`: `$KEY`를 읽고, 없으면 `$KEY_FILE`이 가리키는 파일 내용
  (OpenBao secret-mount pattern), 그다음 기본값을 사용합니다.
- `envutil.List` / `envutil.ListWithFallback`: comma 또는 whitespace로 구분된 list를 trim하고
  중복 제거하며 `StringOrFile`을 통해 읽습니다.
- `envutil.Map`: comma/newline/tab으로 구분된 `k:v` / `k=v` entry를 parsing하며
  `StringOrFile`을 통해 읽습니다.
- `envutil.Bool`: canonical 3-way truth set을 지원합니다. `{1, true, yes, y, on}`은 true,
  `{0, false, no, n, off}`는 false, 인식하지 못한 값은 기본값이 됩니다. 이전에는 2-way
  set이라 인식하지 못한 값이 false로 축소됐습니다. 기존 true-set 동작과 `BoolStrict`는
  유지됩니다.

## v1.7.1 - 2026-06-08

### 변경

- dependency를 갱신하고 pull request secret-boundary 검증을 추가했으며 중복 build 단계를
  제거했습니다.

## v1.7.0 - 2026-06-06

### 추가

- `logging.RunOperation`에 started/succeeded log level을 제어하는 `Level` option을
  추가했습니다. zero value는 `Info`를 유지합니다.

## v1.6.0 - 2026-06-05

### 추가

- `logging.EnableFileLoggingWithOptions`와 async stdout lane option, `Closer` 반환을
  추가했습니다.

## v1.5.2 - 2026-06-03

### 변경

- toolchain 하한을 `go1.26.4`로 명시했습니다.

## v1.5.1 - 2026-06-03

### 수정

- `QueuedPool`의 panic recovery와 log `Message` masking을 보강하고 lint 경로·module 설정을
  교정했습니다.

### 변경

- Go 1.26.x patch를 자동 추종하도록 toolchain 고정을 제거했습니다.

## v1.5.0 - 2026-05-26

### 추가

- `workerconfig.BotPoolWorkerProfile`을 추가했습니다.

## v1.4.0 - 2026-05-25

### 추가

- Iris-Bot worker-profile 계약을 추가했습니다.

## v1.3.0 - 2026-05-25

### 추가

- `QueuedPool`을 추가하고 ants 기반 pool을 제거했습니다.

## v1.2.0 - 2026-05-25

### 변경

- `logging/archive` subpackage를 분리하고 `runtime/loop`를 `lifecycle`로 이동했습니다.
- `httputil` timeout preset을 추가했습니다.

## v1.1.0 - 2026-05-25

### 변경

- 전체 dependency를 당시 최신 version으로 갱신했습니다.

## v1.0.0 - 2026-05-25

### 추가

- iris-stack 공용 Go utility를 독립 `shared-go` repository로 초기화했습니다.
