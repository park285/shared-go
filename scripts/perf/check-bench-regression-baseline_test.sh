#!/usr/bin/env bash

run_checker_require_baseline() {
  set +e
  LAST_OUTPUT="$("${CHECKER}" --baseline "$1" --candidate "$2" --policy "$3" --require-baseline 2>&1)"
  # shellcheck disable=SC2034 # source하는 상위 fixture가 상태를 검사한다.
  LAST_STATUS=$?
  set -e
}

case_missing_baseline_creates_copy() {
  local dir="${TMP_ROOT}/baseline-created"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 2\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "missing baseline creates copy"
  assert_contains "missing baseline message" "baseline created"
  [[ -f "${dir}/baseline/go-bench/result.txt" ]]
  cmp -s "${dir}/candidate/go-bench/result.txt" "${dir}/baseline/go-bench/result.txt"
}

case_required_missing_baseline_fails_without_copy() {
  local dir="${TMP_ROOT}/required-baseline-missing"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 6\n# benchtime: 100ms'
  run_checker_require_baseline "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "required missing baseline fails"
  assert_exit_code "required missing baseline exit code" 2
  assert_contains "required missing baseline message" "required baseline has no result files"
  if [[ -e "${dir}/baseline" ]]; then
    printf 'not ok - required missing baseline was created\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_missing_baseline_race_candidate_does_not_create_baseline() {
  local dir="${TMP_ROOT}/race-baseline-create"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# command: go test -race\n# count: 6\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "race candidate baseline create refused"
  assert_exit_code "race candidate refusal exit code" 2
  assert_contains "race candidate refusal message" "refusing to create baseline from race benchmark results"
  if [[ -e "${dir}/baseline" ]]; then
    printf 'not ok - race candidate created baseline\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}
