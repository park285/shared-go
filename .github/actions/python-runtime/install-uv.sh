#!/usr/bin/env bash
set -euo pipefail
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.9/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="0797542018e541a9288855df555c8077eed06f7a47d206362030480e8ecc9f3d" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.9/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="a7fbaff2b9a271d85c1bdb540941090fd1ce011797b4abeb48e4dcd39c2471d5" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.13.9.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.13.9"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
