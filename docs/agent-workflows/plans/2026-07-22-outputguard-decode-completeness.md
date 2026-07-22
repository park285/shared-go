# OutputGuard Decode Completeness Implementation Plan

> **For Codex agents:** Subagents are not authorized for this task. Execute inline with `update_plan` in the trusted workspace.

**Goal:** Eliminate benign `decode_incomplete` decisions at the `outputguard` owner while preserving fail-closed bounds and every encoded restricted-rule/protected-text detection contract.

**Architecture:** Route restricted-rule decoding through the existing rule-aware bounded decoder so only rule-contributing candidates consume the global candidate budget. Keep the protected-text decoder because it owns one-byte fragments and request-bound overlap semantics, but require ambiguous internal Base64 spans to contribute in their bounded surrounding context. Treat HTTP URL path separators as semantic token boundaries before bounded nested decoding. Keep `ReasonDecodeIncomplete` only for actual candidate, byte, depth, or scan exhaustion; do not add ChatBotGo exceptions, flags, URL stripping, or threshold inflation.

**Tech Stack:** Go 1.26, `pkg/outputguard`, `pkg/internal/guardtext`, ChatBotGo `internal/bot/generationprotect`.

---

### Task 1: Lock the false-positive and security contracts

**Files:**
- Modify: `pkg/outputguard/contextual_decode_test.go`
- Modify: `pkg/internal/guardtext/decode_context_test.go`

- [x] Replace the legacy expectation that nine independent benign Base64 values must fail closed with an allow expectation.
- [x] Add a restricted-rule case with benign decoys followed by encoded `system prompt` text; require `ReasonRoleBlock` without `ReasonDecodeIncomplete`.
- [x] Add bound-guard cases proving benign decoys and structured citation metadata pass while a later encoded protected fragment is still blocked.
- [x] Preserve an actual budget-exhaustion case that must retain `ReasonDecodeIncomplete`.
- [x] Record focused pre-fix failures for the benign and later-malicious cases.

### Task 2: Fix candidate ownership in `shared-go`

**Files:**
- Modify: `pkg/outputguard/outputguard.go`
- Modify: `pkg/internal/guardtext/decode_context.go`
- Modify: `pkg/internal/guardtext/decode_filtered.go`
- Modify: `pkg/internal/guardtext/decode_rule_base64.go`
- Modify: `pkg/internal/guardtext/decode.go`

- [x] Use `DecodeCandidatesWithContextForRules` for restricted output rules.
- [x] Remove the zero-consumer legacy matching API and whole-transform candidate bookkeeping that charged benign transforms to the candidate budget.
- [x] Require ambiguous embedded Base64 spans in the protected path to contribute within a bounded owner-supplied context window.
- [x] Split HTTP URL path runs on semantic `/` boundaries before Base64 expansion while continuing to inspect every path segment.
- [x] Preserve direct/raw matching and fail-closed candidate, byte, depth, scan, and protected-work limits.

### Task 3: Validate the ChatBotGo consumer without an application bypass

**Files:**
- Modify: `../chat-bot-go-kakao/internal/bot/generationprotect/protection_test.go`

- [x] Add a `GenerationProtection.Validate` regression for structured citations with repeated encoded tracking metadata.
- [x] Keep encoded protected text, role leak, and genuine `decode_incomplete` tests blocked.
- [x] Run `go test ./internal/bot/generationprotect ./internal/bot/answer -count=1` from ChatBotGo.

### Task 4: NFR and repository gates

**Files:**
- Modify: `pkg/outputguard/contextual_decode_benchmark_test.go`

- [x] Security/Reliability: run `go test -race -count=1 ./pkg/outputguard ./pkg/internal/guardtext` and the full shared-go test suite.
- [x] Performance: run the structured-citation benchmark and `make guard-perf-gate`; reject material regression.
- [x] Shared library: run `make lint`, `make test`, `make test-race`, `make vulncheck`, and `make build`.
- [x] ChatBotGo: run `./scripts/build-go-binaries.sh` and `./scripts/pre-commit-go-checks.sh` against the final workspace state.
- [x] Review final diffs for raw-content logging, new configuration, dependency/lockfile churn, compatibility breaks, or duplicated decode owners; none are allowed.

### Release boundary

The local multi-repository fix can be implemented and validated without approval. Publishing a `shared-go` release/tag, pushing either repository, updating ChatBotGo to a newly published module version, or redeploying `bot-chatbotgo` requires explicit authorization for those remote/live actions. Do not add a local `replace`, vendored fork, feature flag, or temporary bypass to cross this boundary.
