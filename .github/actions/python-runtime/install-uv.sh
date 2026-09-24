#!/usr/bin/env bash
set -euo pipefail
# tools/checks/toolchain-pins.json에서 생성. 내려받은 byte를 실행 전에 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.18/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="afb6291f3f0a6b4521fc67b947822506c41dde5b60d2189dd8f3695b2ac8c9e7" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.18/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="89eadd7c76fc063887959510d5ba0ab1264dfd5f1143b925ddb73021a40acf16" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.12.18.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.12.18"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
