#!/usr/bin/env bash
# release-gate: shared-go release 전 로컬 전용 풀 품질 게이트.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

echo "════════════════════════════════════════"
echo "  shared-go release full gate"
echo "════════════════════════════════════════"

run_stage() {
  echo "[release] $*"
  "$@"
}

run_stage bash scripts/check-sql-ownership.sh
run_stage make lint
run_stage make test
run_stage make test-race
run_stage make vulncheck
run_stage go mod tidy -diff

echo "════════════════════════════════════════"
echo "  shared-go release full gate passed"
echo "════════════════════════════════════════"
