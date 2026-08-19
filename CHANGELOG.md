# 변경 이력

이 문서는 실제 Git tag를 기준으로 작성합니다. 기존 상세 기록은 모두 보존해 한국어로
옮겼고, 기록이 없던 릴리즈는 해당 tag 범위의 commit으로 보완했습니다.

## 미출시

### 호환성이 깨지는 변경

- `workerpool.ManagedPool.TrySubmit`을 제거하고 admission 결과와 rejection finalizer ownership을
  함께 반환하는 `TrySubmitResult`로 단일화했습니다. 스택 내 소비자는 모두
  `Accepted`와 `FinalizerClaimed`를 확인하도록 이관했습니다.

## v1.51.0 - 2026-08-15

### 추가

- `kakaoformat`: Markdown을 카카오 일반챗 화면에 맞는 유니코드 평문으로 렌더하고, 오픈채팅 여부를 `IsOpenChat`으로 판별합니다.

### 수정

- `llm`: Chat Completions fallback 트리거를 미지원 엔드포인트로 제한합니다.
- `lifecycle`: runtime 오류를 `Run` 반환값에 보존합니다.
- `lockutil`: shard 배열 하한을 compile-time에 검증합니다.
- `h3`: `DialGuard` DNS 해석을 request context에 연결합니다.
- `retry`: invalid delay와 취소를 다음 attempt 전에 거부합니다.
- `logging`: `RunOperation` 로그에 실제 호출자 source를 기록합니다.
- `backoff`: 최초 지연 상한과 duration overflow를 보정합니다.

### 기타

- Go 툴체인 핀을 `1.26.6`으로 올립니다.

## v1.50.0 - 2026-08-12

### 호환성이 깨지는 변경

- `envutil`: `ServiceName` dotenv 로딩의 strict 스위치가 `<PREFIX>_REQUIRE_OPENBAO`에서
  `<PREFIX>_REQUIRE_STATIC_SECRETS`로 바뀝니다. OpenBao는 2026-08-08에 퇴역했고 구 이름은
  더 이상 조회하지 않으므로, 그 이름만 설정한 배포는 strict 모드가 꺼진 채로 동작합니다.
  현행 이름으로 옮겨야 합니다.

## v1.49.0 - 2026-08-11

### 호환성이 깨지는 변경

- `logging`: JSON 출력의 `source`가 3-attr group에서 `"file:line"` 문자열로 평탄화되고
  `function` 필드가 사라집니다. `AddSource` formatter가 record마다 `*slog.Source`를 두 번
  할당하고 slog이 그것을 group으로 전개하며 다시 할당하던 경로를 제거한 결과입니다.
  `source.file`·`source.function`·`source.line`을 참조하는 로그 질의는 갱신해야 합니다.
  PC 0 record가 낳는 빈 `Source`는 빈 `Attr`로 남겨 slog이 통째로 생략하도록 유지합니다.

### 성능

- `logging`: hot path 할당을 제거합니다 — context lookup, attr 구성, error helper의 attr 병합
  slice, slog source 값 재사용, sanitizer group의 copy-on-write 전환.

### 수정

- `logging`: caller가 소유한 `source` 값을 formatter가 덮어쓰지 않도록 보존합니다.

### 기타

- Go 1.26 test rewrites를 적용합니다.

## v1.48.2 - 2026-08-09

### 수정

- `llm`: 다중 cache breakpoint를 instruction segment별로 보존합니다.

## v1.48.1 - 2026-08-08

### 수정

- `llm`: instruction profile 변환에서 cache breakpoint를 보존합니다.

## v1.48.0 - 2026-08-07

### 추가

- `llm`: `prompt_cache_key`를 배선하고 assistant cache breakpoint를 평문으로 렌더합니다.

## v1.47.0 - 2026-08-07

### 호환성이 깨지는 변경

- 스택 내 소비자가 0인 exported API를 제거합니다. README 호환성 정책(보장 대상은
  `hololive-bot`·`chat-bot-go-kakao`·`twentyq-bot` 세 소비자로 한정)에 따라 major 승격 없이
  제거하며, v1.42.0에서 예고한 `envutil` 다중-키 폴백 계열의 예고된 제거입니다.
  - `jsonutil.ReadAllLimit` — `httputil.ReadAllLimited`를 직접 사용하십시오. 별칭
    `jsonutil.ErrBodyTooLarge`는 소비자가 있으므로 유지합니다.
  - `stringutil.HashForLog` — `TruncatedLogHash`를 사용하십시오.
  - `envutil.IntAnyE`·`Int64AnyE`·`BoolAnyE` — 단일 키는 `IntE`/`Int64E`/`BoolE`를 쓰고,
    다중 키 폴백이 필요하면 `StringAny`로 키를 고른 뒤 파싱하십시오.
  - `envutil.ListWithFallback` — `List`를 쓰고 빈 결과의 기본값은 호출부에서 처리하십시오.
  - `envutil.FirstStringOrSecretFile` — 키별로 `StringOrSecretFile`을 호출하고 폴백 순서는
    호출부에서 정하십시오.

