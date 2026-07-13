#!/usr/bin/env bash

run_checker() {
  local baseline="$1"
  local candidate="$2"
  local policy="$3"
  local fixture_root
  fixture_root="$(dirname "${policy}")"

  prepare_strict_evidence "${fixture_root}" "${policy}" "${baseline}" "${candidate}"
  set +e
  LAST_OUTPUT="$(cd "${fixture_root}" && "${CHECKER}" --baseline "${baseline}" --candidate "${candidate}" --policy "${policy}" --gate pr --gate-id fixture-gate 2>&1)"
  LAST_STATUS=$?
  set -e
}

setup_strict_fixture_repo() {
  local root="$1"
  local policy="$2"
  local repo_name

  if [[ -d "${root}/.git" ]]; then
    return
  fi

  repo_name="$(basename "${root}")"
  sed -i "s/^repo: .*/repo: ${repo_name}/" "${policy}"
  mkdir -p "${root}/fixture/a" "${root}/fixture/b"
  cat >"${root}/go.mod" <<EOF
module ${repo_name}

go 1.22
EOF
  cat >"${root}/fixture/bench_test.go" <<'EOF'
package fixture

import "testing"

func BenchmarkTarget(b *testing.B) { for range b.N {} }
func BenchmarkPresent(b *testing.B) { for range b.N {} }
func BenchmarkMissing(b *testing.B) { for range b.N {} }
EOF
  for package_dir in a b; do
    cat >"${root}/fixture/${package_dir}/bench_test.go" <<'EOF'
package fixture

import "testing"

func BenchmarkTarget(b *testing.B) { for range b.N {} }
EOF
  done
  cat >"${root}/.gitignore" <<'EOF'
artifacts/
baseline/
candidate/
EOF
  gofmt -w "${root}/fixture/bench_test.go" "${root}/fixture/a/bench_test.go" "${root}/fixture/b/bench_test.go"
  git -C "${root}" init -q
  git -C "${root}" config user.email benchgate@example.invalid
  git -C "${root}" config user.name benchgate
  git -C "${root}" add .gitignore go.mod fixture "$(basename "${policy}")"
  git -C "${root}" commit -qm fixture
}

install_strict_manifest() {
  local template_manifest="$1"
  local target_root="$2"
  local manifest_name="$3"

  python3 - "${template_manifest}" "${target_root}" "${manifest_name}" <<'PY'
import hashlib
import json
import os
import sys

template_path, target_root, manifest_name = sys.argv[1:]
with open(template_path, encoding="utf-8") as handle:
    manifest = json.load(handle)

files = {}
for current_root, dirs, names in os.walk(target_root, followlinks=False):
    dirs.sort()
    names.sort()
    for name in names:
        if name in {"candidate-manifest.json", "baseline-manifest.json"}:
            continue
        path = os.path.join(current_root, name)
        if not os.path.isfile(path) or os.path.islink(path):
            continue
        relative = os.path.relpath(path, target_root).replace(os.sep, "/")
        with open(path, "rb") as handle:
            files[relative] = hashlib.sha256(handle.read()).hexdigest()

manifest["files"] = files
output_path = os.path.join(target_root, manifest_name)
with open(output_path, "w", encoding="utf-8") as handle:
    json.dump(manifest, handle, ensure_ascii=False, indent=2, sort_keys=True)
    handle.write("\n")
PY
}

prepare_strict_evidence() {
  local root="$1"
  local policy="$2"
  local baseline="$3"
  local candidate="$4"
  local template_candidate="artifacts/perf/template-candidate"
  local template_baseline="artifacts/perf/template-baseline"
  local approved_sha collect_status

  setup_strict_fixture_repo "${root}" "${policy}"
  approved_sha="$(git -C "${root}" rev-parse HEAD)"
  rm -rf "${root}/${template_candidate}" "${root}/${template_baseline}"

  set +e
  (cd "${root}" && "${CHECKER}" collect \
    --policy "$(basename "${policy}")" \
    --candidate "${template_candidate}" \
    --gate pr \
    --gate-id fixture-gate \
    --count 2 \
    --benchtime 1ms) >/dev/null 2>&1
  collect_status=$?
  set -e
  if [[ "${collect_status}" -ne 0 ]]; then
    return
  fi

  (cd "${root}" && "${CHECKER}" bootstrap-baseline \
    --policy "$(basename "${policy}")" \
    --baseline "${template_baseline}" \
    --candidate "${template_candidate}" \
    --gate pr \
    --gate-id fixture-gate \
    --approved-sha "${approved_sha}") >/dev/null

  if [[ -d "${candidate}" ]]; then
    install_strict_manifest \
      "${root}/${template_candidate}/candidate-manifest.json" \
      "${candidate}" \
      candidate-manifest.json
  fi
  if [[ -d "${baseline}" ]]; then
    install_strict_manifest \
      "${root}/${template_baseline}/baseline-manifest.json" \
      "${baseline}" \
      baseline-manifest.json
  fi
}
