# Decode Incomplete Release and Deploy Handoff

이 문서는 다음 Codex 세션에 그대로 전달할 실행 프롬프트입니다.

## Outcome

`decode_incomplete`의 두 독립 원인을 보안 경계를 낮추지 않고 근본 수정한 현재 변경을 검토·릴리스하고, ChatBotGo 소비자 회귀 테스트와 모듈 버전을 갱신한 뒤 `bot-chatbotgo`를 재배포하십시오.

사용자가 명시한 불변식은 다음과 같습니다.

- `$nfr-gate`와 `$tech-debt-guardrails`를 준수할 것.
- 보안을 약화하거나 `decode_incomplete` fail-closed 정책을 우회하지 말 것.
- threshold·candidate/byte/depth/scan 한도를 올리지 말 것.
- lint suppression, `replace`, vendoring, fork, 새 dependency로 해결하지 말 것.
- 읽을 수 있는 encoded text, 미확인 binary, budget/depth exhaustion은 계속 보수적으로 차단할 것.

참조 분석 문서를 먼저 읽으십시오.

- `/home/kapu/work/iris-stack/docs/2026-07-22-chatbotgo-promptguard-opaque-base64-type1-analysis.md`
- `/home/kapu/work/iris-stack/shared-go/docs/agent-workflows/plans/2026-07-22-outputguard-decode-completeness.md`
- `/home/kapu/work/iris-stack/shared-go/docs/agent-workflows/plans/2026-07-22-promptguard-opaque-decode-metadata.md`

## Authorization and gates

사용자는 다음 범위의 실행을 명시적으로 승인했습니다.

1. `shared-go` 현재 변경의 local commit, `origin/main` push, 다음 patch tag push.
2. ChatBotGo의 `shared-go` 버전 갱신, consumer test 추가, local commit과 `origin/main` push.
3. `/home/kapu/work/iris-stack/chat-bot-go-kakao/scripts/chatbotgo-redeploy.sh`를 통한 `bot-chatbotgo` 재배포와 read-only health/readiness/log 확인.

승인은 raw secret 조회·출력, destructive DB operation, threshold 완화, draft PR #36 수정/종료까지 확장되지 않습니다. 원격 branch/tag가 아래 기준에서 diverge했거나 배포 helper가 migration/image/secret-render/health 오류를 보고하면 해당 write 전에 중단하고 보고하십시오. Git hook을 우회하지 마십시오.

## Current workspace state

### shared-go

- Worktree: `/home/kapu/work/iris-stack/shared-go`
- Branch: `main`
- Local HEAD: `05e603807d71f7fb6841d00f906ebb2471a7c146`
- `origin/main`: `05e603807d71f7fb6841d00f906ebb2471a7c146`
- 상태: 의도적인 미커밋 구현·테스트·문서 변경이 있음. 절대 discard, reset, checkout, rebase하지 말 것.

현재 write scope:

- `CHANGELOG.md`
- `docs/agent-workflows/plans/2026-07-22-outputguard-decode-completeness.md`
- `docs/agent-workflows/plans/2026-07-22-promptguard-opaque-decode-metadata.md`
- `docs/agent-workflows/handoffs/2026-07-22-decode-incomplete-release-deploy-handoff.md`
- `pkg/internal/guardtext/decode.go`
- `pkg/internal/guardtext/decode_base64.go`
- `pkg/internal/guardtext/decode_base64_opaque.go`
- `pkg/internal/guardtext/decode_base64_opaque_test.go`
- `pkg/internal/guardtext/decode_context.go`
- `pkg/internal/guardtext/decode_context_test.go`
- `pkg/internal/guardtext/decode_filtered.go`
- `pkg/internal/guardtext/decode_rule_api.go`
- `pkg/internal/guardtext/decode_rule_base64.go`
- `pkg/internal/guardtext/decode_rule_context.go`
- `pkg/internal/guardtext/decode_rule_expansion.go`
- `pkg/internal/guardtext/decode_rule_nested_test.go`
- `pkg/outputguard/contextual_decode_benchmark_test.go`
- `pkg/outputguard/contextual_decode_test.go`
- `pkg/outputguard/outputguard.go`
- `pkg/promptguard/guard_opaque_base64_test.go`

### chat-bot-go-kakao