## v1.46.2 - 2026-08-07

### 수정

- `guardtext`: 공백 결합 판독이 원본 판독을 대체하던 회귀를 고칩니다. 결합본은 두 번째
  판독으로 더해지며, 결합 대상은 4자 그룹 정렬이 깨져 어느 쪽도 단독 해독되지 않는 분할로
  한정합니다. 정상 토큰 경계에서 합치면 2조각 조합 탐지를 잃습니다.

## v1.46.1 - 2026-08-06

### 수정

- `guardtext`: 공백 하나로 분할된 Base64를 rule decoding 경로로 보내 중첩 short base64의
  미탐색 층 fail-open을 봉합합니다.
- `telemetry`: traced/filtered 두 wrapper 경로 모두에서 route pattern을 전파합니다.

### 성능

- `promptguard`: witness 최소화에 64KiB size-gate를 추가합니다.

## v1.46.0 - 2026-08-06

### 추가

- `telemetry`: `HTTPHandlerOptions.SpanRoutePattern`(opt-in)을 도입합니다 — 켜면 server
  span 이름이 `<operation> <route pattern>`이 되고 `http.route` attribute가 붙습니다.
  route 패턴은 정적 문자열이라 원문 경로·식별자를 노출하지 않습니다. 기본값 off이므로
  기존 소비자의 span 이름은 그대로입니다. `net/http` `ServeMux` 라우팅에서만 유효합니다
  (`http.Request.Pattern`이 채워지는 경우).

## v1.45.0 - 2026-08-06

### 추가

- `llm`: GPT-5.6 explicit prompt cache를 지원합니다 — `Message.CacheBreakpoint`,
  `openaipreset.CompletionRequest.CacheMode`, `Usage.CacheWriteTokens`.
  breakpoint가 붙은 content block에 `prompt_cache_breakpoint{mode:explicit}`를 실어
  안정 prefix 끝을 표시하고, 요청에는 `prompt_cache_options{mode:explicit}`를 보냅니다.
  GPT-5.6 미만 모델은 이 필드를 400으로 거부하므로 소비자가 모델 게이팅을 해야 합니다.

## v1.44.1 - 2026-08-06

### 수정

- `promptguard`: scoring budget을 넘어선 후보도 검사해 masked injection을 놓치지 않으면서
  정상 입력 과차단을 만들지 않습니다. 차단 결정의 decode witness를 정확히 보존합니다.
- `outputguard`: 중첩 short Base64 문맥을 한정된 범위에서 보존하고, root decode 재사용에
  토큰 경계를 요구해 중첩 인코딩된 role header를 차단합니다.
- `logging`: 구조화 map의 자격증명을 근거 기반으로 마스킹합니다 — 키 이름만으로 일괄
  마스킹하지 않아 운영 식별자는 로그에 남습니다.
- `guardtext`: Unicode 17 canonical reordering의 mark run 길이를 제한합니다.
- `release`: source bundle을 manifest commit tree에 결속해 재계산된 대체 아카이브
  체크섬을 거부합니다.

## v1.44.0 - 2026-08-06

### 추가

- `httputil`: `ReadAllLimited`를 도입합니다 — 상한 초과 시 `ErrResponseBodyTooLarge`,
  음수 limit 거부 시 신설 `ErrInvalidBodyLimit`. close 소유권은 호출부에 남습니다.
  `ReadAllAndClose`는 이 helper에 위임합니다.

### 변경 (동작 파괴적)

- `jsonutil.ReadAllLimit`: `maxBytes <= 0`에서 무제한 `io.ReadAll`로 fail-open하던 동작을
  제거하고 `httputil.ErrInvalidBodyLimit`을 반환합니다. `ErrBodyTooLarge`는
  `httputil.ErrResponseBodyTooLarge`의 alias가 되어 `errors.Is` 판정은 양방향 호환입니다.
  스택 내 소비자 전수 grep 결과 non-positive limit 호출은 없습니다(모두 양수 상수/설정,
  0은 생성자에서 기본값으로 clamp). 함수 자체는 Deprecated — `httputil.ReadAllLimited`를
  직접 사용하십시오.

## v1.43.3 - 2026-08-05

### 수정

- `telemetry`: `OTLPInsecure=false` provider가 `OTEL_EXPORTER_OTLP_INSECURE`,
  `OTEL_EXPORTER_OTLP_TRACES_INSECURE`, `http://` scheme endpoint 환경 변수로 평문 강등되던
  경로를 `WithTLSCredentials` 명시로 봉합합니다. upstream otlptracegrpc가 env를 user option보다
  먼저 적용하는 동작이 원인이며, env 강등 회귀 테스트(강등 시 red가 확인된 mutation 검증 포함)를
  복원했습니다. `OTLPInsecure=true`의 평문 동작은 불변입니다.
- `telemetry`: doc.go에 전송 보안 계약을 문서화합니다 — TLS 기본, 평문은 collector가 같은 신뢰
  경계 안에 있는 내부망 예외, log sink와 OTLP collector를 같은 저장소로 합류시키지 않는 운영 전제.

