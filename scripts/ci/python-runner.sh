#!/usr/bin/env bash
set -euo pipefail

# .python-version이 고정한 CPython을 uv가 이미 설치한 해석기 중에서 offline으로 고른다.
# 자동 다운로드나 ambient python으로 대체하지 않는다.
UV_MIN_VERSION="0.12.23"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"

fail() {
  echo "python-runner: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --print-interpreter | --print-version | -- <python-args...>" >&2
  exit 2
}
case "${1:-}" in
  --print-interpreter | --print-version) (( $# == 1 )) || usage ;;
  --) (( $# > 1 )) || usage ;;
  *) usage ;;
esac

pin_file="${ROOT_DIR}/.python-version"
[[ -f "${pin_file}" && ! -L "${pin_file}" ]] ||
  fail ".python-version must be a regular file"
# 개행으로 끝나는 버전 한 줄을 한 번 읽는다. sentinel은 명령 치환의 개행 제거를 막는다.
pin_content="$(cat -- "${pin_file}" && printf x)" || fail ".python-version could not be read"
pin_pattern=$'^([0-9]+[.][0-9]+[.][0-9]+)\nx$'
[[ "${pin_content}" =~ ${pin_pattern} ]] ||
  fail ".python-version must contain exactly one newline-terminated X.Y.Z version"
PYTHON_VERSION="${BASH_REMATCH[1]}"
if [[ "$1" == "--print-version" ]]; then
  printf '%s\n' "${PYTHON_VERSION}"
  exit 0
fi

uv_version="$(uv --version 2>/dev/null || true)"
[[ "${uv_version}" =~ ^uv\ ([0-9]+)\.([0-9]+)\.([0-9]+)(\ .*)?$ ]] ||
  fail "uv >= ${UV_MIN_VERSION} is required, got ${uv_version:-none}"
uv_major="${BASH_REMATCH[1]}"
uv_minor="${BASH_REMATCH[2]}"
uv_patch="${BASH_REMATCH[3]}"
IFS=. read -r uv_min_major uv_min_minor uv_min_patch <<< "${UV_MIN_VERSION}"
(( 10#${uv_major} > 10#${uv_min_major} ||
  (10#${uv_major} == 10#${uv_min_major} &&
    (10#${uv_minor} > 10#${uv_min_minor} ||
      (10#${uv_minor} == 10#${uv_min_minor} && 10#${uv_patch} >= 10#${uv_min_patch}))) )) ||
  fail "uv >= ${UV_MIN_VERSION} is required, got ${uv_version}"
interpreter="$(UV_NO_CONFIG=1 UV_PYTHON_DOWNLOADS=never \
  uv python find --offline --no-project --system --no-python-downloads "${PYTHON_VERSION}")" ||
  fail "uv did not resolve a provisioned CPython ${PYTHON_VERSION}"
[[ "$("${interpreter}" -I -S -c 'import platform; print(platform.python_version())')" == "${PYTHON_VERSION}" ]] ||
  fail "resolved interpreter is not CPython ${PYTHON_VERSION}"

if [[ "$1" == "--print-interpreter" ]]; then
  printf '%s\n' "${interpreter}"
else
  shift
  exec "${interpreter}" "$@"
fi
