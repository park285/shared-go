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

expect_failure_app_profile() {
  local label="$1"
  local expected="$2"
  shift 2
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"

  if WORKFLOW_GATE_PROFILE=app "${CHECKER}" "$@" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected app-profile workflow secret check failure: ${label}"
    return
  fi

  if ! grep -Fq "${expected}" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected ${expected} in app-profile failure output: ${label}"
    return
  fi

  pass "${label}"
}

expect_profile_failure() {
  local label="$1"
  local expected="$2"
  local workdir="$3"
  shift 3
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"

  if (cd "${workdir}" && "${CHECKER}" "$@") >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected profile resolution failure: ${label}"
    return
  fi

  if ! grep -Fq "${expected}" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected ${expected} in profile failure output: ${label}"
    return
  fi

  pass "${label}"
}

repo_fixture="${TMP_DIR}/repo-workflows"
mkdir -p "${repo_fixture}/.github"
cp -R "${ROOT_DIR}/.github/workflows" "${repo_fixture}/.github/workflows"
expect_success "repository workflows pass policy" "${repo_fixture}/.github/workflows"/*.yml

missing_profile_root="${TMP_DIR}/missing-profile-root"
mkdir -p "${missing_profile_root}"
expect_profile_failure "missing explicit profile fails closed" \
  "explicit workflow gate profile is required" "${missing_profile_root}" \
  "${repo_fixture}/.github/workflows"/*.yml

invalid_profile_root="${TMP_DIR}/invalid-profile-root"
mkdir -p "${invalid_profile_root}/scripts/ci"
printf 'service\n' >"${invalid_profile_root}/scripts/ci/workflow-gate-profile"
expect_profile_failure "invalid explicit profile fails closed" \
  "expected exact app or lib declaration" "${invalid_profile_root}" \
  "${repo_fixture}/.github/workflows"/*.yml

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

dynamic_secret="${TMP_DIR}/dynamic-secret.yml"
write_workflow "${dynamic_secret}" \
  "name: dynamic-secret" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets[format('\''MODULES_{0}'\'', '\''TOKEN'\'')] }}"'
expect_failure "pull_request dynamic secret index fails" "secrets[dynamic]" "${dynamic_secret}"

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

quoted_on_block="${TMP_DIR}/quoted-on-block.yml"
write_workflow "${quoted_on_block}" \
  "name: quoted-on-block" \
  '"on":' \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "quoted on key still detects pull_request secret" "secrets.MODULES_TOKEN" "${quoted_on_block}"

quoted_on_inline="${TMP_DIR}/quoted-on-inline.yml"
write_workflow "${quoted_on_inline}" \
  "name: quoted-on-inline" \
  "'on': pull_request" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "single-quoted inline on detects pull_request secret" "secrets.MODULES_TOKEN" "${quoted_on_inline}"

quoted_event_key="${TMP_DIR}/quoted-event-key.yml"
write_workflow "${quoted_event_key}" \
  "name: quoted-event-key" \
  "on:" \
  '  "pull_request":' \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "quoted event key detects pull_request secret" "secrets.MODULES_TOKEN" "${quoted_event_key}"

list_item_event_comment="${TMP_DIR}/list-item-event-comment.yml"
write_workflow "${list_item_event_comment}" \
  "name: list-item-event-comment" \
  "on:" \
  "  - pull_request # trigger on PR" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "list-item on entry with trailing comment detects pull_request secret" "secrets.MODULES_TOKEN" "${list_item_event_comment}"

multiline_flow_on="${TMP_DIR}/multiline-flow-on.yml"
write_workflow "${multiline_flow_on}" \
  "name: multiline-flow-on" \
  "on:" \
  "  [" \
  "    push," \
  "    pull_request" \
  "  ]" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "multi-line flow on is rejected fail-closed" "multi-line YAML flow collections" "${multiline_flow_on}"

alias_on="${TMP_DIR}/alias-on.yml"
write_workflow "${alias_on}" \
  "name: alias-on" \
  "events: &pr_events" \
  "  pull_request:" \
  '"on": *pr_events' \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "yaml alias on is rejected fail-closed" "YAML anchors and aliases" "${alias_on}"

pr_target="${TMP_DIR}/pull-request-target.yml"
write_workflow "${pr_target}" \
  "name: pull-request-target" \
  "on:" \
  "  pull_request_target:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "pull_request_target is rejected" "pull_request_target workflow is not allowed" "${pr_target}"

quoted_pr_target="${TMP_DIR}/quoted-pull-request-target.yml"
write_workflow "${quoted_pr_target}" \
  "name: quoted-pull-request-target" \
  '"on": pull_request_target' \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "quoted on pull_request_target is rejected" "pull_request_target workflow is not allowed" "${quoted_pr_target}"

whole_object_secret="${TMP_DIR}/whole-object-secret.yml"
write_workflow "${whole_object_secret}" \
  "name: whole-object-secret" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ toJson(secrets) }}"'
expect_failure "pull_request whole secrets object fails" "whole secrets object" "${whole_object_secret}"

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

commented_write_permissions="${TMP_DIR}/commented-write-permissions.yml"
write_workflow "${commented_write_permissions}" \
  "name: commented-write-permissions" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "  pull-requests: write # should fail" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "pull_request write permissions with trailing comment fails" "read-only permissions" "${commented_write_permissions}"

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

quoted_pr_go_test="${TMP_DIR}/quoted-pr-go-test.yml"
write_workflow "${quoted_pr_go_test}" \
  "name: quoted-pr-go-test" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: "go test ./..."'
expect_failure_app_profile "quoted pull_request full go test fails" "full repository go test" "${quoted_pr_go_test}"

quoted_checkout="${TMP_DIR}/quoted-checkout.yml"
write_workflow "${quoted_checkout}" \
  "name: quoted-checkout" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - uses: "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd"'
expect_failure "quoted checkout without persist-credentials fails" "persist-credentials: false" "${quoted_checkout}"

checkout_env_persist="${TMP_DIR}/checkout-env-persist.yml"
write_workflow "${checkout_env_persist}" \
  "name: checkout-env-persist" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd" \
  "        env:" \
  "          persist-credentials: false"
expect_failure "checkout persist-credentials under env fails" "persist-credentials: false" "${checkout_env_persist}"

checkout_duplicate_persist="${TMP_DIR}/checkout-duplicate-persist.yml"
write_workflow "${checkout_duplicate_persist}" \
  "name: checkout-duplicate-persist" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd" \
  "        with:" \
  "          persist-credentials: false" \
  "          persist-credentials: true"
expect_failure "checkout duplicate persist-credentials fails" "persist-credentials: false" "${checkout_duplicate_persist}"

uppercase_on_secret="${TMP_DIR}/uppercase-on-secret.yml"
write_workflow "${uppercase_on_secret}" \
  "name: uppercase-on-secret" \
  "ON:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  '      - run: echo "${{ secrets.MODULES_TOKEN }}"'
expect_failure "uppercase on key detects pull_request secret" "secrets.MODULES_TOKEN" "${uppercase_on_secret}"

secrets_string_literal="${TMP_DIR}/secrets-string-literal.yml"
write_workflow "${secrets_string_literal}" \
  "name: secrets-string-literal" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    if: \${{ contains(github.event.pull_request.title, 'secrets') }}" \
  "    steps:" \
  "      - run: echo ok"
expect_success "benign secrets string literal passes" "${secrets_string_literal}"

if (( failures > 0 )); then
  echo "[FAIL] workflow secret checker tests failed: ${failures}" >&2
  exit 1
fi

echo "ok: workflow secret checker tests passed"
