#!/usr/bin/env bash
set -euo pipefail

REPO_CI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "${REPO_CI_DIR}/../.." && pwd -P)"
REPO_PYTHON_RUNNER="${REPO_CI_DIR}/python-runner.sh"
REPO_PYTHON_VERSION="3.14.7"

repo_python_init() {
  local actual_version
  if [[ -z "${CI_PYTHON_BIN:-}" ]]; then
    CI_PYTHON_BIN="$("${REPO_PYTHON_RUNNER}" --print-interpreter)"
    CI_PYTHON_RUNTIME_ROOT="${REPO_ROOT}"
  fi
  if [[ "${CI_PYTHON_RUNTIME_ROOT:-}" != "${REPO_ROOT}" || ! -f "${CI_PYTHON_BIN}"     || ! -x "${CI_PYTHON_BIN}" ]]; then
    echo "python-runtime: selected interpreter does not belong to this repository" >&2
    return 1
  fi
  actual_version="$("${CI_PYTHON_BIN}" -I -S -c 'import platform; print(platform.python_version())')"
  if [[ "${actual_version}" != "${REPO_PYTHON_VERSION}" ]]; then
    echo "python-runtime: expected Python ${REPO_PYTHON_VERSION}, got ${actual_version:-unknown}" >&2
    return 1
  fi
  export CI_PYTHON_BIN CI_PYTHON_RUNTIME_ROOT
}
