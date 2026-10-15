#!/usr/bin/env bash
set -euo pipefail
# 내려받은 byte를 실행 전에 고정 SHA-256으로 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.1/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="9041c73c99fdc5892cf128140beb354eca9231d502068cc36e6cb53a8e59ad14" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.1/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="eadd9d27c134ab5ac1ffd4473b97a741407f3ea238288f03121fc4deddf90220" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.13.1.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.13.1"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
