#!/usr/bin/env bash
set -euo pipefail

for variable in GO_TOOLING GO_LINT_MATRIX GOLANGCI_LINT GOVULNCHECK \
  GOLANGCI_LINT_BIN GOVULNCHECK_BIN GOLANGCI_LINT_VERSION GOVULNCHECK_VERSION; do
  if [[ -v "$variable" ]]; then
    printf 'unsupported Go tool override: %s\n' "$variable" >&2
    exit 2
  fi
done

GOLANGCI_LINT_VERSION="v2.15.0"
GOVULNCHECK_VERSION="v1.10.0"
readonly GOLANGCI_LINT_VERSION GOVULNCHECK_VERSION

go_tool_version() {
  case "$1" in
    golangci-lint) printf '%s\n' "$GOLANGCI_LINT_VERSION" ;;
    govulncheck) printf '%s\n' "$GOVULNCHECK_VERSION" ;;
    *) printf 'unknown Go tool: %s\n' "$1" >&2; return 2 ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  return 0
fi

unset CDPATH GNUMAKEFLAGS MAKEFLAGS MFLAGS MAKEOVERRIDES
export GOWORK=off
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX

if [[ "${FULL_PRE_PUSH:-false}" != "true" && -n "${BASE_SHA:-}" && -n "${HEAD_SHA:-}" ]]; then
  if git diff --quiet --no-ext-diff "${BASE_SHA}" "${HEAD_SHA}" -- . ':(exclude)*.md'; then
    echo "[pre-push] documentation-only change; skipping shared-go gate"
    exit 0
  else
    diff_status=$?
    [[ "${diff_status}" == 1 ]] || exit "${diff_status}"
  fi
fi

echo "──── stack lint rules and comment ban (ast-grep, SQL, TLA+, Lean) ────"
bash scripts/ci/verify-tools.sh scan
make lint MODULES= GO_MODULE_DIR=
echo "──── formal verification (TLC, Lean) ────"
bash scripts/ci/verify-tools.sh formal
