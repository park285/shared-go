# Generic Encoded Data and Structured Output Implementation Plan

> **For Codex agents:** Execute inline with `update_plan`; do not dispatch subagents unless the user authorizes them.

**Goal:** Remove application-specific encoded-payload trust rules, preserve normal large encoded data, block readable instructions inside supported encoded containers, and stop structured generated answers from being rejected solely by pre-parse decode incompleteness.

**Architecture:** `guardtext` owns one generic semantic-envelope decoder for declared non-text data and Base64-wrapped zlib/gzip streams, including Base64-alignment framing. It extracts bounded semantic text and admits only rule-contributing windows through existing candidate/depth/scan/byte limits. ChatBotGo skips raw-envelope OutputGuard only when strict structured output is configured, then validates parsed reply, storage, and artifact fields through the existing decoded-field guard.

**Tech Stack:** Go standard library, shared-go `guardtext`/`promptguard`/`outputguard`, ChatBotGo structured response pipeline and disposable integration E2E.

---

### Task 1: Generic encoded semantic owner

**Files:**
- Replace: `pkg/internal/guardtext/decode_base64_opaque.go`
- Modify: `pkg/internal/guardtext/decode_rule_api.go`
- Modify: `pkg/internal/guardtext/decode_context.go`
- Modify: `pkg/internal/guardtext/decode_base64.go`
- Modify: `pkg/internal/guardtext/decode_filtered.go`
- Modify: `pkg/internal/guardtext/decode_rule_base64.go`
- Test: `pkg/internal/guardtext/decode_base64_semantic_test.go`
- Test: `pkg/promptguard/guard_semantic_base64_test.go`

- [x] Add failing tests proving large generic framed compressed JSON is allowed, while the same envelope with a readable or nested encoded instruction is blocked.
- [x] Add source checks proving production guard code contains no application payload names or application-owned JSON keys.
- [x] Replace domain and file-signature allowlists with one bounded semantic decoder using standard MIME metadata and zlib/gzip framing only.
- [x] Preserve existing candidate, byte, depth, scan, and protected-work limits without increasing them.
- [x] Keep unknown binary and incomplete semantic inspection fail-closed.

### Task 2: Restricted-rule source of truth

**Files:**
- Modify: `pkg/outputguard/outputguard.go`
- Test: `pkg/outputguard/contextual_decode_test.go`

- [x] Remove the independent ASCII anchor allowlist from candidate admission.
- [x] Preserve the restricted-rule raw, Base64, and composed encoded regression corpus with `restrictedRules` as the only policy owner.

### Task 3: Structured generated-output owner

**Files:**
- Modify: `internal/bot/answer/ask_completion.go`
- Modify: `internal/bot/answer/pipeline.go`
- Modify: `internal/bot/answer/finalizer.go`
- Test: `internal/bot/answer/*_test.go`
- Test: `internal/securitye2e/target_integration_test.go`

- [x] Add failing streaming and non-streaming tests where a strict structured response is decode-incomplete-only at the raw envelope but all parsed fields are safe.
- [x] Skip raw OutputGuard only when `ResponseFormat.Strict` is true and structured output is enabled.
- [x] After strict parse, validate reply/storage/artifact with `ValidateDecodedStructuredText`; actual rule and protected-text matches must still block.
- [x] Keep unstructured responses on strict raw and final validation.

### Task 4: Release and runtime proof

- [ ] Run focused shared-go and ChatBotGo tests, lint, race, build, vulncheck, and canonical gates without performance benchmarks.
- [ ] Run the disposable signed-H3 E2E with ordinary large encoded data, malicious encoded semantic text, benign structured output, and protected-output attacks.
- [ ] Publish the next shared-go patch tag, bump ChatBotGo without `replace`, push force-free, redeploy through the helper, and verify revision plus `/health`, `/ready`, and `/ready/runtime`.

## Stop rules

- Do not publish if application-specific payload names or keys remain in production guard code.
- Do not publish if any actual rule/protected overlap becomes allowed.
- Do not publish if unknown binary or decode resource exhaustion stops failing closed.
- Do not increase thresholds, add suppression, add dependencies, or run performance benchmarks.