## v1.43.2 - 2026-08-05

### 변경

- `logging`: `room_id`와 `user_id`를 운영 상관관계 ID로 보존합니다. 이름·대화 문맥을 담는
  `room`, `room_name`, `chat_id`, `user_name`, `sender`, `game_key` privacy exact-key와 모든
  credential 마스킹은 그대로 유지합니다.

## v1.43.1 - 2026-08-05

### 수정

- `telemetry`: `OTLPInsecure=true`가 exporter의 공개 `WithInsecure` 경계를 사용하도록 고쳐,
  별도 OTLP endpoint 환경변수에 의존하지 않고도 plaintext gRPC collector에 연결합니다.
- `telemetry`: exporter option test가 upstream private field와 concrete type 대신 실제 gRPC
  연결의 endpoint 및 TLS/plaintext 동작을 검증합니다.

## v1.42.0 - 2026-08-02

### 호환성이 깨지는 변경

- `runtime/bootstrap`: `Options`가 요구하는 runtime 계약을 `Run()`에서 `Run() error`로 바꿉니다.
  runtime이 error를 반환하면 `bootstrap.Run`이 비-0 exit code로 종료해 supervisor(systemd/Docker)의
  재시작 정책이 동작합니다. 기존에는 runtime 실패가 exit code 0으로 끝나 supervisor가 정상 종료로
  오인했습니다. 스택 소비자 호출부는 같은 파동에서 전부 `Run() error`로 전환했습니다.

### 추가

- `httputil.ReadAllAndClose`/`DrainAndClose`/`DefaultDrainLimit`/`ErrNilBody`: close 소유권을 helper가
  갖는 상한부 body 처리를 신설합니다. 상한 초과는 `ErrResponseBodyTooLarge`로 판별하고, read 실패와
  close 실패는 `errors.Join`으로 함께 보존하며, keep-alive 재사용을 위해 잔여 스트림은
  `DefaultDrainLimit`(256KiB)까지만 소비합니다.
- `envutil.DurationE`·`envutil.BoolExplicit`: 설정 오류를 error로 받는 duration 파서와, 미설정/공백을
  unset으로 접는 명시적 bool 파서를 추가합니다. bool 수용 문자열 표는 `Bool`/`BoolE`와 단일 표
  (`lookupBool`)로 통합했습니다.
- `pgxdb.PoolConfig.HealthCheckPeriod`: pool 상태 점검 주기를 노출합니다. 0 이하는 pgx 기본값(1분)을
  유지합니다 — pgxpool은 이 값으로 `time.NewTicker`를 만들므로 0 대입은 panic입니다.
- `workerpool`: finalizer 거부 사유에서 shutdown(`ManagedSubmitRejectedFinalizerClosed`)과 capacity
  (`ManagedSubmitRejectedFinalizerCapacity`)를 분리하고, `ManagedFinalizerSnapshot.OverdueInFlight`
  (FinalizeTimeout을 넘겨 아직 반환하지 않은 callback 수 — reservation 미회수라 지속되면 admission
  고갈로 이어짐)를 추가합니다.
- `openaipreset.LooksLikeToolCallEnvelope`·`openaipreset.CompletionRole`: 소비자가 재구현하던 tool-call
  envelope 판정과 completion role 상수를 export합니다.
- `obsmetrics`: cardinality/label 상한으로 버려진 series를 `<vec>_dropped_series_total` counter로
  노출합니다. 캡에 걸린 series는 exposition에 나타나지 않으므로, 이 family 없이는 "값이 0"과
  "메트릭이 잘렸음"을 구분할 수 없습니다.

### 수정

- `logging`: stdout lane이 EPIPE·ENOSPC로 실패하면 `io.MultiWriter`가 뒤따르는 파일 lane까지 건너뛰어
  내구 기록이 유실되던 문제를 고칩니다. stdout 사본 실패는 삼키고 유실 건수만 세어 Close 시 파일
  lane으로 요약합니다. 파일 lane 실패는 계속 전파됩니다.
- `runtime/httpserver`: `Shutdown`이 이미 종료된 server의 `http.ErrServerClosed`를 정상 종료로 흡수하고,
  deadline 없는 context에는 기본 deadline을 적용합니다.
- `healthprobe`: body close 소유권을 `httputil.ReadAllAndClose`로 이관하고, guard가 없을 때 공유
  `http.DefaultTransport`의 idle pool을 닫지 않도록 고칩니다. guard transport는
  `CloseIdleConnections`를 cleanup으로 돌려줍니다.
- `netguard`: 다중 A/AAAA 후보 dial에서 첫 후보가 context 예산을 모두 소진해 나머지 후보의 failover
  기회가 사라지던 것을, 남은 예산을 남은 후보 수로 나누는 시도별 budget(하한 2초)으로 고칩니다.
- `llm`/`openaipreset`: OpenAI SDK 클라이언트에 `option.WithMaxRetries`를 명시적으로 핀해 재시도
  정책의 소유권을 설정 한 곳으로 고정합니다. JSON decode 실패 메시지는 provider 원문이 새지 않도록
  타입 클래스만 남기고 원문은 `Unwrap`으로만 전달합니다(`errors.Is/As` 보존).
