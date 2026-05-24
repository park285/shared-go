Cross-runtime project canon for `hololive-bot/shared-go`.
Keep this file limited to always-needed subtree rules.

## Project Identity

This subtree is a pure shared Go library consumed across the monorepo.

## Working Defaults

1. Keep the module library-only with no `cmd/` packages.
2. Preserve API stability for downstream consumers.
3. Minimize global side effects.

## Verification Commands

```bash
make lint
go test ./...
go build ./...
```

## Subtree Rules

- Assume active parent and subagent work may legitimately take time and that sufficient computing power is available for their scoped work.
- Do not treat elapsed time alone as a reason to recall, restart, close, or abandon a running subagent or workstream.
- Treat subagent wait timeouts, empty wait results, and delayed completion messages as `running`, not terminal.
- Prefer longer waits or useful parallel parent work over recall churn while a needed subagent is still running.
- Add new packages under `pkg/`.
- Write Korean comments for exported elements when documentation is needed.
- Keep Valkey, logging, telemetry, and JSON helpers reusable and side-effect light.

## Reference

Use the root [AGENTS.md](../AGENTS.md) for monorepo-wide rules.
