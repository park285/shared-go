#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
RUNNER="${SCRIPT_DIR}/python-runner.sh"
EXPECTED_PYTHON="3.14.7"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fail() {
  echo "python-runner-test: $*" >&2
  exit 1
}

expect_failure() {
  local label="${1:?label is required}" needle="${2:?needle is required}"
  shift 2
  local output
  if output="$("$@" 2>&1)"; then
    fail "${label}: expected failure"
  fi
  if [[ "${output}" != *"${needle}"* ]]; then
    fail "${label}: expected ${needle@Q}, got ${output@Q}"
  fi
}

resolved="$("${RUNNER}" --print-interpreter)"
actual="$("${resolved}" -I -S -c 'import platform; print(platform.python_version())')"
[[ "${actual}" == "${EXPECTED_PYTHON}" ]] || fail "actual Python version is ${actual}"

stderr_file="${tmp_dir}/stderr"
stdout="$(
  printf 'stdin-value' |
    "${RUNNER}" -- -c       'import sys; print(sys.argv[1]); print(sys.stdin.read()); print("stderr-value", file=sys.stderr)'       'argv-value' 2>"${stderr_file}"
)"
[[ "${stdout}" == $'argv-value\nstdin-value' ]] || fail "argv/stdin propagation failed"
[[ "$(<"${stderr_file}")" == "stderr-value" ]] || fail "stderr propagation failed"

nonzero_status=0
"${RUNNER}" -- -c 'raise SystemExit(23)' >/dev/null 2>&1 || nonzero_status=$?
signal_status="$("${resolved}" -I -S -c 'import subprocess, sys; print(subprocess.run([sys.argv[1], "--", "-c", "import os, signal; os.kill(os.getpid(), signal.SIGTERM)"]).returncode)' "${RUNNER}")"
[[ "${nonzero_status}" == 23 ]] || fail "nonzero status propagation failed: ${nonzero_status}"
[[ "${signal_status}" == -15 ]] || fail "signal propagation failed: ${signal_status}"

fixture_root="${tmp_dir}/fixture"
fixture_ci="${fixture_root}/scripts/ci"
fake_bin="${tmp_dir}/fake-bin"
no_uv_bin="${tmp_dir}/no-uv-bin"
clean_toolcache_bin="${tmp_dir}/clean-toolcache/bin"
empty_managed_dir="${tmp_dir}/empty-managed"
isolated_uv_bin="${tmp_dir}/isolated-uv-bin"
mkdir -p \
  "${fixture_ci}" \
  "${fake_bin}" \
  "${no_uv_bin}" \
  "${clean_toolcache_bin}" \
  "${empty_managed_dir}" \
  "${isolated_uv_bin}"
cp "${RUNNER}" "${fixture_ci}/python-runner.sh"
printf '%s\n' "${EXPECTED_PYTHON}" >"${fixture_root}/.python-version"
ln -s "$(command -v bash)" "${fake_bin}/bash"
ln -s "$(command -v dirname)" "${fake_bin}/dirname"
ln -s "$(command -v bash)" "${no_uv_bin}/bash"
ln -s "$(command -v dirname)" "${no_uv_bin}/dirname"
ln -s "${resolved}" "${clean_toolcache_bin}/python3.14"
ln -s "$(command -v uv)" "${clean_toolcache_bin}/uv"
ln -s "$(command -v bash)" "${clean_toolcache_bin}/bash"
ln -s "$(command -v dirname)" "${clean_toolcache_bin}/dirname"
ln -s "$(command -v uv)" "${isolated_uv_bin}/uv"
ln -s "$(command -v bash)" "${isolated_uv_bin}/bash"
ln -s "$(command -v dirname)" "${isolated_uv_bin}/dirname"

cat >"${fake_bin}/uv" <<'UV'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--version" ]]; then
  printf '%s\n' "${FAKE_UV_VERSION:-uv 0.12.7 (fixture)}"
  exit 0
fi
if [[ "${1:-}" == "python" && "${2:-}" == "find" ]]; then
  printf '%s\n' "${FAKE_PYTHON_PATH:?}"
  exit 0
fi
exit 64
UV
chmod +x "${fake_bin}/uv"

clean_interpreter="$(
  env -u VIRTUAL_ENV \
    PATH="${clean_toolcache_bin}" \
    UV_PYTHON_INSTALL_DIR="${empty_managed_dir}" \
    "${fixture_ci}/python-runner.sh" --print-interpreter
)"
[[ "$(readlink -f "${clean_interpreter}")" == "$(readlink -f "${resolved}")" ]] \
  || fail "clean setup-python-style interpreter selection failed"

expect_failure \
  "clean runner missing exact interpreter" \
  "No interpreter found" \
  env -u VIRTUAL_ENV \
    PATH="${isolated_uv_bin}" \
    UV_PYTHON_INSTALL_DIR="${empty_managed_dir}" \
    "${fixture_ci}/python-runner.sh" --check

ln -s /usr/bin/python3 "${isolated_uv_bin}/python3.14"
expect_failure \
  "clean runner mismatched interpreter" \
  "No interpreter found" \
  env -u VIRTUAL_ENV \
    PATH="${isolated_uv_bin}" \
    UV_PYTHON_INSTALL_DIR="${empty_managed_dir}" \
    "${fixture_ci}/python-runner.sh" --check

expect_failure "missing uv" "uv 0.12.7 is required"   env PATH="${no_uv_bin}" "${fixture_ci}/python-runner.sh" --check
expect_failure "uv mismatch" "uv version mismatch"   env PATH="${fake_bin}" FAKE_UV_VERSION="uv 0.12.6 (fixture)"   FAKE_PYTHON_PATH="${resolved}" "${fixture_ci}/python-runner.sh" --check
expect_failure "missing interpreter" "uv did not resolve"   env PATH="${fake_bin}" FAKE_PYTHON_PATH="${tmp_dir}/missing-python"   "${fixture_ci}/python-runner.sh" --check
expect_failure "resolved version mismatch" "resolved Python version mismatch"   env PATH="${fake_bin}" FAKE_PYTHON_PATH="/usr/bin/python3"   "${fixture_ci}/python-runner.sh" --check

printf '3.14.7\nextra\n' >"${fixture_root}/.python-version"
expect_failure "extra pin line" "exactly one line"   env PATH="${fake_bin}" FAKE_PYTHON_PATH="${resolved}"   "${fixture_ci}/python-runner.sh" --check
printf '3.14.6\n' >"${fixture_root}/.python-version"
expect_failure "pin mismatch" "does not match 3.14.7"   env PATH="${fake_bin}" FAKE_PYTHON_PATH="${resolved}"   "${fixture_ci}/python-runner.sh" --check

printf 'python-runner-test: ok\n'
