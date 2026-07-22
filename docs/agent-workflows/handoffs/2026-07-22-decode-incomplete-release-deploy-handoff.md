# Decode Incomplete Root-Fix Release and Deploy Completion

## Outcome

`decode_incomplete` 계열의 입력·출력 오탐과 encoded payload 우회를 애플리케이션별 예외 없이 수정하고, shared-go patch release와 ChatBotGo 반영·배포까지 완료했습니다.

완료된 불변식은 다음과 같습니다.

- generic guard production 코드에는 특정 애플리케이션명, 애플리케이션 소유 JSON key, file-signature allowlist가 없습니다.
- 기존 candidate/byte/depth/scan/protected-work 한도를 올리지 않았습니다.
- unknown binary, unreadable compressed data, resource exhaustion은 계속 fail closed입니다.
- strict structured output의 raw JSON envelope는 OutputGuard 대상이 아닙니다.
- strict parse 뒤 사용자 표시·저장·artifact 필드는 `ValidateDecodedStructuredText`로 검사합니다.
- decoded field의 실제 restricted rule 또는 protected-text overlap은 계속 차단합니다.
- unstructured output은 raw와 final field 모두 strict validation을 유지합니다.
- `replace`, vendoring, fork, 새 dependency, lint suppression은 추가하지 않았습니다.
- 성능 benchmark와 performance gate는 실행하지 않았습니다.

구현 계획은 `docs/agent-workflows/plans/2026-07-22-generic-encoded-data-and-structured-output.md`에 있습니다.

## Root fixes

### shared-go

- `guardtext`가 declared non-text `data:` payload와 Base64-wrapped zlib/gzip readable text를 하나의 bounded semantic decoder로 처리합니다.
- 0–3 byte Base64 alignment framing은 protocol-level rule로 처리하며 애플리케이션별 prefix나 schema를 사용하지 않습니다.
- declared binary에서는 연속된 의미 텍스트만 bounded candidate로 추출합니다. 정상 fragmented binary와 rule에 기여하지 않는 대용량 압축 문서는 candidate budget을 소비하지 않습니다.
- readable text, nested Base64/hex instruction, restricted rule, protected-text overlap은 기존 rule owner로 전달됩니다.
- `OutputGuard`의 별도 ASCII anchor 목록을 제거해 `restrictedRules`만 정책 source of truth로 남겼습니다.

### ChatBotGo

- streaming과 non-streaming ask completion은 실제 `ResponseFormat.Strict`가 있을 때 raw structured envelope 검사를 strict parse 이후로 미룹니다.
- pipeline은 structured-output mode를 한 번 계산해 prompt composer와 parsed field validation에 동일하게 사용합니다.
- reply/storage 중복 검증 경로를 제거하고 `validateAnswerOutputs`가 reply, storage, artifact validation을 소유합니다.
- strict parse 실패는 기존 `PrepareAnswerOutput` 경계에서 차단됩니다.
- raw envelope가 `decode_incomplete`-only인 정상 structured answer는 reply되고, decoded field의 encoded restricted output은 차단됩니다.

## Published artifacts

### shared-go

- main commit: `e39746e` (`fix(guard): 인코딩 의미 검사 경계 일반화`)
- patch tag: `v1.32.4`
- tag target: `e39746e`
- 이전 application-specific implementation branch/PR은 병합하지 않았고 `main`에서 fix-forward했습니다.

### chat-bot-go-kakao

- main commit: `7f850a69` (`fix(answer): 구조화 출력 검증 경계 정합화`)
- dependency: `github.com/park285/shared-go v1.32.4`
- `replace`: 없음
- deployed full revision: `7f850a693facb6183878f2186875bdd8af7bf296`

다른 세션의 작업을 보존하기 위해 구현은 clean isolated clone에서 수행했습니다. 원본 shared-go checkout의 별도 `go.mod`/`go.sum` 변경은 수정하거나 폐기하지 않았습니다. ChatBotGo 원본 checkout은 배포 직전에 clean 상태를 확인한 뒤 `--ff-only`로 원격 main까지 전진시켰습니다.

## Verification

### shared-go final state

다음 non-performance gate가 통과했습니다.

```bash
make lint
make test
make test-race
make vulncheck
go build ./...
make tidy
git diff --check
```

검증한 주요 경로:

- 정상 대용량 framed compressed document: complete allow
- declared binary data URI와 대용량 fragmented binary: complete allow
- readable instruction inside declared binary or framed/unframed compressed text: complete rule block
- nested encoded instruction inside compressed document: complete rule block
- compressed restricted output and protected-text copy: complete block
- unknown binary and decode-limit exhaustion: fail closed
- restricted-rule corpus: raw/Base64/composed transform 차단 유지

### ChatBotGo final state

다음 gate가 통과했습니다.

```bash
CHATBOTGO_HOOK_LINT_MODE=full ./scripts/pre-commit-go-checks.sh
go test ./internal/bot/answer ./internal/bot/generationprotect ./internal/securitye2e -count=1
git push origin main
```

`git push`의 configured pre-push hook이 다음을 추가로 통과했습니다.

- release/workflow/shell/SQL/DB-access boundary gates
- responsibility and cross-cutting boundary gates
- canonical three-binary build
- full `golangci-lint` and `go vet`
- bot/session/simstate race suites
- disposable PostgreSQL-backed `go test -count=1 -p=1 ./...`
- execution-partition race suite
- `govulncheck ./...`: reachable vulnerability 0

Disposable signed-H3 integration E2E는 다음 항목을 한 target lifecycle에서 검증했습니다.

- 일반 질문 정상 reply
- session context 뒤의 정상 대용량 compressed document reply
- readable instruction behind declared binary media type 차단
- framed compressed readable instruction 차단
- raw structured envelope가 `decode_incomplete`-only인 정상 answer reply
- decoded `answer_body`에 Base64로 숨긴 restricted rule 차단

성능 benchmark는 실행하지 않았습니다.

## Runtime verification

배포 명령:

```bash
cd /home/kapu/work/iris-stack/chat-bot-go-kakao
./scripts/chatbotgo-redeploy.sh --timeout 120
```

배포 helper가 다음을 완료했습니다.

- effective rendered env key/direct-infra validation
- host canonical build와 image build
- PostgreSQL/Valkey health
- schema migration
- retired long-term-memory schema absence check
- `bot-chatbotgo` recreate
- container health와 H3 `/health`
- binary module metadata와 image provenance

배포 후 fresh 확인 결과:

- Compose: `bot-chatbotgo` `healthy`
- image revision: `7f850a693facb6183878f2186875bdd8af7bf296`
- `/health`: success
- `/ready`: success
- `/ready/runtime`: success
- 배포 후 filtered logs: `decode_incomplete`, `generation_blocked`, `answer_output_blocked`, panic, permission, x509, missing-file, deadline, timeout marker 없음

raw prompt, raw answer, session payload, secret 값은 조회하거나 출력하지 않았습니다.

## Completion status

이 handoff의 code, release, consumer integration, E2E, deploy, runtime verification 항목은 모두 완료됐습니다. 관련 작업을 이어갈 세션은 `v1.32.4`와 ChatBotGo `7f850a69`를 기준 상태로 사용하십시오.