- `pgxdb`: `QueryExecMode` 적용 경로를 DSN `default_query_exec_mode` 한 곳으로 단일화하고, overlay
  경로에서 `MinConns > MaxConns` 역전을 둘 다 명시된 경우에만 검사합니다.
- `jsonutil.Extract`: regex 캡처 반환값이 입력 전체 사본을 alias해 큰 입력의 GC를 막던 것을
  `bytes.Clone`으로 끊습니다.
- `promptguard`: singleflight 공유 반환값을 in-place로 변형하지 않는 불변식을 문서화하고, 소유권
  경계(반환 직전) 한 곳에서만 clone하도록 중복 clone을 제거합니다.

### 성능

- `jsonutil.Extract`: 입력 전체가 object/array JSON인 경우 regex 없이 즉시 반환하는 fast-path를
  추가합니다(대형 페이로드 기준 약 8배).
- `obsmetrics`: label escaper를 재사용하고 canonical label 직렬화의 할당을 줄이며,
  `runtime.ReadMemStats`(stop-the-world)를 1초 TTL 캐시로 묶어 다중 scraper에서도 STW 빈도를
  고정합니다.
- `logging`: sanitize 변경-감지 패스의 결과를 재구축에 재사용해, 변경 없는 선행 attr의 중복 정제를
  제거합니다.
- `guardtext`: base64 후보 디코드가 스팬마다 할당하던 목적지 buffer를 열거 1회당 1개로 재사용하고,
  `IsReadableString`으로 `[]byte` 변환 복사를 제거합니다.

### 사용 중단 예고

- `envutil`: 스택 소비자가 0인 다중-키 폴백 계열(`IntAnyE` 등)을 Deprecated로 표기합니다. 제거는
  README 호환성 정책에 따라 이후 minor에서 진행합니다.

## v1.41.0 - 2026-08-02

### 호환성이 깨지는 변경

- 스택 내 소비자가 0인 exported API를 제거합니다. README 호환성 정책(보장 대상은
  `hololive-bot`·`chat-bot-go-kakao`·`twentyq-bot` 세 소비자로 한정하며, 소비자가 0인 exported
  API는 major 승격 없이 minor에서 제거 가능)에 근거한 minor 릴리스 제거입니다. 세 소비자
  전수 확인 결과 아래 심볼의 참조는 0건이며, 외부 importer 호환성은 보장 대상이 아닙니다.
  - `pgxdb.OpenPoolWithRetry`. 세 소비자는 `OpenPool`(hololive-bot·twentyq-bot) 또는
    `OpenPoolDSN`(chat-bot-go-kakao)만 사용합니다. 연결 재시도 정책은 호출자가 소유합니다.
  - `pgxdb.Options.DNSFallback`과 그 폴백 구현 `pgxdb.ShouldFallbackToLocalhost`. host가 정확히
    `postgres`일 때의 localhost DNS 구제 경로였습니다.
  - `workerpool.QueuedPool`, `workerpool.NewQueued`, `workerpool.NewQueuedWithLogger`,
    `workerpool.QueuedConfig`. `ManagedPool`(`NewManaged`)이 실질 후계이며 유일한 스택 소비자인
    chat-bot-go-kakao는 `NewManaged`만 사용합니다.
- `pgxdb.RetryConfig`를 `pgxdb.PingConfig`로, `DefaultRetryConfig()`를 `DefaultPingConfig()`로,
  `Options.Retry`를 `Options.Ping`으로 이름을 바꾸고 `MaxAttempts`·`BaseDelay`·`MaxDelay` 세 필드를
  제거합니다. `OpenPoolWithRetry` 제거로 세 필드를 읽는 코드가 사라져, 남겨두면 설정해도 무시되는
  필드가 됩니다. 이 패키지가 소유하는 값은 `PingTimeout` 하나뿐이므로 타입 이름을 내용에 맞췄습니다.
  `PingTimeout`의 의미와 미설정 시 기본값 5초는 그대로이며 동작 변경은 없습니다. 유일한 스택
  소비자인 hololive-bot은 `PingTimeout`만 설정하므로 호출부를 `Ping: pgxdb.PingConfig{…}`로 바꾸면
  됩니다.

### 문서

- `pkg/db/pgxdb` package 문서에 `Config.SSLRootCert`와 `POSTGRES_SSLROOTCERT` env의 이중 경로
  우선순위를 명시합니다: 구조체 필드가 우선이고, trim 후 빈 값일 때에만 env로 폴백하며, 둘 다
  비면 `sslrootcert`를 DSN에서 생략해 pgx/libpq 기본 탐색 경로에 위임합니다. 이 폴백은 `Config`
  경유 경로에만 적용되고 `OpenPoolDSN`은 DSN 원문을 그대로 사용합니다. 동작 변경은 없습니다.

## v1.40.0 - 2026-08-01

### 호환성이 깨지는 변경

