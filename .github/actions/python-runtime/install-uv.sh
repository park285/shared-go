#!/usr/bin/env bash
set -euo pipefail
# 내려받은 byte를 실행 전에 고정 SHA-256으로 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.7/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="71279f8d3c67241e807b9d8db872d1bf541579fb9f08b7d1eb1682196d1e57dc" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.7/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="c5221ff41b7892bfc223543883600125f6a47fe845093e0f5bdff4f359196d83" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.13.7.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.13.7"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
