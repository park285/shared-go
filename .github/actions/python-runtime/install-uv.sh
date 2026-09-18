#!/usr/bin/env bash
set -euo pipefail
# tools/checks/toolchain-pins.json에서 생성. 내려받은 byte를 실행 전에 검증한다.
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.13/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="2eaa5d94f5db7b3a1a092156b9420459e42ab0217d917fe74a876309cef9b5e9" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.12.13/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="745765a3b6e360ad76743599ae5c42e9278c7edf8bbff9fc76d05bf2623a04dd" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.12.13.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.12.13"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
