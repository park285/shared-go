#!/usr/bin/env bash
set -euo pipefail

# .python-version이 고정한 CPython을 uv가 이미 설치한 해석기 중에서 offline으로 고른다.
# 자동 다운로드나 ambient python으로 대체하지 않는다.
UV_VERSION="0.12.18"
PYTHON_VERSION="3.14.7"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"

fail() {
  echo "python-runner: $*" >&2
  exit 1
}

[[ "$(cat "${ROOT_DIR}/.python-version" 2>/dev/null)" == "${PYTHON_VERSION}" ]] ||
  fail ".python-version must contain exactly ${PYTHON_VERSION}"
uv_version="$(uv --version 2>/dev/null || true)"
[[ "${uv_version}" == "uv ${UV_VERSION}" || "${uv_version}" == "uv ${UV_VERSION} "* ]] ||
  fail "uv ${UV_VERSION} is required, got ${uv_version:-none}"
interpreter="$(UV_NO_CONFIG=1 UV_PYTHON_DOWNLOADS=never \
  uv python find --offline --no-project --system --no-python-downloads "${PYTHON_VERSION}")" ||
  fail "uv did not resolve a provisioned CPython ${PYTHON_VERSION}"
[[ "$("${interpreter}" -I -S -c 'import platform; print(platform.python_version())')" == "${PYTHON_VERSION}" ]] ||
  fail "resolved interpreter is not CPython ${PYTHON_VERSION}"

case "${1:-}" in
  --print-interpreter) printf '%s\n' "${interpreter}" ;;
  --) shift; exec "${interpreter}" "$@" ;;
  *) echo "usage: $0 --print-interpreter | -- <python-args...>" >&2; exit 2 ;;
esac