- Worktree: `/home/kapu/work/iris-stack/chat-bot-go-kakao`
- Branch: `main`
- Local HEAD: `37bee419d7fb0ac544723ed5137fedc09a8a7598`
- `origin/main`: `771f8a066ee7568388f2a7e328d8b0154ee966bc`
- 상태: clean, local `main`이 1 commit ahead.
- 기존 local commit `37bee419d fix(answer): 웹 검색 출처 표시 신뢰 경계 보강`은 보존하십시오. amend/rebase/drop하지 말고, 의존성 반영 commit과 함께 push하십시오.
- 현재 `go.mod`/`go.sum`은 `github.com/park285/shared-go v1.32.2`입니다.

ChatBotGo의 기본 write scope는 아래로 제한합니다.

- `internal/bot/promptcomposer/*_test.go`의 focused prompt-bundle consumer regression test
- 필요한 경우 별도 focused test file 하나
- `go.mod`
- `go.sum`

consumer test가 실제 production owner 문제를 드러낼 때만 최소 production fix로 scope를 확장하고 그 근거를 보고하십시오.

## Implemented design

1. OutputGuard Type II:
   - restricted rule decoder는 실제 rule 기여 후보만 result budget에 admission합니다.
   - 반복 citation Base64와 HTTP(S) URL path가 false short fragment를 만들어 candidate budget을 소진하던 경로를 제거했습니다.
   - raw protected overlap fast path와 ASCII restricted-anchor preflight를 추가했습니다.
   - 실제 뒤쪽의 encoded restricted/protected 공격 및 budget exhaustion은 계속 fail closed입니다.

2. PromptGuard Type I:
   - 확인된 binary signature, unreadable non-text data URI, 그리고 실제 `{"blueprint":...}`/`{"blueprint_book":...}` prefix를 zlib stream으로 검증한 Factorio `0` envelope만 opaque로 분류합니다.
   - opaque span은 rule-only projection에서 한 칸 separator로 치환되며 raw evaluation은 바뀌지 않습니다.
   - readable payload, image media type 뒤의 readable instruction, unknown binary, unframed zlib, Factorio처럼 보이는 non-blueprint zlib는 opaque가 아니며 계속 검사·차단됩니다.

3. Nested transform:
   - 기존 `decodedContextCandidate` metadata가 transform owner입니다.
   - standard Base64/hex decoded replacement가 다시 short decode surface를 노출할 때만 expansion-only queue entry를 enqueue합니다.
   - expansion-only entry는 `DecodeResult.Candidates`와 result byte budget을 쓰지 않지만 기존 depth/scan limit은 그대로 적용됩니다.
   - 별도 recursive decoder나 admission-time transform 재추론은 없습니다.

Draft PR `park285/shared-go#36`의 branch `agent/fix-promptguard-nested-context-decode`는 behavior 참고용일 뿐입니다. 그 구현은 transform owner를 이중화하고 lint complexity 문제가 있으므로 merge/cherry-pick하지 마십시오. PR 수정이나 종료도 이번 scope가 아닙니다.

## Evidence already collected

최종 코드 상태에서 다음이 통과했습니다.

```bash
cd /home/kapu/work/iris-stack/shared-go
go test ./pkg/internal/guardtext ./pkg/promptguard ./pkg/outputguard -count=1
make lint
make test
make test-race
make vulncheck
make guard-perf-gate
make build
git diff --check
```

주요 보안/정상 경로 검증:

- 8 KiB 초과 Factorio blueprint + unrelated short Base64 context: complete allow.
- opaque PNG data URI: complete allow.
- readable instruction behind `data:image/png`: complete rule block.
- unknown/unframed zlib와 Factorio-like non-blueprint: conservative `DecodeIncomplete` block.
- nested Base64/hex 및 percent/HTML/JSON boundary composition: complete block.
- beyond-depth nested input: `DecodeDepthLimit` fail closed.
- citation-heavy OutputGuard consumer shape: allow; 뒤쪽 encoded attack: block.

성능 게이트는 첫 실행에서 `BenchmarkPromptGuardDecoderHeavy`가 `74 allocs/op`로 absolute budget `64`를 초과했습니다. Base64 opaque probe를 fixed stack buffer로 바꿔 `64 allocs/op`로 낮춘 후 11개 benchmark strict gate가 통과했습니다. 예산 또는 baseline은 수정하지 않았습니다.

아직 하지 않은 것:

- `bash scripts/ci/release-gate.sh`
- shared-go commit/push/tag
- ChatBotGo PromptGuard production bundle consumer test
- ChatBotGo module bump와 `GOWORK=off` 검증
- ChatBotGo commit/push/redeploy/runtime verification

현재 ChatBotGo의 아래 standalone test는 `v1.32.2`를 사용하므로 수정 전 결함을 재현해 실패하는 것이 예상됩니다. 새 release bump 뒤에는 반드시 통과해야 합니다.

