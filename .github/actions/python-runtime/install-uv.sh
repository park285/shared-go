#!/usr/bin/env bash
set -euo pipefail
# 내려받은 byte를 실행 전에 고정 SHA-256으로 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.0/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="3ccfb6af6e242433eb552f7d9676abd5412c6595c497e990d9c8cb7b5bd4d2c3" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.0/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="1468ebd5a5541121837c5a2817b9972ba6090fa6caa3d142620850a47fb75154" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.13.0.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.13.0"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
