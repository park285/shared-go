#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHECKER="${SCRIPT_DIR}/check-bench-regression.sh"

cd "${REPO_ROOT}"

if [[ ! -x "${CHECKER}" ]]; then
  echo "not ok - checker missing or not executable: ${CHECKER}" >&2
  exit 1
fi

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT
REPO_NAME="$(basename "${REPO_ROOT}")"
LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

write_policy() {
  local path="$1"
  local mode="$2"
  local class="$3"
  local bench="$4"
  local noise_floor="$5"
  local extra="${6:-}"

  write_policy_for_repo "${path}" "${REPO_NAME}" "${mode}" "${class}" "${bench}" "${noise_floor}" "${extra}"
}

write_policy_for_repo() {
  local path="$1"
  local repo_name="$2"
  local mode="$3"
  local class="$4"
  local bench="$5"
  local noise_floor="$6"
  local extra="${7:-}"

  cat >"${path}" <<EOF
schemaVersion: 1
repo: ${repo_name}
defaults:
  critical:
    max_ns_regression_percent: 10
    max_bytes_regression_percent: 10
    allow_alloc_increase: false
  hotpath:
    max_ns_regression_percent: 15
    max_bytes_regression_percent: 15
    allow_alloc_increase: false
  build_path:
    max_ns_regression_percent: 20
    max_bytes_regression_percent: 20
    allow_alloc_increase: warn
  non_critical:
    max_ns_regression_percent: 25
    max_bytes_regression_percent: 25
    allow_alloc_increase: warn
benchmarks:
  ${bench}:
    package: ./fixture
    class: ${class}
    gate: pr
${extra}
settings:
  mode: ${mode}
  benchstat_alpha: 0.05
  min_count: 2
  noise_floor_ns: ${noise_floor}
EOF
}

write_bench_file() {
  local dir="$1"
  local bench="$2"
  local ns="$3"
  local bytes="$4"
  local allocs="$5"
  local header="${6:-}"
  local package="${7:-./fixture}"
  mkdir -p "${dir}/go-bench"
  {
    if [[ -n "${header}" ]]; then
      printf '%s\n' "${header}"
    fi
    printf '# package: %s\n' "${package}"
    printf 'goos: linux\n'
    printf 'goarch: amd64\n'
    printf 'pkg: fixture\n'
    printf 'Benchmark%s-16 100 %s ns/op %s B/op %s allocs/op\n' "${bench#Benchmark}" "${ns}" "${bytes}" "${allocs}"
    printf 'Benchmark%s-16 100 %s ns/op %s B/op %s allocs/op\n' "${bench#Benchmark}" "${ns}" "${bytes}" "${allocs}"
    printf 'PASS\n'
  } >"${dir}/go-bench/result.txt"
}

write_bench_file_no_benchmem() {
  local dir="$1"
  local bench="$2"
  local ns="$3"
  local header="${4:-}"
  local package="${5:-./fixture}"
  mkdir -p "${dir}/go-bench"
  {
    if [[ -n "${header}" ]]; then
      printf '%s\n' "${header}"
    fi
    printf '# package: %s\n' "${package}"
    printf 'goos: linux\n'
    printf 'goarch: amd64\n'
    printf 'pkg: fixture\n'
    printf 'Benchmark%s-16 100 %s ns/op\n' "${bench#Benchmark}" "${ns}"
    printf 'Benchmark%s-16 100 %s ns/op\n' "${bench#Benchmark}" "${ns}"
    printf 'PASS\n'
  } >"${dir}/go-bench/result.txt"
}

write_two_package_bench_file() {
  local dir="$1"
  local bench="$2"
  local package_a_ns="$3"
  local package_b_ns="$4"
  mkdir -p "${dir}/go-bench"
  {
    printf '# package: ./fixture/a\n'
    printf 'goos: linux\n'
    printf 'goarch: amd64\n'
    printf 'pkg: fixture/a\n'
    printf 'Benchmark%s-16 100 %s ns/op 8 B/op 1 allocs/op\n' "${bench#Benchmark}" "${package_a_ns}"
    printf 'Benchmark%s-16 100 %s ns/op 8 B/op 1 allocs/op\n' "${bench#Benchmark}" "${package_a_ns}"
    printf '# package: ./fixture/b\n'
    printf 'goos: linux\n'
    printf 'goarch: amd64\n'
    printf 'pkg: fixture/b\n'
    printf 'Benchmark%s-16 100 %s ns/op 8 B/op 1 allocs/op\n' "${bench#Benchmark}" "${package_b_ns}"
    printf 'Benchmark%s-16 100 %s ns/op 8 B/op 1 allocs/op\n' "${bench#Benchmark}" "${package_b_ns}"
    printf 'PASS\n'
  } >"${dir}/go-bench/result.txt"
}

