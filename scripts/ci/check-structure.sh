#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${repo_root}/scripts/ci/python-runtime.sh"
repo_python_init
root="$repo_root"
args=()
while (($#)); do
  case "$1" in
    --root) root="$2"; args+=("$1" "$2"); shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
exec "${CI_PYTHON_BIN}" "$root/scripts/structure/go_responsibility.py" \
  --policy "$root/scripts/structure/policy.json" "${args[@]}"
