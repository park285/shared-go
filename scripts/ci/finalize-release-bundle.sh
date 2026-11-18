#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
. "${SCRIPT_DIR}/prepare-release-bundle.sh"

if (( $# != 4 )); then
  echo "usage: $0 <bundle-dir> <owner/repository> <tag> <commit>" >&2
  exit 2
fi

bundle_dir="$1"
release_identity "$2" "$3" "$4"

"${CI_PYTHON_BIN}" -m json.tool "${bundle_dir}/${sbom}" >/dev/null
(
  cd "$bundle_dir"
  sha256sum \
    "$archive" \
    "$sbom" \
    release-manifest.json >SHA256SUMS
  chmod 0600 SHA256SUMS
)
bash "$SCRIPT_DIR/verify-release-bundle.sh" "$bundle_dir" "$repository" "$tag" "$commit"