- `feat(logging)!:` text formatter와 `logging.FormatText`를 제거합니다. `Config.Format`의 빈 값은
  JSON을 선택하고, `"json"` 외 값은 `EnableFileLogging*` 계열이 명시적으로 거부합니다.
  no-arg `NewLogger()`도 JSON handler를 사용합니다.

### 수정

- `logging` privacy exact-key 마스킹을 중첩 `map[string]any`까지 재귀 적용합니다. privacy key가
  없는 경로는 사본을 만들지 않고, self-referential map은 depth 8에서 탐색을 멈춥니다.
  `map[string]string`과 struct 필드는 계속 대상이 아닙니다.
- worker profile의 최대 delivery horizon이 `1h` 이상이면 `receive.dedup_ttl_ms`에 불가능한 값을
  요구하지 않고, 원인인 `delivery.max_attempts`/`delivery.request_timeout_ms` 조합과 계산된 horizon을
  보고합니다. dedup TTL 부족 오류도 계산된 horizon과 필요한 최소 TTL을 포함합니다.
- `obsmetrics`에 동일 metric family의 header를 한 번만 쓰는 `WriteCounterSeries`와
  `WriteGaugeSeries`를 추가합니다. 기존 단일-series API는 호환 유지합니다.

## v1.39.0 - 2026-08-01

### 호환성이 깨지는 변경

- `logging.NewUnsanitizedLoggerForTests`와 호환 별칭 `logging.NewTestLoggerWithOutput`을 제거합니다.
  두 생성자는 sanitize handler를 우회하므로 production package에서 제공하지 않고, 필요한 테스트가
  각 `_test.go` 안에서 raw handler를 직접 구성합니다. 프로덕션 호출부는 스택 전체에서 0건이고,
  영향은 테스트 코드에 한정됩니다.
- credential key 마스킹이 값 타입에 종속되지 않습니다. `slog.Int64("token", …)`,
  `slog.Bool("password", …)`, `slog.Any("authorization", …)`처럼 canonical key인데 값이 문자열이
  아닌 attr이 그동안 마스킹을 빠져나갔습니다. key 기반 판정(`isSensitiveKey`)을 `KindAny`,
  `KindGroup`, `KindString` **세 값 분기 전부보다 앞으로** 옮겨 privacy 판정과 같은 자리에
  둡니다. 이전에는 `slog.Any("token", map[string]any{…})`의 마스킹 여부가 그 map이 privacy key를
  포함하는지라는 무관한 조건에 종속됐고, `slog.Group("access_token", …)`는 아예 마스킹되지
  않았습니다. 키 집합은 그대로입니다. **`has_token` 같은 boolean 플래그도 `*_token` 규칙에 걸려
  이제 마스킹됩니다**(문자열 값이었다면 이전에도 마스킹되던 키입니다). privacy key
  (`user_id`/`room_id` 등)는 이미 가드 앞에서 판정되고 있어 이번 변경의 영향이 없습니다.
- 이름이 credential key인 group은 그 아래 attr을 key와 무관하게 전부 마스킹합니다.
  `WithGroup("access_token")` 아래의 `raw`는 이전에 그대로 출력됐습니다. privacy key group에만
  있던 규칙을 credential에도 적용해 두 정책을 대칭으로 맞춥니다.

### 기능

- worker profile의 기본 `receive.dedup_ttl_ms`를 `16m`으로 올리고, 수신 dedup TTL이
  `delivery.max_attempts`, `delivery.request_timeout_ms`, retry wait ceiling, enabled breaker cooldown으로
  유도한 최대 delivery horizon보다 엄격히 길도록 검증합니다. 기본 profile의 horizon은 `15m`입니다.
  기존 profile에 `60s` 등 더 짧은 값을 명시한 소비자는 기동 전에 profile을 갱신해야 합니다.
  duration은 wire 단위와 같은 whole millisecond만 허용해 `Validate`를 통과한 값이 canonical JSON에서
  horizon 경계로 절삭되지 않게 하며, enabled breaker cooldown은 `1h`로 제한해 horizon 곱셈의
  `time.Duration` overflow를 차단합니다. raw `*_ms` integer도 곱셈 전에 범위를 확인해 overflow가
  작은 양수 duration으로 wrap되어 기존 aggregated validation을 우회하지 못하게 합니다.

- `logging.Config`에 `Format`을 추가합니다. `logging.FormatText`(빈 값과 동일)와
  `logging.FormatJSON`을 지원하며, 알 수 없는 값은 `EnableFileLogging*` 계열이 error로 거부합니다
  — 새 부팅 실패 경로입니다. formatter 생성을 `newFormatHandler` 한 지점으로 모아 어떤 포맷을
  고르든 sanitize 래핑이 구조적으로 유지되고, 이 불변식은 `pkg/logging` 비-test 소스에서
  formatter 생성자 호출을 AST로 훑는 게이트 테스트가 강제합니다.
- 타임스탬프 정밀도는 lane마다 다릅니다. json lane은 slog 기본값인 **나노초** 단위
  RFC3339Nano(`2026-07-31T09:23:06.879728621+09:00`)이고, text lane은 tint `TimeFormat`이
  RFC3339라 초 단위입니다.

