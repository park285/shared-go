#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$(mktemp "${TMPDIR:-/tmp}/benchgate.XXXXXX")"
trap 'rm -f "${BIN}"' EXIT

( cd "${SCRIPT_DIR}/benchgate" && GOWORK=off go build -o "${BIN}" . )
"${BIN}" "$@"
