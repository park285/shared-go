#!/usr/bin/env bash
set -euo pipefail
# 내려받은 byte를 실행 전에 고정 SHA-256으로 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.23/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="6524bd338177ed50d035d39354e12545e993bbeba2ecbddf0480c5b3a81d313f" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.23/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="9167d72b3319674b6303c4cbe071854bba13ebdf3d76b1a7cbdc175471fb66d6" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.12.23.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.12.23"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