### 보안

- json lane의 `source.file`이 빌드 머신의 절대 경로 대신 `logging/format.go` 형태로 축약됩니다.
  slog 기본값은 빌드 디렉터리 구조를 모든 record에 실었습니다. text lane(tint)은 이미 축약된
  형태였습니다.
- 비동기 stdout writer의 종료 손실 요약이 활성 포맷을 따릅니다. 이전에는 handler 바깥에서 raw
  문자열을 stdout lane에 직접 써서, `Format: json` + `Options{AsyncStdout: true}` 조합에서 drop이
  한 번이라도 발생한 프로세스가 종료 시 JSON 스트림 한가운데 파싱 불가 라인을 남겼습니다
  (Promtail/Vector/Fluent Bit의 json parser가 깨집니다). 요약은 절단 건수도 함께 보고합니다.
- 비동기 stdout writer의 라인 절단(64 KiB)이 record 경계를 보존합니다. 이전에는 말미의 개행까지
  잘라내 절단된 조각이 다음 record와 한 줄로 이어붙었고, json lane에서는 2건이 함께 파싱 불가가
  됐습니다. 이제 마지막 바이트를 개행으로 확정해 손상을 해당 record 1건으로 가둡니다. 절단
  지점이 multi-byte rune 한가운데면 마지막 rune 경계까지 물러나므로, 남는 바이트는 항상 valid
  UTF-8입니다(이전에는 `한` 반복 입력이 `…한\xed\x95`로 잘렸고, 여러 수집기가 invalid UTF-8을
  JSON parse 오류보다 험하게 다룹니다).
- 종료 요약의 `truncated`가 **실제로 stdout에 도달한** 절단 라인만 셉니다. 계상 지점이 큐 진입
  시점이었고 `select`의 송신 피연산자는 `default`를 타도 평가되므로, 절단됐지만 큐가 가득 차
  버려진 라인이 `dropped`와 `truncated` 양쪽에 계상됐습니다. 정체된 depth-1 큐에 oversize 20건을
  넣으면 stdout에 도달한 절단 라인은 2건인데 요약은 `truncated=20`을 보고했고, 운영자가 존재하지
  않는 손상 라인 18건을 찾게 됐습니다. 계상을 target 기록 성공 이후로 옮겨, 큐에 들어간 뒤
  `target.Write`가 실패한 라인도 `dropped`에만 잡힙니다. 두 카운터의 합은 `Write` 호출 수를
  넘지 않습니다.
- `asyncDropWriter.Close`가 멱등입니다. `stop sync.Once`가 `close(done)`만 감쌌고 `stopped`는
  닫힌 채 유지되므로, 두 번째 `Close`가 손실 요약을 다시 썼습니다. 동시 `Close`에서는 요약을 쓰는
  goroutine들이 같은 target에 함께 들어가 `-race`가 검출하는 data race가 됩니다. 현재 스택의
  소비자에서는 도달 불가 경로였으나 계약 결함이라 닫습니다.
- `netguard.GuardedClient`가 표준 `*http.Transport` 경로에도 request 시점 scheme/host/port
  검증을 적용하고, `resolveDialAddresses`가 dial 직전에 `AllowedHosts`/`AllowHost`를 강제합니다.
  기존에는 `GuardedClient`와 `GuardedTransport`의 dial 경로가 port와 IP만 검사해 host allowlist가
  적용되지 않았습니다. dial을 통제할 수 없는 opaque `RoundTripper`에는 IP 정책을 보장하지 않음을
  문서로 명시하고, `Policy.RequireGuardedDial`과 `DialGuardedRoundTripper` 계약,
  `ErrUnguardedTransport`로 fail-closed 선택지를 추가합니다.
- `netguard.RedirectPolicy`의 credential 유지 경계를 hostname 단위에서 normalized scheme +
  hostname + effective port의 same-origin 단위로 좁힙니다. `ForwardHeaders=false`일 때
  same-host/different-port와 HTTPS→HTTP downgrade redirect에서도 `Authorization` 등 기존 header를
  제거합니다. 비교 기준은 직전 hop이 아니라 최초 요청(`via[0]`) origin입니다. `net/http`가 hop마다
  최초 요청 header 사본을 복원하므로, 직전 hop과 비교하면 hop1에서 제거한 header가 hop2에서
  복원되어 다시 새어 나갑니다.
- `netguard`의 host 비교를 IDN-safe하게 맞춥니다. dial 계층은 punycode, request 계층은 unicode
  host를 보므로 allowlist와 후보를 모두 punycode ASCII로 정규화해 비교합니다. 이전에는 unicode·
  punycode 어느 형태로 allowlist를 적어도 한쪽 계층이 매치하지 못했습니다. 이를 위해
  `golang.org/x/net`이 indirect에서 direct dependency로 승격됩니다(버전 변경 없음).
