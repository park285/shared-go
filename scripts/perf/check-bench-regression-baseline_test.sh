#!/usr/bin/env bash

run_checker_missing_baseline() {
  set +e
  LAST_OUTPUT="$("${CHECKER}" --baseline "$1" --candidate "$2" --policy "$3" --gate pr --gate-id fixture-gate 2>&1)"
  # shellcheck disable=SC2034 # source하는 상위 fixture가 상태를 검사한다.
  LAST_STATUS=$?
  set -e
}

case_missing_baseline_fails_without_copy() {
  local dir="${TMP_ROOT}/baseline-missing"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 6\n# benchtime: 100ms'
  run_checker_missing_baseline "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "missing baseline fails"
  assert_exit_code "missing baseline exit code" 2
  assert_contains "missing baseline message" "required baseline has no result files"
  if [[ -e "${dir}/baseline" ]]; then
    printf 'not ok - missing baseline was created\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}
