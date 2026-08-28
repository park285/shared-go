#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init

if (( $# != 4 )); then
  echo "usage: $0 <bundle-dir> <owner/repository> <tag> <commit>" >&2
  exit 2
fi

bundle_dir="$1"
repository="$2"
tag="$3"
commit="$4"
repository_name="${repository##*/}"

"${CI_PYTHON_BIN}" -m json.tool "${bundle_dir}/${repository_name}-${tag}.spdx.json" >/dev/null
(
  cd "$bundle_dir"
  sha256sum \
    "${repository_name}-${tag}.tar.gz" \
    "${repository_name}-${tag}.spdx.json" \
    release-manifest.json >SHA256SUMS
  chmod 0600 SHA256SUMS
)
bash "$SCRIPT_DIR/verify-release-bundle.sh" "$bundle_dir" "$repository" "$tag" "$commit"
