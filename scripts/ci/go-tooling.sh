#!/usr/bin/env bash
set -euo pipefail
unset CDPATH
export GOWORK=off

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
. "$SCRIPT_DIR/pre-push-gate.sh"

if [[ "${1:-}" == release-modules || "${1:-}" == tidy-workspace || "${1:-}" == source-workspace || "${1:-}" == workspace-path ]]; then
  action="$1"
  shift
  exec "$SCRIPT_DIR/python-runner.sh" -- -I - "$REPO_ROOT" "${action}" "$@" <<'PY'
import difflib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
action = sys.argv[2]
if action in ("source-workspace", "workspace-path"):
    if len(sys.argv) != 4 or not sys.argv[3]:
        raise SystemExit(f"usage: {action} <go.work-path>")
    target = (root / sys.argv[3]).resolve()
    if not target.is_relative_to(root):
        raise SystemExit(f"{target}: workspace must be inside the repository")
    if "\n" in str(target) or "\r" in str(target):
        raise SystemExit("workspace path must fit on one line")
    if action == "workspace-path":
        if not target.is_file():
            raise SystemExit(f"{target}: expected a workspace file")
        workspace = json.loads(subprocess.check_output(["go", "work", "edit", "-json", target]))
        members = {(target.parent / member["DiskPath"]).resolve() for member in workspace.get("Use") or []}
        if Path.cwd() not in members:
            raise SystemExit(f"{target}: workspace does not use {Path.cwd()}")
        print(target)
        raise SystemExit(0)

modules = subprocess.check_output(
    [sys.executable, "-I", "scripts/ci/go-lint-matrix.py", "--list-modules"], cwd=root, text=True,
).splitlines()


def module_metadata(module):
    if not (root / module / "go.mod").is_file():
        raise SystemExit(f"{module}/go.mod: missing tracked manifest")
    return json.loads(subprocess.check_output(
        ["go", "mod", "edit", "-json"], cwd=root / module,
        text=True,
    ))


metadata = {module: module_metadata(module) for module in modules}
for module, document in metadata.items():
    owned = "github.com/park285/shared-go" + ("" if module == "." else "/" + module)
    declared = (document.get("Module") or {}).get("Path") or ""
    if not re.fullmatch(re.escape(owned) + r"(/v(?:[2-9]|[1-9][0-9]+))?", declared):
        raise SystemExit(f"{module}/go.mod: module path must be {owned} or its /vN form")
    if re.fullmatch(r"v[0-9]+", declared.rsplit("/", 1)[-1]) and not re.fullmatch(r"v(?:[2-9]|[1-9][0-9]+)", declared.rsplit("/", 1)[-1]):
        raise SystemExit(f"{module}/go.mod: {declared} ends in an invalid major version element")
if action == "release-modules":
    for directory in (".", "core", "postgres", "transport", "openai", "sqlite"):
        if directory not in modules:
            raise SystemExit(f"required release module missing: {directory}/go.mod")
        expected = "github.com/park285/shared-go/" + ("v2" if directory == "." else directory)
        if (root / directory / "go.mod").is_symlink():
            raise SystemExit(f"{directory}/go.mod: expected a regular release manifest")
        actual = (metadata[directory].get("Module") or {}).get("Path")
        if actual != expected:
            raise SystemExit(f"{directory}/go.mod: expected {expected}, got {actual}")
    print(*modules, sep="\n")
    raise SystemExit(0)
locations = {document["Module"]["Path"]: root / module for module, document in metadata.items()}
if action == "source-workspace":
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
selected = sys.argv[3:] or modules
if any(module not in modules for module in selected):
    raise SystemExit("source tidy: 허용되지 않은 모듈")
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
        existing = {item["Old"]["Path"] for item in metadata[module].get("Replace") or []}
        inserted = {
            path: sibling for path, sibling in locations.items()
            if sibling != directory and path not in existing
        }
        subprocess.run(
            [*command, *(f"-replace={path}={location}" for path, location in inserted.items())],
            cwd=directory, check=True,
        )
        subprocess.run(
            ["go", "mod", "tidy", f"-modfile={modfile}"],
            cwd=directory, check=True,
        )
        subprocess.run(
            [*command, *(f"-dropreplace={path}" for path in inserted)],
            cwd=directory, check=True,
        )
        for filename, actual_path in (("go.mod", modfile), ("go.sum", sumfile)):
            expected_path = directory / filename
            expected = expected_path.read_text(encoding="utf-8").splitlines(keepends=True) if expected_path.exists() else []
            actual = actual_path.read_text(encoding="utf-8").splitlines(keepends=True) if actual_path.exists() else []
            if filename == "go.sum":
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

action="${1:-}"
case "$action" in
  golangci-lint|golangci-lint-path|golangci-lint-version) tool=golangci-lint ;;
  govulncheck|govulncheck-path) tool=govulncheck ;;
  *) echo "usage: $0 <golangci-lint|golangci-lint-path|golangci-lint-version|govulncheck|govulncheck-path|release-modules|tidy-workspace|source-workspace|workspace-path> [args...]" >&2; exit 2 ;;
esac
shift
version="$(go_tool_version "$tool")"
if [[ "$action" == golangci-lint-version ]]; then
  printf '%s\n' "$version"
  exit
fi

module_dir="$REPO_ROOT"
if [[ "$PWD/" == "$REPO_ROOT/"* && -f go.mod ]]; then
  module_dir="$PWD"
fi
if [[ "$action" == *-path ]]; then
  (( $# <= 1 )) || { echo "usage: $action [module-directory]" >&2; exit 2; }
  module_dir="${1:-$module_dir}"
fi
resolver=("$SCRIPT_DIR/python-runner.sh" -- -I "$SCRIPT_DIR/resolve-go-tool.py" --tool "$tool" --version "$version" --repo "$module_dir")
if [[ "$action" == *-path ]]; then
  exec "${resolver[@]}"
fi
bin="$("${resolver[@]}")"
exec "$bin" "$@"
