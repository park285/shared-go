#!/usr/bin/env bash
set -euo pipefail

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
echo "──── formal verification (TLC, Lean) ────"
bash scripts/ci/verify-tools.sh formal