- `netguard.Policy.RequireGuardedDial`이 어떤 조합에서도 검증을 약화시키지 않습니다.
  `DialGuardedRoundTripper` 선언은 `RequireGuardedDial` 요구 충족만 의미하며, opaque RoundTripper
  경로의 request 시점 `ValidateTarget`(resolve + IP 정책)은 선언 여부와 무관하게 항상 실행됩니다.
- `netguard`의 request 거부 경로가 `RoundTripper` 계약대로 `req.Body`를 닫습니다.
- `envutil.LoadDotenvFile` strict 모드의 TOCTOU를 제거합니다. `Lstat` 검사 후 별도 `os.Open`으로
  다시 여는 대신 no-follow FD를 먼저 열고 fstat, `os.SameFile`, regular-file, mode를 검증한 뒤 그
  FD에서 읽습니다. regular-file 검사가 없어 0600 FIFO가 startup을 무기한 막을 수 있던 문제도 함께
  닫습니다(no-follow open에 `O_NONBLOCK` 추가). non-strict local dotenv 동작은 그대로입니다.
- `logging` sanitize handler에 credential 정책과 분리된 privacy exact-key 정책을 추가합니다.
  `room`, `room_id`, `chat_id`, `user_id`, `user_name`, `room_name`, `thread_id`,
  `session_thread_id`, `sender`, `game_key`는 `Resolve()` 직후 모든 `slog.Kind`와 nested group에서
  마스킹됩니다. `channel_id`, `video_id` 같은 공개 콘텐츠 ID를 지키기 위해 `*_id` 전면 마스킹은
  쓰지 않습니다. `WithGroup`/`Group` 이름이 privacy key면(`WithGroup("sender")`) 그 아래 attr은
  key와 무관하게 전부 마스킹되고, `slog.Any`의 `map[string]any`는 map key를 걸어 값을 마스킹합니다
  (호출자 map은 변형하지 않고 hit일 때만 사본을 만듭니다).

### 성능

- json lane의 `source.file` 축약이 `filepath.Join`(내부 `Clean`) 대신 substring slice를 씁니다.
  `AddSource`가 켜진 json record마다 할당이 10개/714 B → 9개/690 B로 줄어듭니다. 빈 경로에 `""`를
  돌려주는 것이 부수적으로 중요합니다 — `filepath` 판본은 `"."`를 만들어, `PC 0` record가 낳는
  빈 `Source`를 slog이 생략하지 못하게 되살렸고 json 종료 요약에 `"source":{"file":"."}`가
  실렸습니다(text lane은 tint가 `PC==0`을 건너뛰어 두 lane이 어긋났습니다).
- sanitize handler의 **할당은 변경 전후 동일합니다**(`Clean` 0 alloc, `WideClean` 80 B/1 alloc,
  `PrivacyMap` 1,008 B/6 alloc, `GroupNoSecret` 912 B/13 alloc). 처리 시간은 회귀합니다 — 변경
  전후 test binary를 미리 컴파일해 16 라운드 교차 실행한 **최소값 기준 +0.3% ~ +11.7%**입니다
  (`Clean` +11.7%, `WidePrivacy` +11.5%, `GroupNoSecret` +11.2%, `WideClean` +9.9%,
  `GroupWithSecret` +4.6%, `PrivacyMap` +4.2%, `PrivacyGroup` +2.2%, `Sensitive` +1.8%,
  `PrivacyKeys` +0.3%).
- 위 회귀는 credential key 판정을 값 분기보다 앞으로 옮긴 대가입니다. `isSensitiveKey` 호출이
  늘어나는 대상은 **모든 attr이 아니라** 값이 문자열이 아닌 attr, group 이름, `KindAny` attr입니다
  (계측한 record당 호출 수: `Clean` 3→4, `WideClean` 6→7, `GroupNoSecret` 3→6, `PrivacyMap` 2→4).
  `KindString` attr은 이전에도 같은 판정을 거쳤고 privacy key attr은 `isPrivacyKey`에서 먼저
  반환되므로 호출이 늘지 않습니다(`PrivacyKeys` 3→3). 회귀는 `KindAny` map(`PrivacyMap` +4.2%)에
  그치지 않고 counter·duration 같은 비-string scalar가 섞인 평범한 clean record에도 같은 폭으로
  옵니다. `Clean` +11.7%와 `GroupNoSecret` +11.2%의 차이는 측정 노이즈 안이므로 둘 사이의 순서는
  주장하지 않습니다 — 호출 수 증가만 보면 group(3→6)이 clean(3→4)보다 큽니다. 실측은 호출 1회당
  비용(`BenchmarkIsSensitiveKey` 55~60 ns/op)으로 환산한 값보다 큽니다. 마스킹 정확성을 위해
  감수한, 수정에 내재한 비용입니다.

### 알려진 한계

- 알 수 없는 log level은 info로 무음 fallback하지만, 알 수 없는 log format은 error입니다. 이
  비대칭은 의도적입니다 — 잘못된 level은 verbosity만 바꾸지만, 잘못된 format은 수집기가 스트림
  전체를 읽지 못하게 만듭니다.
