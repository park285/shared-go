# shared-go

Iris Stack의 Go 프로젝트들(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot` 등)이 공통으로 사용하는 유틸리티 라이브러리 전용 Go 모듈입니다.

본 라이브러리는 `cmd/` 실행 진입점이 없는 순수 라이브러리(Library-only) 모듈로 설계되었으며, 이를 호출하는 서비스들을 위해 일관되고 안정적인 API 사양을 유지합니다.

> **호환성 정책**: 이 모듈의 호환성 보장 대상은 iris-stack 내부 소비자(`hololive-bot`, `chat-bot-go-kakao`, `twentyq-bot`, `iris-console`)로 한정됩니다. 외부 사용을 막지는 않지만 외부 importer 호환성은 보장하지 않으며, 스택 내 소비자가 0인 exported API는 major 승격 없이 minor 릴리스에서 제거될 수 있습니다.

## 설치 (Installation)

`llm/openaipreset.Client.GenerateLayeredResponsesJSON`은 Responses API 스트리밍을
사용합니다. 중간 delta 대신 정상 종료된 `response.completed`의 전체 `output_text`를
반환하므로 호출자는 기존 출력 검증과 JSON 디코딩을 그대로 적용할 수 있습니다.
미완료·실패·취소·종료 후 오류에서는 부분 출력을 반환하지 않습니다.
이 변경은 v2.6.1에 포함되며, 소비자는 해당 버전으로 의존성을 갱신해야 합니다.

```bash
go get github.com/park285/shared-go/v2@latest
```

## 제공 패키지 목록 (Package Catalog)

| 패키지 경로 | 기능 및 역할 |
|---|---|
| `pkg/backoff` | 시도 횟수 및 상태를 기반으로 한 지수 백오프(Exponential Backoff) 계산 유틸리티 (대기 및 재시도 루프 제어는 호출부에서 직접 처리) |
| `pkg/db/pgxdb` | `jackc/pgx/v5` pgxpool 기반 PostgreSQL 연결 풀 생성 도구 (sslmode 명시 강제, sslrootcert는 `Config.SSLRootCert`로만 지정하고 env를 읽지 않음, 라이브러리 기본값 없이 시작 ping 기한(`Ping.PingTimeout`, 필수)·구조화 경로의 풀 크기·수명(`ConnMaxLifetime`·`ConnMaxIdleTime`, 필수, 지터 0은 지터 없음)을 호출자가 명시, 재시도는 호출자 소유) |
| `pkg/dbmigrate` | embed.FS의 `manifest.txt` 순서대로 SQL 마이그레이션 파일을 실행하는 공통 처리 모듈 (pgx 실행 함수 주입 방식) |
| `pkg/envutil` | 환경 변수 로드 및 `*_FILE` 형태의 파일 경로로 보안 토큰/시크릿 값을 주입하는 도구 (숫자·bool·duration은 잘못된 값을 오류로 돌려주는 `IntE`·`Int64E`·`FloatE`·`BoolE`·`BoolExplicit`·`DurationE`만 제공, `StringOrSecretFile`은 `KEY`와 `KEY_FILE`이 함께 설정되면 오류) |
| `pkg/h3` | HTTP/3 전송 프로토콜 설정 도구 (자체 CA 번들 등록, TLS 상세 사양 정의) |
| `pkg/health` | version·uptime·component 상태 스냅샷과 /health·/ready 응답 본문 계약을 보관하는 server 측 health 모듈 |
| `pkg/healthprobe` | 서비스 헬스체크 및 프로브(Readiness / Liveness Probe) 도구 |
| `pkg/httputil` | HTTP 클라이언트 커넥션 풀링 및 프로파일 구성 도구, 관리 API key 인증(`X-API-Key` 헤더만 수용, `Authorization: Bearer`는 거부) |
| `pkg/irisdurable` | Iris webhook durability 계약(admission·nonce·reply outbox 어휘, bounded reissue ladder). Iris admission 보존·replay 지평의 정본은 Iris 계약 파일과 iris-client-go 공개 상수이며 이 모듈은 사본을 두지 않음 |
| `pkg/irisdurable/contracttest` | irisdurable 계약을 봇별 저장소 구현에 적용하는 재사용 테스트 스위트(admission 멱등성, nonce set-once, outcome_unknown 보존, bounded reissue, 보존·지평) |
| `pkg/irisdurable/pgstore` | irisdurable 계약의 PostgreSQL 구현(ordering key FIFO inbox, nonce set-once TTL, reply outbox claim fence·redrive·retire·prune). 정책 기본값이 없어 `Options`의 lease·시도 상한·백오프·보존·`IrisAdmissionRetention`·`IrisAutomaticReplayHorizon`을 호출자가 넘기고, 0인 값을 쓰는 연산은 오류를 반환한다. inbox 종단 보존은 Iris admission 보존보다 길어야 하고, 자동 replay 지평은 정본 지평 이하이며 정본 지평은 admission 보존보다 짧아야 한다. `ClaimBatch`가 결과 미상 오류(`*ClaimBatchError`)를 내거나 후속 처리가 실패하면 소비자가 계속 처리할 claim id를 keep으로 넘겨 `ReleaseClaimToken(ctx, token, keep)`으로 나머지 claim만 즉시 반납한다. 테이블 DDL은 소비 저장소 migration이 소유하고 `testdata/schema.sql`이 참조 스키마다 |
| `pkg/kakaoformat` | Markdown 표현을 카카오 일반 채팅용 평문과 링크로 변환하는 formatter |
| `pkg/llm` | LLM provider 클라이언트 추상화 (`JSONGenerator`, OpenAI 호환 JSON 생성·가드, 필수 SDK 재시도 수 `OpenAICompatibleConfig.MaxRetries`). 지시는 `InvariantPrompt`/`DeveloperPrompt` 계층으로만 전달 |
| `pkg/llm/openaipreset` | `pkg/llm`의 OpenAI 호환 JSON 생성 경로를 functional options로 구성하는 재사용 preset (`WithMaxRetries` 필수, 지시는 `PromptLayers`로만 전달) |
| `pkg/logging` | Slog 기반의 구조화된 로깅 모듈 (비동기 처리, credential 마스킹과 서비스가 `Options.PrivacyKeys`로 넘긴 개인 식별자 키 마스킹, 실시간 로그 로테이션 지원) |
| `pkg/netguard` | 외부 HTTP 대상 URL 및 dial 주소를 fail-closed로 검증하는 네트워크 가드 (private/loopback/link-local/ULA 대역 차단, `Policy.AllowedHosts` allowlist 지원) |
| `pkg/obsmetrics` | `client_golang` 의존성 없이 Prometheus 평문 텍스트 exposition을 생성하는 메트릭 키트 (webhook/런타임 메트릭, prefix 네임스페이스 분리) |
| `pkg/outputguard` | LLM 생성 출력 가드 (구조화된 차단 사유, 출력·보호 텍스트 크기 제한, 역할/시크릿/보호 지침 중첩 탐지, 요청 소유 보호 index를 묶는 `BoundGuard`) |
| `pkg/panicguard` | callback panic을 복구해 구조화된 진단을 남기는 실행 경계 |
| `pkg/promptguard` | source-aware 프롬프트 인젝션 가드 (embedded v3 한/영/일 baseline, optional rules-only overlay, bounded decoding, policy digest, TTL cache) |
| `pkg/reflectutil` | interface에 담긴 nilable 값을 안전하게 판별하는 reflection helper |
| `pkg/retry` | `pkg/backoff`의 지연 값 계산을 사용해 context 취소를 존중하며 sleep·재시도·중단을 수행하는 재시도 루프(`WithRetry`) 구현체 |
| `pkg/runtime/bootstrap` | 프로세스 시작 입력을 검증하고 runtime 구성을 적용하는 bootstrap helper. 필수 `NewLogger`로 서비스 logger를 만든다. `GOMAXPROCS`는 Go runtime의 container 인식 기본값을 따른다 |
| `pkg/runtime/httpserver` | HTTP server 생성(수신 한계·OTel 계측)·본문 읽기 예산·시작·실행·graceful shutdown 소유권을 단일화하는 lifecycle helper. 서버 실행 정본: 종료 ctx로 수명을 닫는 프로세스는 `Run(ctx, server, timeout)`, `lifecycle.Run` 훅 안에서는 `StartServerWithPrefix`와 `Shutdown` |
| `pkg/runtime/lifecycle` | 기동·주기 실행·종료 정리와 다중 close를 조정하는 runtime lifecycle helper |
| `pkg/sqlutil` | embed FS의 SQL asset을 읽고 공백을 제거하며 누락·빈 query에서 panic하는 loader |
| `pkg/stringutil` | 범용 문자열 처리 유틸리티 |
| `pkg/telemetry` | OpenTelemetry 기반의 분산 트레이싱(Tracing) 정보 설정 및 컨텍스트 전파 유틸리티 |
| `pkg/workercontract` | strict worker profile 로드(env 경로, 소비 서비스가 소유한 `Identity`), service-owned settings decode·shape 검증, diagnostics registry, 공통 Prometheus exposition 모듈 |
| `pkg/workerpool` | 큐(Queue) 기반의 동시성 제어 워커 풀 구현체 |

새로운 공통 기능이 필요할 경우, `pkg/` 하위에 신규 패키지 형식으로 추가해 주십시오. 스택 소비자가 쓰지 않는 구현 세부는 `pkg/internal/` 아래에 둡니다(예: Prometheus text 표기 규칙을 obsmetrics·workercontract가 공유하는 `pkg/internal/promtext`).

### 공개 API 일관성 규칙

새로 추가하거나 바꾸는 공개 API는 아래 규칙을 따릅니다. 기존 API의 미준수는 해당 패키지를 고칠 때 함께 맞춥니다.

- 설정은 struct 옵션을 기본으로 하고, 선택지가 계속 늘어나는 열린 집합일 때만 functional option을 씁니다(`llm/openaipreset`은 열린 집합이라 option을 유지합니다).
- `*slog.Logger`를 받는 API는 nil이면 `slog.Default()`를 씁니다.
- 반환 오류 문자열은 `<pkg>: ` 접두로 시작합니다. 콜백 오류를 그대로 돌려주는 것이 계약인 함수(`panicguard.RunE`·`logging.RunOperation`·`dbmigrate.WithAdvisoryLock`)의 콜백 오류와 `io.Reader` 계약상 그대로 전달하는 하위 reader 오류(`io.EOF` 등)는 감싸지 않습니다. 호출자가 접두를 정하는 helper(`logging.LogAndWrapError`·`httpserver.Shutdown`)는 그 접두를 씁니다.

## 로그 마스킹 키

보존·마스킹 키의 정본은 이 표입니다. key는 대소문자·`-`·`.`·공백을 정규화해 비교하며, 일치한 attr·group·중첩 `map[string]any`(깊이 8) 값을 `***REDACTED***`로 바꿉니다.

| 구분 | 키 | 소유 |
|---|---|---|
| 기본 마스킹(credential) | `password`·`pwd`·`passwd`·`secret`·`client_secret`·`token`·`bot_token`·`access_token`·`refresh_token`·`api_key`·`apikey`·`private_key`·`secret_key`·`authorization`·`auth_header`·`cookie`·`webhook_url`·`database_url`·`postgres_dsn`·`connection_url`, 접미 `_token`·`_secret`·`_password`·`_pwd`·`_passwd`·`_api_key`·`_private_key`·`_secret_key`, 값이 비밀처럼 보이는 `key` | shared-go `logging` |
| 서비스 privacy 키 | 서비스가 `logging.Options.PrivacyKeys`로 넘긴 키(예: chat `room`·`session_thread_id`, twentyq `sender`) | 각 서비스 |
| 보존 | 위 두 행에 없는 키. `room_id`·`user_id`·`request_id`·`trace_id` 같은 상관관계 ID는 서비스가 넘기지 않는 한 보존하며 `*_id` 접미 일괄 마스킹은 하지 않음 | — |

라이브러리는 업무 식별자 privacy 키를 내장하지 않습니다. `NewLogger()`와 `PrivacyKeys`를 비운 logger는 credential 규칙만 적용합니다.

## stdout 사본과 유실 계측

파일 로깅의 `Options.AsyncStdout`은 stdout 사본에만 bounded queue를 적용합니다.
정상 JSON은 그대로 전달하고, 64 KiB를 넘는 사본은 유효한 JSON으로 축약합니다.
`time/level/msg/event/trace_id/span_id/request_id` 문자열을 각각 최대 1024 UTF-8 bytes로
보존하며 `stdout_truncated`·`stdout_original_bytes`를 추가합니다. 나머지 필드의
전체 sanitized 원본은 동기 파일 lane에 남습니다.

`logging.SnapshotStdout()`과 `obsmetrics.WriteLoggingMetrics(w)`는 프로세스 수명 동안의
`iris_stack_logging_stdout_dropped_total`·`iris_stack_logging_stdout_truncated_total`을
제공합니다. 큐 포화·종료 후 쓰기·sink 오류·short write는 dropped, 성공적으로 전달한
축약 사본은 truncated입니다. 두 counter는 파일 쓰기·console-only logger·종료 진단 자체를 집계하지 않습니다.
async 유실 요약은 파일에도 기록되며, 포화 큐에서는 복사 전에 거절해 추가 할당을 하지 않습니다.

파일 lane 쓰기 실패는 `logging.SnapshotFile().WriteFailed`로 세고, 같은 `WriteLoggingMetrics`가
`iris_stack_logging_file_write_failed_total`로 노출합니다. 오류나 short write로 끝난 동기 파일 기록
(`MaxSizeMB`를 넘는 record, 디스크 부족, rotate·open 실패, 종료 시 유실 요약 기록 포함)이
대상이며 실패한 record는 일부만 기록됐을 수 있습니다. console-only logger에는 파일 lane이 없습니다.
0은 알려진 실패가 없다는 뜻일 뿐 내구 저장을 증명하지 않습니다.

`Close`의 2초 제한은 대기 상한이지 I/O 취소가 아닙니다. timeout 이후에도 pending write가
완료될 수 있으므로 종료 요약은 기록 시점에 알려진 결과만 나타냅니다.
새 API를 사용하는 소비자는 같은 shared-go 소스로 workspace 검증한 뒤 실제 게시 버전으로
의존성을 함께 갱신해야 합니다. 소스 구현은 운영 배포를 뜻하지 않습니다.

## Prompt guard 계약

`Check` 호출은 `Source`와 `Enforcement`를 모두 명시해야 합니다. `EnforcementUnspecified`와 알 수 없는 값은 탐지나 cache 조회 전에 `ErrInvalidCheckRequest`로 거부됩니다.

| `Enforcement` | `Allow` | `Review` | `Block` |
|---|---|---|---|
| `EnforcementObserve` | 허용 | 허용 | 허용 |
| `EnforcementInteractive` | 허용 | 허용 | 거부 |
| `EnforcementPersistent` | 허용 | 거부 | 거부 |

지원 source는 `user_prompt`, `prompt_bundle`, `retrieved_memory`, `memory_candidate`, `session_patch`, `simulation_state`, `law_context`, `session_context`, `chat_log`, `web_search_result`, `image_prompt`로 고정됩니다. 저장·요약·재사용되는 데이터에는 `EnforcementPersistent`를 사용해야 합니다.

`UseEmbeddedDefaults=true`는 `pkg/promptguard/rulepacks`의 v3 baseline을 먼저 로드합니다. `RulepacksDir` 또는 `RulepackFS`를 함께 지정하면 v3 `kind: rules` overlay 하나만 추가할 수 있으며, policy 변경과 baseline rule ID 중복은 시작 단계에서 거부됩니다. `PolicyDigest()`는 engine version과 최종 유효 policy/rules의 결정론적 digest이며 사용자 입력을 포함하지 않습니다. Runtime override 없이 v3 policy 문서만 threshold를 소유합니다.

원문과 메시지 경계는 순차 검사하고, 규칙별 `max_occurrences`만큼의 근거와 고정 크기 경계
윈도우만 보존합니다. Markdown 코드·인용·메시지 경계 개수 자체로는 차단하지 않습니다.
전체 입력 byte 한도(`MaxInputBytes`, 기본 8 MiB)는 검사 전에 적용합니다.
독립된 디코딩 영역의 원문·변환 선택은 문자열 그래프로 보존합니다. 필수 리터럴 검사는
검사할 규칙을 고르는 데만 쓰며, 실제 정규식 프로그램으로 그래프를 검사합니다. 해석 영역이 없는 루트는
그래프를 훑지 않고, 길이 상한이 있는 정규식은 해석 선택 간선에 닿을 수 없는 위치에서 새 일치를
시작하지 않습니다. 점수는 구체적인 해석 문자열을 기존 규칙 매처로 재검증한 뒤에만 반영합니다.
영역 사이의 조합을 전체 문자열 후보로 펼치지 않으며, 원문 우선순위·규칙별 occurrence·정규화 경계를 보존합니다.
독립 영역 경로는 후보 개수·변환 단계·누적 스캔 횟수·국소 후보 용량으로 완료를 중단하지
않습니다. 대신 변환 4 MiB, 문맥 16 MiB, 미캐시 변환 시도 `2^18`의 기존 작업 예산을
공유합니다. 같은 Base64 경계 후보의 디코딩 결과는 요청 안에서 재사용하되 위치·문맥별
판정은 재사용하지 않습니다. 판독 불가가 확정된 Base64 창은 그 4자 그룹까지 접두가 같은 창과
결과를 공유하므로, 끝 번호만 다른 식별자 행이 반복돼도 행마다 예산을 쓰지 않습니다.
같은 텍스트의 독립 영역은 요청 안에서 한 번만 해석하고 위치만 바꿔 근거를 전달합니다.
실제 경계 열거가 잘리거나 작업 예산이 소진되면 미완료입니다.
압축·비텍스트 Base64의 의미 투영과 단일 영역·결합 문맥의 기존 디코더 한도는 유지합니다.
`raw` 규칙의 디코딩 사전 필터와 치환 경계 검사도 원문 매처의 ASCII 대소문자 대체 경로를 보존합니다.
실제 미완료는 계속 `DecodeIncomplete`로 보고하고 `EnforcementPersistent`에서 거부합니다.

v3 규칙의 `exclude`는 대소문자를 구분하는 원문 정규식 목록입니다. 일치한 정상 문맥(단어 내부,
코드 식별자, 부정·서술형, 방법·예시를 묻는 요청)을 같은 수의 중립 rune으로 가린 뒤 적중을 다시
확인합니다. 정규식에 묶음이 있으면 첫 묶음만 가리고 나머지 일치는 예외 문맥의 조건으로만
씁니다. 부정한 동사만 지워 같은 문장의 다른 요청 동사가 비밀값 명사와 다시 짝지을 수 있게 합니다.
`sentence_break`(0~64)는 두 단어 이상인 문장 끝의 문장부호와 뒤따르는 공백을 그 수의
중립 rune으로 바꿔, 근거가 문장을 건널 때 정규식 간격을 더 쓰게 합니다. 한 단어씩 끊은 문장부호와
문장부호 없는 줄바꿈은 문장 경계가 아닙니다. 두 필드는 길이 상한이 있는 `regex` 규칙과 `raw`가
아닌 view에서만 허용합니다. 결합 세그먼트에서는 마지막 조각 경계를 가로지르는 근거만 다시
확인합니다. 조각마다 가린 view를 결합 view와 같은 방식으로 이어 비교하므로 조각 경계를 가로지른
예외 문맥은 쓰지 않습니다. 가린 view는 한 요청 안에서 조각과 가림 설정마다 한 번만 만들고, 가린 원문이
같으면 설정이 달라도 정규화한 view를 공유합니다. 예외 정규식은 필수 리터럴이 원문에 없으면 실행하지 않습니다.
규칙의 필수 리터럴 사전 필터는 모든 규칙의 리터럴을 합친 오토마톤으로 세그먼트 view마다 한 번만 찾습니다.
리터럴이 512개를 넘으면 오토마톤을 쓰지 않고 규칙마다 문자열을 검색해 같은 판정을 하므로 검사만 느려집니다.

일본어 규칙(`injection-ja.yml`)은 지시 무시와 시스템 프롬프트 유출 두 가지만 `raw` view에서 봅니다.
정규화의 혼동 문자 치환이 가타카나를 다른 글자로 바꾸기 때문입니다. 반각 가타카나로 비튼 입력과
비밀값 요청·탈옥·출력 고정 같은 다른 일본어 공격은 아직 보지 않습니다.

정규화는 ASCII 대문자 `I`를 `i`로 읽되, ASCII 소문자 바로 뒤의 `I`는 `l`을 흉내 낸 위장(`poIicy`,
`reveaI`, `aII`)으로 보고 `l`로 읽습니다. 뒤에 `n`·`g`가 오는 camelCase 경계(`previousInstructions`,
`pleaseIgnore`)는 `i`로 남깁니다. JSON 문자열의 `\n`·`\r`·`\t`는 실제 줄바꿈·탭으로 풀어
디코딩 영역을 나눕니다. 줄 단위로 감싼 Base64 행만 잇습니다. 소문자 단어 전체가 글자·숫자 없는
바이트로만 풀리면 Base64로 읽지 않고, 독립 영역의 내부 Base64 창은 8자 이상만 봅니다. 손상·절단된
압축 스트림은 해제된 앞부분이 읽을 수 있는 텍스트일 때만 해석합니다. 숫자만인 토큰은 Base64로
읽지 않습니다. hex는 `hex:` 표지가 붙은 값과, 영문·숫자·밑줄 단어 전체가 16자리 이상의 짝수 자리
hex(`0x` 접두 허용)이고 글자가 있는 읽을 수 있는 텍스트로 풀리는 표지 없는 토큰을 해석합니다.
해시·ID처럼 무작위 바이트로 풀리는 값은 원문으로만 검사합니다. 표지 뒤 바이트 쌍은 구분자 없이
붙여 써도 됩니다. 긴 hex 숫자 줄은 앞 줄도 hex 줄일 때만 Base64 감싼 행처럼 잇습니다.
ASCII와 한글 음절만 있는 글은 전체 정규화 경로와 같은 결과를 내는 빠른 경로로 정규화합니다.

## Benchmark와 결정적 guard 검증

Go benchmark 함수는 명시적인 성능 조사와 프로파일링을 위해 유지합니다. 필요할 때 표준 Go 명령으로 직접 실행합니다.

```bash
go test -run '^$' -bench . ./pkg/promptguard ./pkg/outputguard
```

Blocking CI는 머신별 baseline이나 상대 wall-clock 비교를 사용하지 않습니다. Prompt/output guard의 의미론·적대적 입력·fail-closed 계약은 일반 테스트가 소유하며, 운영상 명시적으로 보호하던 prompt guard allocation 상한은 `testing.AllocsPerRun` 기반 결정적 테스트로 검증합니다. `ns/op`와 `B/op`는 수동 조사 지표이며 CI 합격 판정으로 사용하지 않습니다.

Prompt guard는 v3 rulepack과 source/enforcement를 명시하는 `Check`만 지원합니다.

## Inbox 업무 복구 transaction

`pgstore.InboxRecoveryDecision`과 `Store.ApplyInboxRecovery`는 소비자가 잠근 inbox·업무 행을
판정한 뒤 공용 inbox 상태 변경만 한 bulk SQL로 적용합니다. `Store`는 기존 transaction에
묶어야 하며 pool에서 자동 transaction을 열지 않습니다. 소비자는 선택·업무 행을 잠그고
미청구·만료 inbox 또는 자신이 소유한 처리 claim만 판정합니다. 살아 있는 업무 lease의 제외와
업무 정산·환불·metadata 변경도 같은 transaction에서 소유합니다.

판정은 `pending`·`completed`·`manual_review`만 허용합니다. scope·ID·message ID·attempts와
processing token·정확한 lease snapshot을 대조하며, 빈 token은 token·lease가 NULL인 pending만
바꿉니다. `pending` 사유는 비어야 하고 `manual_review`는 사유가 필요합니다.
모든 입력을 SQL 전에 검증하고, 영향 행 수가 판정 수와 다르면 `ErrClaimLost`를 반환합니다.
일부 행은 이미 바뀌었을 수 있으므로 **오류가 나면 호출자가 전체 transaction을 rollback**해야 합니다.
완료 payload는 지우고 수동 검토 payload는 `DiscardInboxManualReviewPayload` 정책을 따릅니다.

## 로컬 검증 (Verification)

개발 시 로컬에서 아래 명령어로 검증할 수 있습니다.

```bash
make lint
go test ./...
go build ./...
```

**CI 정책:** 본 리포지토리는 원격 깃허브 액션(GitHub Actions)이 실제 검증 주체입니다. `ci.yml`은 PR 및 `main` push마다 module tidy drift, 고정 Unicode 생성 테이블, SQL ownership 검사, `golangci-lint`(gofumpt·gci·goimports formatter와 govet 포함), 경합 조건 검사를 포함한 테스트 슈트(`go test -race -count=1 ./...`)를 수행하며, `security.yml`은 `main` push·주간 스케줄·수동 dispatch 시 `govulncheck` 취약점 분석을 수행합니다.

파일 줄 수(800)와 함수 길이 상한은 `.golangci.yml`의 revive `file-length-limit`과 `funlen`이 검사합니다. 별도 구조 예산은 두지 않습니다. Workflow 보안 정책(action 전체 SHA 고정, `id-token`·`attestations` 권한은 `release.yml`만, PR의 secret 사용 금지)은 iris-stack의 `tools/checks/check-workflow-policy.py`가 검증합니다. 로컬 릴리스 게이트도 `make test-allocations`로 race 빌드에서 빠지는 할당 상한을 검증합니다.

릴리스 검증(`bash scripts/ci/release-gate.sh`)은 Docker가 준비된 환경에서 별도 일회용 PostgreSQL 컨테이너를 소유합니다. `pkg/irisdurable/...`의 race 테스트를 실제 DB로 실행하고 필수 pgstore 사례의 pass와 전체 skip=0을 확인합니다. 컨테이너를 준비할 수 없으면 릴리스 검증은 실패합니다.
