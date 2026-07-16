#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECKER="${ROOT_DIR}/scripts/ci/check-workflow-secrets.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

workflow="${TMP_DIR}/quoted-pr-go-test.yml"
cat >"${workflow}" <<'YAML'
name: quoted-pr-go-test
on:
  pull_request:
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: "go test ./..."
YAML

if ! WORKFLOW_GATE_PROFILE=app "${CHECKER}" "${workflow}" >"${TMP_DIR}/out" 2>"${TMP_DIR}/err"; then
  cat "${TMP_DIR}/out" >&2
  cat "${TMP_DIR}/err" >&2
  echo "[FAIL] library profile must permit quoted pull_request full go test" >&2
  exit 1
fi

if ! grep -Fq "profile=lib" "${TMP_DIR}/out"; then
  cat "${TMP_DIR}/out" >&2
  echo "[FAIL] environment profile override must preserve profile=lib" >&2
  exit 1
fi

echo "[PASS] quoted pull_request full go test passes"
echo "[PASS] environment profile override is ignored"
