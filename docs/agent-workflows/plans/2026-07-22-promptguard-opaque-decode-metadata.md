# PromptGuard Opaque Decode Metadata Implementation Plan

> **For Codex agents:** Use inline execution only. Subagents are not authorized for this task.

**Goal:** Allow confirmed opaque binary Base64 envelopes inside large prompt bundles without weakening readable, unknown, nested, or exhausted decode blocking.

**Architecture:** Classify opaque Base64 envelopes and replace them with a bounded separator in the rule-decoder projection, while raw prompt evaluation remains unchanged. Existing `decodedContextCandidate` transform metadata remains authoritative; the rule decoder may enqueue a non-matching intermediate only when that decoded replacement exposes another supported short decode surface. Expansion-only queue entries keep the existing depth and scan bounds and never consume result candidate or byte budgets.

**Tech Stack:** Go 1.26, `pkg/internal/guardtext`, `pkg/promptguard`, ChatBotGo prompt bundle consumer.

---

### Task 1: Lock opaque and security behavior

**Files:**
- Create: `pkg/internal/guardtext/decode_base64_opaque_test.go`
- Create: `pkg/internal/guardtext/decode_rule_nested_test.go`
- Create: `pkg/promptguard/guard_opaque_base64_test.go`

- [x] Add confirmed PNG and Factorio version-byte + zlib classification tests.
- [x] Add a large Factorio-shaped prompt bundle plus unrelated short Base64 context; require complete allow.
- [x] Add readable instruction text behind an image media type; require rule block.
- [x] Add unknown binary/readable nested Base64 and hex cases; retain inspection and blocking.
- [x] Add nested standard-to-short composition and depth-exhaustion tests.
- [x] Run focused tests against the current implementation and record the expected failures.

### Task 2: Make opaque metadata authoritative

**Files:**
- Create: `pkg/internal/guardtext/decode_base64_opaque.go`
- Modify: `pkg/internal/guardtext/decode_base64.go`
- Modify: `pkg/internal/guardtext/decode_filtered.go`
- Modify: `pkg/internal/guardtext/decode_rule_base64.go`
- Modify: `pkg/internal/guardtext/decode_rule_api.go`

- [x] Recognize only confirmed binary signatures, non-text data URI payloads, and validated Factorio `0` + zlib blueprint framing as opaque.
- [x] Keep readable Base64 and unknown binary on the conservative existing path.
- [x] Build a rule-only projection that replaces opaque spans with one separator; do not mutate raw prompt evaluation.
- [x] Skip opaque spans in standard, contextual, protected, and short Base64 enumeration across BFS depth.

### Task 3: Preserve nested transforms without a second decoder owner

**Files:**
- Create: `pkg/internal/guardtext/decode_rule_expansion.go`
- Modify: `pkg/internal/guardtext/decode_context.go`
- Modify: `pkg/internal/guardtext/decode_rule_context.go`

- [x] Reuse the transform kind and replacement range emitted by the existing `decodedContextCandidate` owner.
- [x] Defer only standard Base64/hex intermediates whose decoded replacement exposes a supported short decode surface.
- [x] Keep expansion-only entries out of `DecodeResult.Candidates` and result byte accounting.
- [x] Enforce existing `maxDecodeDepth` and `maxDecodeScans`; do not add or raise limits.
- [x] Avoid admission-time transform rediscovery, lint suppression, and a parallel recursive decoder.

### Task 4: Verify and publish

**Files:**
- Update: `CHANGELOG.md`
- Update consumer: `../chat-bot-go-kakao/go.mod`, `go.sum`

- [x] Run focused guardtext/promptguard/outputguard tests.
- [x] Add and run the ChatBotGo prompt bundle consumer tests against the released module.
- [x] Run the final-state `make lint`, `make test`, `make test-race`, `make vulncheck`, `make guard-perf-gate`, and `make build` suite.
- [x] Run `bash scripts/ci/release-gate.sh` before publishing `shared-go`.
- [x] Commit, push, tag a new `shared-go` release, then bump ChatBotGo without `replace` or vendoring.
- [x] Run ChatBotGo canonical build and pre-commit gate, push, redeploy `bot-chatbotgo`, and verify health/readiness/runtime plus filtered block metrics/logs.

### Stop rules

- Stop before publish if any readable or unknown encoded attack changes from block/fail-closed to allow.
- Stop before deploy if standalone `GOWORK=off` consumer tests do not use the released module version.
- Stop and report if release/tag state diverges remotely or the deploy helper detects migration, image, secret-render, or health failure.