write_collect_fixture_repo() {
  local dir="$1"
  local repo_name
  repo_name="$(basename "${dir}")"
  mkdir -p "${dir}/fixture"
  cat >"${dir}/go.mod" <<EOF
module ${repo_name}

go 1.22
EOF
  cat >"${dir}/fixture/bench_test.go" <<'EOF'
package fixture

import "testing"

func BenchmarkTarget(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = i + 1
	}
}
EOF
  write_policy_for_repo "${dir}/policy.yaml" "${repo_name}" "warn" "critical" "BenchmarkTarget" 50
}

run_checker() {
  set +e
  LAST_OUTPUT="$("${CHECKER}" --baseline "$1" --candidate "$2" --policy "$3" 2>&1)"
  LAST_STATUS=$?
  set -e
}

run_checker_allow_smoke_baseline() {
  set +e
  LAST_OUTPUT="$("${CHECKER}" --baseline "$1" --candidate "$2" --policy "$3" --allow-smoke-baseline 2>&1)"
  LAST_STATUS=$?
  set -e
}

run_collect_checker() {
  local repo_dir="$1"
  shift
  set +e
  LAST_OUTPUT="$(cd "${repo_dir}" && "${CHECKER}" collect "$@" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_success() {
  local name="$1"
  if [[ "${LAST_STATUS}" -ne 0 ]]; then
    printf 'not ok - %s\nstatus=%s\n%s\n' "${name}" "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_failure() {
  local name="$1"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - %s unexpectedly succeeded\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_contains() {
  local name="$1"
  local needle="$2"
  if [[ "${LAST_OUTPUT}" != *"${needle}"* ]]; then
    printf 'not ok - %s missing %q\n%s\n' "${name}" "${needle}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

assert_not_contains() {
  local name="$1"
  local needle="$2"
  if [[ "${LAST_OUTPUT}" == *"${needle}"* ]]; then
    printf 'not ok - %s unexpectedly contained %q\n%s\n' "${name}" "${needle}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_empty_match_errors() {
  local dir="${TMP_ROOT}/empty-match"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkMissing" 50
  write_bench_file "${dir}/baseline" "BenchmarkPresent" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkPresent" 100 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "empty-match error"
  assert_contains "empty-match error" "missing candidate benchmark: BenchmarkMissing"
}

case_unknown_field_errors() {
  local dir="${TMP_ROOT}/unknown-field"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkTarget" 50 "    typo_field: true"
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "unknown-field error"
  assert_contains "unknown-field error" "unknown field"
}

case_unknown_defaults_field_errors() {
  local dir="${TMP_ROOT}/unknown-defaults-field"
  mkdir -p "${dir}"
  cat >"${dir}/policy.yaml" <<EOF
schemaVersion: 1
repo: ${REPO_NAME}
defaults:
  typo_field: true
  critical:
    max_ns_regression_percent: 10
    max_bytes_regression_percent: 10
    allow_alloc_increase: false
  hotpath:
    max_ns_regression_percent: 15
    max_bytes_regression_percent: 15
    allow_alloc_increase: false
  build_path:
    max_ns_regression_percent: 20
    max_bytes_regression_percent: 20
    allow_alloc_increase: warn
  non_critical:
    max_ns_regression_percent: 25
    max_bytes_regression_percent: 25
    allow_alloc_increase: warn
benchmarks:
  BenchmarkTarget:
    package: ./fixture
    class: critical
    gate: pr
settings:
  mode: warn
  benchstat_alpha: 0.05
  min_count: 2
  noise_floor_ns: 50
EOF
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "unknown defaults field error"
  assert_contains "unknown defaults field error" "unknown field: defaults.typo_field"
}

case_unknown_settings_field_errors() {
  local dir="${TMP_ROOT}/unknown-settings-field"
  mkdir -p "${dir}"
  cat >"${dir}/policy.yaml" <<EOF
schemaVersion: 1
repo: ${REPO_NAME}
defaults:
  critical:
    max_ns_regression_percent: 10
    max_bytes_regression_percent: 10
    allow_alloc_increase: false
  hotpath:
    max_ns_regression_percent: 15
    max_bytes_regression_percent: 15
    allow_alloc_increase: false
  build_path:
    max_ns_regression_percent: 20
    max_bytes_regression_percent: 20
    allow_alloc_increase: warn
  non_critical:
    max_ns_regression_percent: 25
    max_bytes_regression_percent: 25
    allow_alloc_increase: warn
benchmarks:
  BenchmarkTarget:
    package: ./fixture
    class: critical
    gate: pr
settings:
  mode: warn
  benchstat_alpha: 0.05
  min_count: 2
  noise_floor_ns: 50
  typo_field: true
EOF
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "unknown settings field error"
  assert_contains "unknown settings field error" "unknown field: settings.typo_field"
}

case_warn_mode_prints_all_and_exits_zero() {
  local dir="${TMP_ROOT}/warn-mode"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 140 16 2
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "warn mode exits zero"
  assert_contains "warn mode prints violation" "violation"
  assert_contains "warn mode names benchmark" "BenchmarkTarget"
  assert_contains "warn mode prints ns/op violation" "metric=ns/op"
  assert_contains "warn mode prints B/op violation" "metric=B/op"
  assert_contains "warn mode prints allocs/op violation" "metric=allocs/op"
}

case_fail_mode_critical_exits_one() {
  local dir="${TMP_ROOT}/fail-critical"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 140 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "fail mode critical exits nonzero"
  assert_contains "fail mode critical violation" "critical"
}

case_fail_mode_hotpath_exits_one() {
  local dir="${TMP_ROOT}/fail-hotpath"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "hotpath" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 140 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "fail mode hotpath exits nonzero"
  assert_contains "fail mode hotpath violation" "hotpath"
}

case_fail_mode_non_critical_exits_zero() {
  local dir="${TMP_ROOT}/fail-non-critical"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "non_critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 140 16 2
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "fail mode non_critical exits zero"
  assert_contains "fail mode non_critical prints violation" "violation"
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

case_missing_baseline_race_candidate_does_not_create_baseline() {
  local dir="${TMP_ROOT}/race-baseline-create"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# command: go test -race\n# count: 6\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "race candidate baseline create refused"
  assert_contains "race candidate refusal message" "refusing to create baseline from race benchmark results"
  if [[ -e "${dir}/baseline" ]]; then
    printf 'not ok - race candidate created baseline\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_race_results_skip() {
  local dir="${TMP_ROOT}/race-skip"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1 "# command: go test -race"
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 200 32 4 "# command: go test -race"
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "race result skips comparison"
  assert_contains "race skip message" "skip"
  assert_contains "race skip mentions race" "race"
}

case_collect_rejects_unsafe_candidate_paths() {
  local dir="${TMP_ROOT}/unsafe-candidate"
  local repo_dir="${dir}/repo"
  mkdir -p "${repo_dir}"
  write_policy_for_repo "${repo_dir}/policy.yaml" "repo" "warn" "critical" "BenchmarkTarget" 50

  if [[ -e /tmp/x ]]; then
    printf 'not ok - unsafe candidate fixture requires /tmp/x to be absent\n' >&2
    exit 1
  fi
  run_collect_checker "${repo_dir}" --policy policy.yaml --candidate /tmp/x --gate pr
  if [[ -e /tmp/x ]]; then
    rm -rf /tmp/x
  fi
  assert_failure "collect rejects absolute candidate"
  assert_contains "absolute candidate refusal" "candidate path must be a relative path under artifacts/perf"

  run_collect_checker "${repo_dir}" --policy policy.yaml --candidate ../outside --gate pr
  assert_failure "collect rejects escaping candidate"
  assert_contains "escaping candidate refusal" "candidate path must be a relative path under artifacts/perf"
  if [[ -e "${dir}/outside" ]]; then
    printf 'not ok - escaping candidate created outside path\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_smoke_candidate_refused_for_baseline_without_allow_flag() {
  local dir="${TMP_ROOT}/smoke-baseline-refused"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 1\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "smoke candidate baseline refused"
  assert_contains "smoke candidate refusal" "refusing to create baseline from smoke benchmark results"
  if [[ -e "${dir}/baseline" ]]; then
    printf 'not ok - smoke candidate created baseline\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_smoke_candidate_allow_flag_creates_baseline() {
  local dir="${TMP_ROOT}/smoke-baseline-allowed"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 1\n# benchtime: 100ms'
  run_checker_allow_smoke_baseline "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "smoke candidate allow flag creates baseline"
  assert_contains "smoke candidate allow flag message" "baseline created"
  cmp -s "${dir}/candidate/go-bench/result.txt" "${dir}/baseline/go-bench/result.txt"
}

case_non_default_benchtime_candidate_refused_for_baseline() {
  local dir="${TMP_ROOT}/benchtime-baseline-refused"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 2\n# benchtime: 10x'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "non-default benchtime candidate baseline refused"
  assert_contains "non-default benchtime refusal" "benchtime=10x differs from required 100ms"
}

case_existing_smoke_baseline_warns_in_warn_mode() {
  local dir="${TMP_ROOT}/existing-smoke-baseline-warn"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1 $'# count: 1\n# benchtime: 100ms'
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 2\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "existing smoke baseline warns in warn mode"
  assert_contains "existing smoke baseline warning" "warning: existing baseline is smoke/junk baseline"
}

case_existing_smoke_baseline_errors_in_fail_mode() {
  local dir="${TMP_ROOT}/existing-smoke-baseline-fail"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1 $'# count: 1\n# benchtime: 100ms'
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 100 8 1 $'# count: 2\n# benchtime: 100ms'
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "existing smoke baseline errors in fail mode"
  assert_contains "existing smoke baseline error" "error: existing baseline is smoke/junk baseline"
}

case_missing_benchmem_columns_error_for_critical_bench() {
  local dir="${TMP_ROOT}/missing-benchmem"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file_no_benchmem "${dir}/candidate" "BenchmarkTarget" 100
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "missing benchmem columns error"
  assert_contains "missing benchmem names benchmark" "BenchmarkTarget"
  assert_contains "missing benchmem B/op" "missing B/op"
  assert_contains "missing benchmem file" "candidate file"
}

case_collect_smoke_count_prints_marker() {
  local repo_dir="${TMP_ROOT}/collect-smoke-repo"
  write_collect_fixture_repo "${repo_dir}"
  run_collect_checker "${repo_dir}" --policy policy.yaml --candidate artifacts/perf/pr --gate pr --count 1 --benchtime 100ms
  assert_success "collect smoke count succeeds"
  assert_contains "collect smoke count marker" "smoke run: count<min_count"
}

case_same_benchmark_name_in_two_packages_compares_selected_package() {
  local dir="${TMP_ROOT}/same-name-two-packages"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  sed -i 's|package: ./fixture|package: ./fixture/a|' "${dir}/policy.yaml"
  write_two_package_bench_file "${dir}/baseline" "BenchmarkTarget" 100 10000
  write_two_package_bench_file "${dir}/candidate" "BenchmarkTarget" 200 10000
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "same benchmark name compares selected package"
  assert_contains "same benchmark package violation" "metric=ns/op"
}

case_noise_floor_uses_absolute_ns() {
  local dir="${TMP_ROOT}/noise-floor"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 10 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 40 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_success "noise floor uses absolute ns"
  assert_not_contains "noise floor no violation" "violation"
}

CASES=(
  case_empty_match_errors
  case_unknown_field_errors
  case_unknown_defaults_field_errors
  case_unknown_settings_field_errors
  case_warn_mode_prints_all_and_exits_zero
  case_fail_mode_critical_exits_one
  case_fail_mode_hotpath_exits_one
  case_fail_mode_non_critical_exits_zero
  case_missing_baseline_creates_copy
  case_missing_baseline_race_candidate_does_not_create_baseline
  case_race_results_skip
  case_collect_rejects_unsafe_candidate_paths
  case_smoke_candidate_refused_for_baseline_without_allow_flag
  case_smoke_candidate_allow_flag_creates_baseline
  case_non_default_benchtime_candidate_refused_for_baseline
  case_existing_smoke_baseline_warns_in_warn_mode
  case_existing_smoke_baseline_errors_in_fail_mode
  case_missing_benchmem_columns_error_for_critical_bench
  case_collect_smoke_count_prints_marker
  case_same_benchmark_name_in_two_packages_compares_selected_package
  case_noise_floor_uses_absolute_ns
)

if [[ -n "${TEST_CASE:-}" ]]; then
  "${TEST_CASE}"
else
  for test_case in "${CASES[@]}"; do
    "${test_case}"
  done
fi

printf 'passed %d fixture cases\n' "${PASSED}"
