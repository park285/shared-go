#!/usr/bin/env bash
# release-gate: shared-go release 전 로컬 전용 풀 품질 게이트.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"
export GOWORK=off
modules=("$@")
if ((${#modules[@]} == 0)); then
  modules=(. core postgres transport openai)
fi
for module in "${modules[@]}"; do
  case "$module" in
    .|core|postgres|transport|openai) ;;
    *) echo "release-gate: unsupported module" >&2; exit 2 ;;
  esac
done

echo "════════════════════════════════════════"
echo "  shared-go release full gate"
echo "════════════════════════════════════════"

run_stage() {
  echo "[release] $*"
  "$@"
}

for module in "${modules[@]}"; do
  run_stage make tidy-check MODULES="$module"
  if [[ "$module" == . ]]; then
    run_stage go run ./pkg/internal/guardtext/genconfusables.go \
      -confusables-source ./pkg/internal/guardtext/testdata/confusables-17.0.0.txt.gz \
      -unicode-data-baseline-source ./pkg/internal/guardtext/testdata/UnicodeData-15.0.0.txt.gz \
      -unicode-data-source ./pkg/internal/guardtext/testdata/UnicodeData-17.0.0.txt.gz \
      -output ./pkg/internal/guardtext/confusables_table_generated.go \
      -check
    run_stage make test-allocations
  fi
  run_stage bash scripts/check-sql-ownership.sh "$module"
  run_stage make build MODULES="$module"
  if [[ "$module" == postgres ]]; then
    run_stage bash scripts/ci/pgstore-release-gate.sh
  else
    run_stage make test-race MODULES="$module"
  fi
  run_stage make vulncheck MODULES="$module"
done

echo "════════════════════════════════════════"
echo "  shared-go release full gate passed"
echo "════════════════════════════════════════"
