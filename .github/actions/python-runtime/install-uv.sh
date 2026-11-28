#!/usr/bin/env bash
set -euo pipefail
case "${RUNNER_OS:?}/${RUNNER_ARCH:?}" in
  Linux/ARM64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.10/uv-aarch64-unknown-linux-gnu.tar.gz"; expected_sha="14590f644805a6234ffe071154340669e38137f38e60aa9629690119704107d2" ;;
  Linux/X64) archive_url="https://releases.astral.sh/github/uv/releases/download/0.13.10/uv-x86_64-unknown-linux-gnu.tar.gz"; expected_sha="bf65ac1a0af10f92c522d91ac096c7c0a39657647b155577e7836c2cbbb4526c" ;;
  *) echo 'unsupported uv bootstrap platform' >&2; exit 1 ;;
esac
work_dir="$(mktemp -d "${RUNNER_TEMP:?}/uv-bootstrap.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
curl --fail --silent --show-error --location --proto "=https" --tlsv1.2 --max-time 120 "$archive_url" -o "$work_dir/uv.tar.gz"
printf '%s  %s\n' "$expected_sha" "$work_dir/uv.tar.gz" | sha256sum --check --status
tar -xzf "$work_dir/uv.tar.gz" -C "$work_dir" --strip-components=1
install_dir="$(mktemp -d "${RUNNER_TEMP}/iris-uv-0.13.10.XXXXXX")"
install -m 0755 "$work_dir/uv" "$install_dir/uv"
[[ "$("$install_dir/uv" --version)" == "uv 0.13.10"* ]]
printf '%s\n' "$install_dir" >>"${GITHUB_PATH:?}"
