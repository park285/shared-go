#!/usr/bin/env bash
set -euo pipefail
# 내려받은 byte를 실행 전에 고정 SHA-256으로 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.24/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="5231be65f496304623895dacdbf1de8504fec90303684bdf05805aa34414dd21" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.24/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="b4dfaef47d491a7296981f8374a4595f55dbf84e8937c8ecd2983574d8bb3da6" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.12.24.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.12.24"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
