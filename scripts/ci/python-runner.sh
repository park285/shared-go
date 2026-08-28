#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd -P)"
UV_VERSION="0.12.7"
PYTHON_VERSION="3.14.7"
PIN_FILE="${ROOT_DIR}/.python-version"

fail() {
  echo "python-runner: $*" >&2
  exit 1
}

if [[ ! -f "${PIN_FILE}" || -L "${PIN_FILE}" ]]; then
  fail "required regular pin file is missing: .python-version"
fi

exec 3<"${PIN_FILE}"
if ! IFS= read -r pinned_python <&3; then
  exec 3<&-
  fail ".python-version must contain one newline-terminated exact version"
fi
if IFS= read -r _ <&3; then
  exec 3<&-
  fail ".python-version must contain exactly one line"
fi
exec 3<&-

if [[ "${pinned_python}" != "${PYTHON_VERSION}" ]]; then
  fail ".python-version=${pinned_python:-empty} does not match ${PYTHON_VERSION}"
fi

uv_path="$(command -v uv || true)"
if [[ -z "${uv_path}" || ! -x "${uv_path}" || -d "${uv_path}" ]]; then
  fail "uv ${UV_VERSION} is required"
fi
uv_version_output="$("${uv_path}" --version)"
case "${uv_version_output}" in
  "uv ${UV_VERSION}"|"uv ${UV_VERSION} "*) ;;
  *) fail "uv version mismatch: expected ${UV_VERSION}, got ${uv_version_output:-unknown}" ;;
esac

interpreter="$(
  UV_NO_CONFIG=1 UV_PYTHON_DOWNLOADS=never \
    "${uv_path}" python find \
      --offline \
      --no-project \
      --system \
      --no-python-downloads \
      "${PYTHON_VERSION}"
)"
if [[ -z "${interpreter}" || "${interpreter}" != /* || "${interpreter}" == *$'\n'*   || ! -f "${interpreter}" || ! -x "${interpreter}" ]]; then
  fail "uv did not resolve an executable provisioned CPython ${PYTHON_VERSION}"
fi

actual_version="$("${interpreter}" -I -S -c 'import platform; print(platform.python_version())')"
if [[ "${actual_version}" != "${PYTHON_VERSION}" ]]; then
  fail "resolved Python version mismatch: expected ${PYTHON_VERSION}, got ${actual_version:-unknown}"
fi

sync_project() {
  local mode="${1:?sync mode is required}"
  local project_interpreter project_version resolved_project resolved_selected
  local -a sync_args=(
    sync
    --directory "${ROOT_DIR}"
    --locked
    --no-python-downloads
    --python "${interpreter}"
  )
  if [[ "${mode}" == "offline" ]]; then
    sync_args+=(--offline)
  fi
  for project_file in pyproject.toml uv.lock; do
    if [[ ! -f "${ROOT_DIR}/${project_file}" || -L "${ROOT_DIR}/${project_file}" ]]; then
      fail "locked project sync requires regular ${project_file}"
    fi
  done
  (
    unset UV_PROJECT_ENVIRONMENT UV_PYTHON UV_PYTHON_DOWNLOADS
    UV_NO_CONFIG=1 UV_PYTHON_DOWNLOADS=never \
      "${uv_path}" "${sync_args[@]}" >&2
  )
  project_interpreter="${ROOT_DIR}/.venv/bin/python"
  if [[ -L "${ROOT_DIR}/.venv" || ! -f "${project_interpreter}" || ! -x "${project_interpreter}" ]]; then
    fail "locked project sync did not materialize .venv/bin/python"
  fi
  resolved_project="$(readlink -f "${project_interpreter}")"
  resolved_selected="$(readlink -f "${interpreter}")"
  if [[ -z "${resolved_project}" || "${resolved_project}" != "${resolved_selected}" ]]; then
    fail "locked project interpreter does not resolve to selected CPython"
  fi
  project_version="$("${project_interpreter}" -I -S -c 'import platform; print(platform.python_version())')"
  if [[ "${project_version}" != "${PYTHON_VERSION}" ]]; then
    fail "project Python version mismatch: expected ${PYTHON_VERSION}, got ${project_version:-unknown}"
  fi
  printf '%s\n' "${project_interpreter}"
}

case "${1:-}" in
  --sync-locked)
    if (( $# != 1 )); then
      fail "--sync-locked does not accept arguments"
    fi
    sync_project online
    exit 0
    ;;
  --sync-locked-offline)
    if (( $# != 1 )); then
      fail "--sync-locked-offline does not accept arguments"
    fi
    sync_project offline
    exit 0
    ;;
  --print-interpreter)
    if (( $# != 1 )); then
      fail "--print-interpreter does not accept arguments"
    fi
    printf '%s\n' "${interpreter}"
    exit 0
    ;;
  --check)
    if (( $# != 1 )); then
      fail "--check does not accept arguments"
    fi
    printf 'python-runner: uv=%s python=%s interpreter=%s\n'       "${UV_VERSION}" "${PYTHON_VERSION}" "${interpreter}"
    exit 0
    ;;
  --)
    shift
    ;;
esac

if (( $# == 0 )); then
  echo "usage: ${BASH_SOURCE[0]} [--check|--print-interpreter|--sync-locked|--sync-locked-offline|--] <python-args...>" >&2
  exit 2
fi

exec "${interpreter}" "$@"