```bash
cd /home/kapu/work/iris-stack/chat-bot-go-kakao
GOWORK=off go test ./internal/bot/generationprotect \
  -run '^TestGenerationProtectionAllowsStructuredCitationsWithEncodedTrackingMetadata$' -count=1
```

## Required continuation

1. 양쪽 저장소의 가장 가까운 `AGENTS.md`와 필요한 skills(`executing-plans`, `verification-before-completion`, `git-pr-conventions`, `chatbotgo-ops`)를 다시 읽으십시오.
2. 위 commit/remote 기준과 dirty files를 재확인하십시오. 관련 없는 변경이 새로 생겼으면 보존하고 충돌 전까지 진행하십시오.
3. shared-go diff 전체를 correctness/security/complexity 관점에서 review하고 `git diff --check`를 재확인하십시오. threshold, public API, schema, dependency 변화가 없어야 합니다.
4. ChatBotGo `internal/bot/promptcomposer`에 실제 `Composer.validatePromptBundle`/production `promptguard.Guard`를 통과하는 대형 Factorio blueprint + normal assistant/session context regression test를 추가하십시오. readable disguised attack case도 함께 검증하십시오. 먼저 workspace module로 focused test를 실행하십시오.
5. shared-go에서 `bash scripts/ci/release-gate.sh`를 실행하십시오. 실패하면 원인을 수정하고 모든 영향받은 final-state gate를 다시 실행하십시오. threshold/baseline 완화는 금지합니다.
6. remote tag를 조회해 충돌이 없으면 다음 patch version `v1.32.3`을 사용하십시오. staged diff를 검토한 뒤 한국어 Conventional Commit으로 commit하고 normal hooks를 통과시키십시오. 예시: `fix(guard): 인코딩 오탐과 중첩 우회 차단`.
7. `origin/main`이 기준 commit을 유지하는지 다시 확인한 뒤 shared-go main과 tag를 push하고 원격 commit/tag를 검증하십시오. GitHub Release object는 repository 관례가 요구하지 않으면 만들지 마십시오.
8. ChatBotGo에서 `go get github.com/park285/shared-go@v1.32.3`으로 갱신하십시오. `replace`를 추가하지 마십시오. 아래를 최소 검증하십시오.

```bash
cd /home/kapu/work/iris-stack/chat-bot-go-kakao
GOWORK=off go test ./internal/bot/generationprotect ./internal/bot/promptcomposer -count=1
./scripts/build-go-binaries.sh
./scripts/pre-commit-go-checks.sh
```

9. `go.mod`, `go.sum`, consumer test만 우선 stage하여 diff를 확인하고 한국어 Conventional Commit으로 commit하십시오. 예시: `fix(deps): shared-go v1.32.3 guard 수정 반영`. 기존 ahead commit과 새 commit을 force 없이 `origin/main`에 push하고 remote HEAD를 검증하십시오.
10. `chatbotgo-ops` runbook에 따라 secret 값을 읽거나 출력하지 않고 preflight를 수행한 뒤 아래 helper로 재배포하십시오.

```bash
cd /home/kapu/work/iris-stack/chat-bot-go-kakao
./scripts/chatbotgo-redeploy.sh --timeout 120
```

11. Compose status와 deployed revision/module metadata를 확인하고 `/health`, `/ready`, `/ready/runtime`를 검증하십시오. payload나 secret을 출력하지 않는 filtered logs에서 startup/migration/health 오류와 `decode_incomplete`/output recheck block reason만 확인하십시오. metrics token 원문은 읽지 마십시오.
12. shared plan의 남은 checklist를 실제 evidence가 생긴 뒤에만 완료 처리하십시오. 릴리스·배포 후 fresh verification 결과, tag/commit, runtime 상태, 남은 draft PR #36 상태를 최종 한국어 응답에 간결히 보고하십시오.

## Stop conditions

- readable 또는 unknown encoded attack이 allow로 바뀌면 publish하지 마십시오.
- `GOWORK=off` consumer tests가 정확한 release version을 사용하지 않거나 실패하면 deploy하지 마십시오.
- shared-go/ChatBotGo remote branch 또는 `v1.32.3` tag가 예상과 다르면 push 전에 중단하십시오.
- release gate, hook, canonical ChatBotGo gate, migration, rendered config, image build, health/readiness 중 하나라도 실패하면 우회하지 말고 원인과 마지막 안전 상태를 보고하십시오.
