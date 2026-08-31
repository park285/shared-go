#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$repo_root/scripts/ci/check-readme-package-catalog.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

bash "$checker"

awk '$0 !~ /^\| `pkg\/ginjson` /' "$repo_root/README.md" >"$tmp_dir/missing-package.md"
if bash "$checker" "$tmp_dir/missing-package.md" >/dev/null 2>&1; then
  echo "catalog checker accepted a missing package" >&2
  exit 1
fi

awk '{ print; if ($0 ~ /^\| `pkg\/ginjson` /) print }' "$repo_root/README.md" >"$tmp_dir/duplicate-package.md"
if bash "$checker" "$tmp_dir/duplicate-package.md" >/dev/null 2>&1; then
  echo "catalog checker accepted a duplicate package" >&2
  exit 1
fi
