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
repo_name_for_root() {
  local root="$1" common_dir
  common_dir="$(git -C "${root}" rev-parse --git-common-dir 2>/dev/null || true)"
  if [[ -z "${common_dir}" ]]; then
    basename "${root}"
    return
  fi
  if [[ "${common_dir}" != /* ]]; then
    common_dir="${root}/${common_dir}"
  fi
  if [[ "$(basename "${common_dir}")" == ".git" ]]; then
    basename "$(dirname "${common_dir}")"
  else
    basename "${root}"
  fi
}
REPO_NAME="$(repo_name_for_root "${REPO_ROOT}")"
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
    harness_files:
      - fixture/bench_test.go
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
  git -C "${dir}" init -q
  git -C "${dir}" config user.email benchgate@example.invalid
  git -C "${dir}" config user.name benchgate
  git -C "${dir}" add go.mod fixture/bench_test.go policy.yaml
  git -C "${dir}" commit -qm fixture
}

source "${SCRIPT_DIR}/check-bench-regression-fixture-support.sh"

run_collect_checker() {
  local repo_dir="$1"
  shift
  set +e
  LAST_OUTPUT="$(cd "${repo_dir}" && "${CHECKER}" collect "$@" --gate-id fixture-gate 2>&1)"
  LAST_STATUS=$?
  set -e
}

run_collect_checker_bounded() {
  local repo_dir="$1"
  shift
  set +e
  LAST_OUTPUT="$(cd "${repo_dir}" && timeout 5s "${CHECKER}" collect "$@" --gate-id fixture-gate 2>&1)"
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

assert_exit_code() {
  local name="$1"
  local expected="$2"
  if [[ "${LAST_STATUS}" -ne "${expected}" ]]; then
    printf 'not ok - %s expected exit %s got %s\n%s\n' "${name}" "${expected}" "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s exit=%s\n' "${name}" "${expected}"
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
  assert_exit_code "empty-match exit code" 2
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
  assert_exit_code "unknown-field exit code" 2
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
    harness_files:
      - fixture/bench_test.go
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
  assert_exit_code "unknown defaults field exit code" 2
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
    harness_files:
      - fixture/bench_test.go
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
  assert_exit_code "unknown settings field exit code" 2
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
  assert_contains "warn mode ns precision format" "baseline=100.000 candidate=140.000"
  assert_contains "warn mode percent format" "+40.00% exceeds budget 10.00%"
}

case_fail_mode_critical_exits_one() {
  local dir="${TMP_ROOT}/fail-critical"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 140 8 1
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "fail mode critical exits nonzero"
  assert_exit_code "fail mode critical exit code" 1
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
  assert_exit_code "fail mode hotpath exit code" 1
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

case_stdout_race_marker_does_not_skip_comparison() {
  local dir="${TMP_ROOT}/race-skip"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1 "# command: go test -race"
  write_bench_file "${dir}/candidate" "BenchmarkTarget" 200 32 4 "# command: go test -race"
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "stdout race marker does not skip comparison"
  assert_exit_code "stdout race marker comparison exit code" 1
  assert_not_contains "stdout race marker does not set provenance" "skip"
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
  assert_exit_code "absolute candidate exit code" 2
  assert_contains "absolute candidate refusal" "candidate path must be a relative path under artifacts/perf"

  run_collect_checker "${repo_dir}" --policy policy.yaml --candidate ../outside --gate pr
  assert_failure "collect rejects escaping candidate"
  assert_exit_code "escaping candidate exit code" 2
  assert_contains "escaping candidate refusal" "candidate path must be a relative path under artifacts/perf"
  if [[ -e "${dir}/outside" ]]; then
    printf 'not ok - escaping candidate created outside path\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_collect_rejects_escaping_package_paths() {
  local repo_dir="${TMP_ROOT}/escaping-package-repo"
  write_collect_fixture_repo "${repo_dir}"
  sed -i 's|package: ./fixture|package: ./../../|' "${repo_dir}/policy.yaml"

  run_collect_checker_bounded "${repo_dir}" --policy policy.yaml --candidate artifacts/perf/pr --gate pr
  assert_failure "collect rejects escaping package"
  assert_exit_code "escaping package exit code" 2
  assert_contains "escaping package refusal" "benchmark package path must stay under repo root"
}

case_collect_rejects_symlinked_perf_root() {
  local repo_dir="${TMP_ROOT}/symlinked-perf-root/repo"
  local outside="${TMP_ROOT}/symlinked-perf-root/outside"
  write_collect_fixture_repo "${repo_dir}"
  mkdir -p "${repo_dir}/artifacts" "${outside}/pr"
  touch "${outside}/pr/keep"
  ln -s "${outside}" "${repo_dir}/artifacts/perf"

  run_collect_checker "${repo_dir}" --policy policy.yaml --candidate artifacts/perf/pr --gate pr
  assert_failure "collect rejects symlinked perf root"
  assert_exit_code "symlinked perf root exit code" 2
  assert_contains "symlinked perf root refusal" "candidate path"
  if [[ ! -e "${outside}/pr/keep" ]]; then
    printf 'not ok - symlinked perf root collect deleted outside candidate\n%s\n' "${LAST_OUTPUT}" >&2
    exit 1
  fi
}

case_missing_benchmem_columns_error_for_critical_bench() {
  local dir="${TMP_ROOT}/missing-benchmem"
  mkdir -p "${dir}"
  write_policy "${dir}/policy.yaml" "warn" "critical" "BenchmarkTarget" 50
  write_bench_file "${dir}/baseline" "BenchmarkTarget" 100 8 1
  write_bench_file_no_benchmem "${dir}/candidate" "BenchmarkTarget" 100
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "missing benchmem columns error"
  assert_exit_code "missing benchmem exit code" 2
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
  assert_exit_code "same benchmark name exit code" 1
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

case_multifile_baseline_orders_like_pathlib() {
  local dir="${TMP_ROOT}/multifile-baseline-order"
  mkdir -p "${dir}/baseline/sub" "${dir}/candidate/go-bench"
  write_policy "${dir}/policy.yaml" "fail" "critical" "BenchmarkTarget" 50
  printf '# package: ./fixture\n# count: 3\n# benchtime: 100ms\npkg: fixture\nBenchmarkTarget-16 100 100 ns/op 8 B/op 1 allocs/op\n' >"${dir}/baseline/sub/deep.txt"
  printf '# package: ./fixture\n# count: 1\n# benchtime: 100ms\npkg: fixture\nBenchmarkTarget-16 100 100 ns/op 8 B/op 1 allocs/op\n' >"${dir}/baseline/sub-extra.txt"
  printf '# package: ./fixture\npkg: fixture\nBenchmarkTarget-16 100 100 ns/op 8 B/op 1 allocs/op\nBenchmarkTarget-16 100 100 ns/op 8 B/op 1 allocs/op\nPASS\n' >"${dir}/candidate/go-bench/result.txt"
  run_checker "${dir}/baseline" "${dir}/candidate" "${dir}/policy.yaml"
  assert_failure "multifile baseline pathlib order"
  assert_exit_code "multifile baseline pathlib order exit code" 2
  assert_contains "multifile baseline last-file count wins" "count=1 < min_count=2"
  assert_contains "multifile baseline names pathlib-last file" "sub-extra.txt"
}

source "${SCRIPT_DIR}/check-bench-regression-baseline_test.sh"

CASES=(
  case_empty_match_errors
  case_unknown_field_errors
  case_unknown_defaults_field_errors
  case_unknown_settings_field_errors
  case_warn_mode_prints_all_and_exits_zero
  case_fail_mode_critical_exits_one
  case_fail_mode_hotpath_exits_one
  case_fail_mode_non_critical_exits_zero
  case_missing_baseline_fails_without_copy
  case_stdout_race_marker_does_not_skip_comparison
  case_collect_rejects_unsafe_candidate_paths
  case_collect_rejects_escaping_package_paths
  case_collect_rejects_symlinked_perf_root
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
