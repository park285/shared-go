#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

CHECKER="${ROOT_DIR}/scripts/ci/check-workflow-secrets.sh"
failures=0

record_fail() {
  echo "[FAIL] $*" >&2
  failures=$((failures + 1))
}

pass() {
  echo "[PASS] $*"
}

write_workflow() {
  local file="$1"
  shift

  mkdir -p "$(dirname "${file}")"
  printf '%s\n' "$@" >"${file}"
}

expect_success() {
  local label="$1"
  shift
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"

  if ! "${CHECKER}" "$@" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected workflow secret check success: ${label}"
    return
  fi

  pass "${label}"
}

expect_failure() {
  local label="$1"
  local expected="$2"
  shift 2
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"

  if "${CHECKER}" "$@" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected workflow secret check failure: ${label}"
    return
  fi

  if ! grep -Fq "${expected}" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected ${expected} in failure output: ${label}"
    return
  fi

  pass "${label}"
}

repo_fixture="${TMP_DIR}/repo-workflows"
mkdir -p "${repo_fixture}/.github"
cp -R "${ROOT_DIR}/.github/workflows" "${repo_fixture}/.github/workflows"
expect_success "repository workflows pass policy" "${repo_fixture}/.github/workflows"/*.yml

disallowed_secret="${TMP_DIR}/disallowed-secret.yml"
write_workflow "${disallowed_secret}" \
  "name: disallowed-secret" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "pull_request repository secret fails" "secrets.MODULES_TOKEN" "${disallowed_secret}"

bracket_secret="${TMP_DIR}/bracket-secret.yml"
write_workflow "${bracket_secret}" \
  "name: bracket-secret" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets['\''MODULES_TOKEN'\''] }}"'
expect_failure "pull_request bracket secret fails" "secrets.MODULES_TOKEN" "${bracket_secret}"

multiline_secret="${TMP_DIR}/multiline-secret.yml"
write_workflow "${multiline_secret}" \
  "name: multiline-secret" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - env:" \
  '          TOKEN: ${{' \
  "            secrets.MODULES_TOKEN" \
  "          }}" \
  "        run: echo masked"
expect_failure "pull_request multiline secret fails" "secrets.MODULES_TOKEN" "${multiline_secret}"

reusable_inherit="${TMP_DIR}/reusable-inherit.yml"
write_workflow "${reusable_inherit}" \
  "name: reusable-inherit" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  call:" \
  "    uses: owner/repo/.github/workflows/reusable.yml@main" \
  "    secrets: inherit"
expect_failure "pull_request reusable secrets inherit fails" "secrets: inherit" "${reusable_inherit}"

reusable_secret_map="${TMP_DIR}/reusable-secret-map.yml"
write_workflow "${reusable_secret_map}" \
  "name: reusable-secret-map" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  call:" \
  "    uses: owner/repo/.github/workflows/reusable.yml@main" \
  "    secrets:" \
  '      token: ${{ secrets['\''MODULES_TOKEN'\''] }}'
expect_failure "pull_request reusable secrets map fails" "reusable workflow secrets" "${reusable_secret_map}"

missing_permissions="${TMP_DIR}/missing-permissions.yml"
write_workflow "${missing_permissions}" \
  "name: missing-permissions" \
  "on:" \
  "  pull_request:" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "pull_request missing permissions fails" "top-level read-only permissions" "${missing_permissions}"

write_permissions_without_token="${TMP_DIR}/write-permissions-without-token.yml"
write_workflow "${write_permissions_without_token}" \
  "name: write-permissions-without-token" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: write" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "pull_request write permissions without token fails" "read-only permissions" "${write_permissions_without_token}"

allowed_token="${TMP_DIR}/allowed-github-token.yml"
write_workflow "${allowed_token}" \
  "name: allowed-github-token" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.GITHUB_TOKEN }}"'
expect_success "pull_request read-only GITHUB_TOKEN passes" "${allowed_token}"

write_token="${TMP_DIR}/write-github-token.yml"
write_workflow "${write_token}" \
  "name: write-github-token" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: write" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.GITHUB_TOKEN }}"'
expect_failure "pull_request write GITHUB_TOKEN fails" "read-only permissions" "${write_token}"

trusted_push="${TMP_DIR}/trusted-push.yml"
write_workflow "${trusted_push}" \
  "name: trusted-push" \
  "on:" \
  "  push:" \
  "    branches:" \
  "      - main" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_success "trusted push repository secret passes" "${trusted_push}"

if (( failures > 0 )); then
  echo "[FAIL] workflow secret checker tests failed: ${failures}" >&2
  exit 1
fi

echo "ok: workflow secret checker tests passed"
