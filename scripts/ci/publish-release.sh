#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if (( $# != 4 )); then
  echo "usage: $0 <owner/repository> <tag> <commit> <bundle-dir>" >&2
  exit 2
fi

repository="$1"
tag="$2"
commit="$3"
bundle_dir="$4"
repository_name="${repository##*/}"

bash "$SCRIPT_DIR/verify-release-bundle.sh" "$bundle_dir" "$repository" "$tag" "$commit"
if gh release view "$tag" --repo "$repository" >/dev/null 2>&1; then
  echo "release publish: ${tag} already exists; refusing duplicate or rerun" >&2
  exit 1
fi

gh release create "$tag" \
  "$bundle_dir/${repository_name}-${tag}.tar.gz" \
  "$bundle_dir/${repository_name}-${tag}.spdx.json" \
  "$bundle_dir/release-manifest.json" \
  "$bundle_dir/SHA256SUMS" \
  --repo "$repository" \
  --verify-tag \
  --title "$tag" \
  --generate-notes
