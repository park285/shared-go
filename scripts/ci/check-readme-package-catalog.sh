#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readme="${1:-$repo_root/README.md}"

module_path="$(GOWORK=off go -C "$repo_root" list -m -f '{{.Path}}')"
actual="$({
  GOWORK=off go -C "$repo_root" list -f '{{.ImportPath}}' ./pkg/...
} | awk -v prefix="$module_path/" '
  index($0, "/internal/") == 0 && $0 !~ /\/internal$/ {
    sub("^" prefix, "")
    print
  }
' | LC_ALL=C sort)"

documented="$(awk '
  /^## 제공 패키지 목록 / { in_catalog = 1; next }
  in_catalog && /^## / { exit }
  in_catalog && /^\| `pkg\// {
    path = $0
    sub(/^\| `/, "", path)
    sub(/`.*/, "", path)
    print path
  }
' "$readme" | LC_ALL=C sort)"

if [[ -z "$documented" ]]; then
  echo "README package catalog is empty or missing" >&2
  exit 1
fi

if [[ "$actual" != "$documented" ]]; then
  echo "README package catalog differs from public go list ./pkg/..." >&2
  diff -u <(printf '%s\n' "$actual") <(printf '%s\n' "$documented") >&2 || true
  exit 1
fi