- `slog.Any`에 담긴 **구조체**는 여전히 마스킹되지 않습니다(reflection 미대응).
  `slog.Any("payload", struct{UserID, Token string}{…})`는 필드 값이 그대로 나갑니다. 단, 그
  attr의 key 자체가 privacy/credential key면 attr 전체가 마스킹됩니다.
- 절단된 record 자체는 여전히 파싱 불가입니다. 경계 보존과 rune 경계 보존은 **다음** record와
  수집기의 UTF-8 처리를 지키는 것이지 절단된 record를 복구하지 않습니다. 절단 발생 사실은 종료
  요약의 `truncated` 카운터로만 관측됩니다.
- `truncated` 카운터는 target 기록이 성공한 뒤에 오릅니다. 따라서 stdout lane이 정체된 동안에는
  이미 절단된 라인도 카운터에 아직 반영되지 않습니다. 종료 요약은 run goroutine이 큐를 모두
  비운 뒤에 기록되므로, **마지막 `Write`가 `Close`보다 happens-before인 한** 요약 값 자체는
  영향을 받지 않습니다. `Close`와 동시에 진행되는 `Write`는 run이 `drain`의 `default`를 탄 뒤
  큐에 들어갈 수 있고, 그런 라인은 전달도 계상도 되지 않습니다 — 이번 변경 이전부터 있던 설계
  내재 속성입니다.
- formatter 게이트는 `pkg/logging` 트리의 비-test 소스만 훑습니다. 다른 패키지가 자체 slog
  handler를 만드는 경로(예: `bootstrap.Options.NewLogger` 후크)는 이 게이트의 범위 밖입니다.
  게이트는 호출식이 아니라 생성자 **참조**를 세므로 `f := slog.NewJSONHandler` 형태의 함수 값과
  dot-import(`import . "log/slog"`)도 잡지만, 판정은 AST 수준이라 package 이름을 가리는 지역
  식별자까지는 구분하지 못합니다.
- privacy 마스킹은 attr key와 `map[string]any` key에만 적용됩니다. **struct 필드(reflection),
  error/message 문자열에 보간된 식별자(`fmt.Errorf("user_id=%s", …)`), log message 본문은 이번
  범위에서 마스킹되지 않습니다.** 이 경로는 callsite 정리와 정적 스윕이 소유합니다.
  `map[string]string` 등 다른 map 타입도 아직 대상이 아닙니다.
- `GuardedClient`가 반환하는 client의 `Transport`는 더 이상 `*http.Transport`로 타입 단언되지
  않습니다(`CloseIdleConnections`는 위임합니다). 내부 transport 접근이 필요하면 정책 구성 시점에
  직접 보관하십시오.
- no-follow open을 지원하지 않는 플랫폼(`!unix`)에서 strict dotenv는 **의도적으로 항상 실패**합니다.
  non-strict local dotenv 경로는 그대로 동작합니다.

## v1.38.0 - 2026-07-29

### 수정

- output guard가 장문 기술 답변의 일부 base64 유사 구간을 `decode_incomplete`로 오탐해 전체 출력을
  차단하던 문제를 해소했습니다. decode 후보의 문맥·확장 경계를 더 엄격하게 판정하고 장문 회귀
  테스트를 추가했습니다.

## v1.37.1 - 2026-07-29

### 유지보수

- tag 기반 release에서 local full gate, SBOM, checksum manifest, keyless attestation과 immutable
  GitHub Release를 생성·검증하는 provenance 파이프라인을 추가했습니다.

## v1.37.0 - 2026-07-28

### 보안

- 2021년 이후 갱신되지 않은 `mtibben/confusables`의 Unicode 13.0 table을 제거하고,
  Unicode 17.0 UTS #39 `confusables.txt`를 versioned URL과 SHA-256으로 검증해 생성한
  repository-owned table로 교체합니다. Unicode 15/17 `UnicodeData.txt`도 함께 pin해 canonical
  decomposition 20개와 combining class 46개의 차이를 toolchain-independent overlay로 생성합니다.
  기존 NFD → mapping → NFD skeleton 계약은 유지하면서 6,565개 최신 mapping을 적용합니다.

### 유지보수

- prompt rulepack YAML owner를 `gopkg.in/yaml.v3 v3.0.1`에서 유지되는 canonical module인
  `go.yaml.in/yaml/v3 v3.0.5`로 이전합니다. 공개 API와 rulepack schema는 변경하지 않습니다.
- Unicode source checksum과 generated table drift를 release gate에서 검증하고 Unicode License V3
  고지를 `THIRD_PARTY_NOTICES.md`에 포함합니다.

## v1.36.2 - 2026-07-28

### 유지보수

- `openai-go/v3`를 `v3.46.0`, `quic-go`를 `v0.61.0`, `go-isatty`를 `v0.0.24`로 갱신하고
  관련 전이 dependency를 최신 compatible revision으로 정렬합니다.
- local/repository security gate의 `govulncheck`를 `v1.6.0`으로 갱신합니다.

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
- `docs/REFACTORING_PLAN_20260602.md`에 이 작업의 완료된 P1/P3 결정을 기록했습니다.

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
