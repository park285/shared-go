#!/usr/bin/env bash
set -euo pipefail

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.15.0}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.9.0}"

go_bin_tool() {
  local tool="$1" minimum probe pattern
  local paths gobin gopath bin output invalid_bin=""
  local required_major required_minor required_patch
  local -a candidates=()

  case "${tool}" in
    golangci-lint)
      minimum="${GOLANGCI_LINT_VERSION}"
      probe=version
      pattern='version[[:space:]]+([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
      ;;
    govulncheck)
      minimum="${GOVULNCHECK_VERSION}"
      probe=-version
      pattern='govulncheck@v([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
      ;;
    *) echo "unsupported Go tool: ${tool}" >&2; return 1 ;;
  esac
  [[ "${minimum}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
    echo "invalid minimum version for ${tool}: ${minimum}" >&2
    return 1
  }
  IFS=. read -r required_major required_minor required_patch <<<"${minimum#v}"

  paths="$(go env GOBIN GOPATH)" || return
  gobin="${paths%%$'\n'*}"
  gopath="${paths#*$'\n'}"
  [[ -z "${gobin}" ]] || candidates+=("${gobin}/${tool}")
  [[ -z "${gopath}" ]] || candidates+=("${gopath}/bin/${tool}")
  bin="$(command -v "${tool}" || true)"
  [[ -z "${bin}" ]] || candidates+=("${bin}")

  for bin in "${candidates[@]}"; do
    [[ -x "${bin}" ]] || continue
    if ! output="$("${bin}" "${probe}" 2>/dev/null)" || [[ ! "${output}" =~ ${pattern} ]]; then
      invalid_bin="${bin}"
      continue
    fi
    if (( 10#${BASH_REMATCH[1]} > 10#${required_major}
      || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} > 10#${required_minor})
      || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} == 10#${required_minor}
        && 10#${BASH_REMATCH[3]} >= 10#${required_patch}) )); then
      printf '%s\n' "${bin}"
      return
    fi
  done
  if [[ -n "${invalid_bin}" ]]; then
    echo "cannot verify ${tool} version: ${invalid_bin}" >&2
    return 1
  fi
}

ensure_golangci_lint() {
  local bin
  bin="$(go_bin_tool golangci-lint)" || return
  if [[ -z "${bin}" ]]; then
    echo "[GO TOOLING] Installing golangci-lint@${GOLANGCI_LINT_VERSION}" >&2
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" || return
    bin="$(go_bin_tool golangci-lint)" || return
  fi
  [[ -n "${bin}" ]] || { echo "golangci-lint >= ${GOLANGCI_LINT_VERSION} is required" >&2; return 1; }
  printf '%s\n' "${bin}"
}

ensure_govulncheck() {
  local bin
  bin="$(go_bin_tool govulncheck)" || return
  if [[ -z "${bin}" ]]; then
    echo "[GO TOOLING] Installing govulncheck@${GOVULNCHECK_VERSION}" >&2
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" || return
    bin="$(go_bin_tool govulncheck)" || return
  fi
  [[ -n "${bin}" ]] || { echo "govulncheck >= ${GOVULNCHECK_VERSION} is required" >&2; return 1; }
  printf '%s\n' "${bin}"
}

