#!/usr/bin/env bash
# release-gate: shared-go release 전 로컬 전용 풀 품질 게이트.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
cd "${ROOT_DIR}"

echo "════════════════════════════════════════"
echo "  shared-go release full gate"
echo "════════════════════════════════════════"

run_stage() {
  echo "[release] $*"
  "$@"
}

run_stage bash scripts/check-sql-ownership.sh
run_stage bash scripts/ci/check-structure.sh --mode hard --format text
run_stage bash scripts/ci/check-release-provenance.sh
run_stage make lint
run_stage make test
run_stage make test-race
run_stage make vulncheck
run_stage go mod tidy -diff
run_stage go run ./pkg/internal/guardtext/genconfusables.go \
  -confusables-source ./pkg/internal/guardtext/testdata/confusables-17.0.0.txt.gz \
  -unicode-data-baseline-source ./pkg/internal/guardtext/testdata/UnicodeData-15.0.0.txt.gz \
  -unicode-data-source ./pkg/internal/guardtext/testdata/UnicodeData-17.0.0.txt.gz \
  -output ./pkg/internal/guardtext/confusables_table_generated.go \
  -check

echo "════════════════════════════════════════"
echo "  shared-go release full gate passed"
echo "════════════════════════════════════════"
