#!/usr/bin/env bash
set -euo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"
export GOWORK=off
unset LINT_GOWORK SOURCE_GOWORK GO_MODULE_DIR MODULES MAKEFLAGS MFLAGS MAKEOVERRIDES GNUMAKEFLAGS
discovered=$(bash scripts/ci/go-tooling.sh release-modules)
mapfile -t modules <<< "$discovered"
if (($#)); then
  for requested in "$@"; do
    for module in "${modules[@]}"; do
      if [[ "$requested" == "$module" ]]; then continue 2; fi
    done
    printf 'release-gate: unknown module: %s\n' "$requested" >&2
    exit 2
  done
  modules=("$@")
fi

echo "════════════════════════════════════════"
echo "  shared-go release full gate"
echo "════════════════════════════════════════"

run_stage() {
  echo "[release] $*"
  "$@"
}

run_stage bash scripts/ci/verify-tools.sh scan
run_stage bash scripts/ci/verify-tools.sh formal

for module in "${modules[@]}"; do
  printf '[release] module %s\n' "$module"
  run_stage make tidy-check MODULES="$module"
  if [[ "$module" == . ]]; then
    run_stage go run ./pkg/internal/guardtext/genconfusables.go \
      -confusables-source ./pkg/internal/guardtext/testdata/confusables-17.0.0.txt.gz \
      -unicode-data-baseline-source ./pkg/internal/guardtext/testdata/UnicodeData-15.0.0.txt.gz \
      -unicode-data-source ./pkg/internal/guardtext/testdata/UnicodeData-17.0.0.txt.gz \
      -output ./pkg/internal/guardtext/confusables_table_generated.go \
      -check
    run_stage make test-allocations MODULES="$module"
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