if [[ "${1:-}" == tidy-workspace || "${1:-}" == source-workspace ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
  . "${SCRIPT_DIR}/python-runtime.sh"
  repo_python_init
  action="$1"
  shift
  exec "${CI_PYTHON_BIN}" - "${SCRIPT_DIR}/../.." "${action}" "$@" <<'PY'
import difflib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
modules = {
    ".": "github.com/park285/shared-go/v2",
    "core": "github.com/park285/shared-go/core",
    "postgres": "github.com/park285/shared-go/postgres",
    "transport": "github.com/park285/shared-go/transport",
    "openai": "github.com/park285/shared-go/openai",
}
action = sys.argv[2]
if action == "source-workspace":
    if len(sys.argv) != 4:
        raise SystemExit("usage: source-workspace <new-go.work-path>")
    target = Path(sys.argv[3]).resolve()
    metadata = {
        module: json.loads(subprocess.run(
            ["go", "mod", "edit", "-json"], cwd=root / module,
            env=os.environ | {"GOWORK": "off"}, check=True, capture_output=True, text=True,
        ).stdout)
        for module in modules
    }
    locations = {path: root / module for module, path in modules.items()}
    replacements = sorted({
        (requirement["Path"], requirement["Version"])
        for document in metadata.values()
        for requirement in document.get("Require") or []
        if requirement["Path"] in locations
    })
    contents = f"go {metadata['.']['Go']}\n\nuse (\n"
    contents += "".join(f"\t{json.dumps(str(root / module))}\n" for module in modules)
    contents += ")\n\nreplace (\n"
    contents += "".join(
        f"\t{path} {version} => {json.dumps(str(locations[path]))}\n"
        for path, version in replacements
    )
    contents += ")\n"
    with target.open("x", encoding="utf-8") as output:
        output.write(contents)
    raise SystemExit(0)
selected = sys.argv[3:] or list(modules)
if any(module not in modules for module in selected):
    raise SystemExit("source tidy: 허용되지 않은 모듈")
if any(not (root / module / "go.mod").is_file() for module in modules):
    raise SystemExit("source tidy: checkout 모듈 go.mod 없음")
environment = os.environ | {"GOWORK": "off"}
failed = False
for module in selected:
    directory = root / module
    with tempfile.TemporaryDirectory(prefix="shared-go-source-tidy-") as temporary:
        modfile = Path(temporary) / "go.mod"
        sumfile = modfile.with_suffix(".sum")
        shutil.copyfile(directory / "go.mod", modfile)
        if (directory / "go.sum").exists():
            shutil.copyfile(directory / "go.sum", sumfile)
        command = ["go", "mod", "edit", f"-modfile={modfile}"]
        metadata = json.loads(subprocess.run(
            [*command, "-json"], cwd=directory, env=environment,
            check=True, capture_output=True, text=True,
        ).stdout)
        existing = {item["Old"]["Path"] for item in metadata.get("Replace") or []}
        inserted = {
            path: root / sibling for sibling, path in modules.items()
            if sibling != module and path not in existing
        }
        # 실제 manifest는 건드리지 않고 이 임시 modfile에 삽입한 replace만 제거한다.
        subprocess.run(
            [*command, *(f"-replace={path}={location}" for path, location in inserted.items())],
            cwd=directory, env=environment, check=True,
        )
        subprocess.run(
            ["go", "mod", "tidy", f"-modfile={modfile}"],
            cwd=directory, env=environment, check=True,
        )
        subprocess.run(
            [*command, *(f"-dropreplace={path}" for path in inserted)],
            cwd=directory, env=environment, check=True,
        )
        for filename, actual_path in (("go.mod", modfile), ("go.sum", sumfile)):
            expected_path = directory / filename
            expected = expected_path.read_text(encoding="utf-8").splitlines(keepends=True) if expected_path.exists() else []
            actual = actual_path.read_text(encoding="utf-8").splitlines(keepends=True) if actual_path.exists() else []
            if filename == "go.sum":
                # 로컬 sibling의 게시 checksum은 source 검사로 입증할 수 없다.
                expected = [line for line in expected if line.split()[0] not in inserted]
                actual = [line for line in actual if line.split()[0] not in inserted]
            if expected != actual:
                failed = True
                sys.stdout.writelines(difflib.unified_diff(
                    expected, actual, fromfile=f"{module}/{filename}",
                    tofile=f"{module}/{filename} (source tidy)",
                ))
raise SystemExit(1 if failed else 0)
PY
fi

tool="${1:-}"
[[ -n "${tool}" ]] || { echo "usage: $0 <golangci-lint|govulncheck|tidy-workspace|source-workspace> [args...]" >&2; exit 2; }
shift
case "${tool}" in
  golangci-lint) bin="$(ensure_golangci_lint)" ;;
  govulncheck) bin="$(ensure_govulncheck)" ;;
  *) echo "unsupported Go tool: ${tool}" >&2; exit 2 ;;
esac
exec "${bin}" "$@"
