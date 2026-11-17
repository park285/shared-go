#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init
# shellcheck source=scripts/ci/prepare-release-bundle.sh
. "${SCRIPT_DIR}/prepare-release-bundle.sh"

if (( $# != 4 )); then
  echo "usage: $0 <bundle-dir> <owner/repository> <tag> <commit>" >&2
  exit 2
fi

bundle_dir="$1"
release_identity "$2" "$3" "$4"
release_source_identity

"${CI_PYTHON_BIN}" - "$bundle_dir/release-manifest.json" "$repository" "$tag" "$commit" "$archive" "$sbom" "$module_path" "$module_dir" "$version" "$artifact_name" <<'PY'
import json
import sys
from pathlib import Path

path, repository, tag, commit, archive, sbom, module, directory, version, artifact = sys.argv[1:]
manifest = json.loads(Path(path).read_text(encoding="utf-8"))
expected = {
    "schemaVersion": 2,
    "repository": repository,
    "ref": f"refs/tags/{tag}",
    "tag": tag,
    "commit": commit,
    "module": module,
    "moduleDirectory": directory,
    "version": version,
    "artifactName": artifact,
    "signerWorkflow": f"{repository}/.github/workflows/release.yml",
    "oidcIssuer": "https://token.actions.githubusercontent.com",
    "sourceArchive": archive,
    "sbom": sbom,
}
if manifest != expected:
    raise SystemExit("release bundle: manifest identity mismatch")
PY

"${CI_PYTHON_BIN}" - "$bundle_dir/$sbom" <<'PY'
import json
import sys
from pathlib import Path

sbom = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
if not str(sbom.get("spdxVersion", "")).startswith("SPDX-2."):
    raise SystemExit("release bundle: SBOM is not SPDX JSON")
if sbom.get("SPDXID") != "SPDXRef-DOCUMENT":
    raise SystemExit("release bundle: SBOM document identity is missing")
PY

actual_files=$(find "$bundle_dir" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | sort)
expected_files=$(printf '%s\n' "$archive" "$sbom" release-manifest.json SHA256SUMS | sort)
[[ "$actual_files" == "$expected_files" && "$(find "$bundle_dir" -mindepth 1 -maxdepth 1 ! -type f -print | wc -l)" == 0 ]] || {
  echo "release bundle: unexpected asset or non-regular file" >&2
  exit 1
}

expected_names=$(printf '%s\n' "$archive" "$sbom" release-manifest.json | sort)
actual_names=$(awk 'NF == 2 && $1 ~ /^[0-9a-f]{64}$/ {name=$2; sub(/^\*/, "", name); print name}' \
  "$bundle_dir/SHA256SUMS" | sort)
[[ "$actual_names" == "$expected_names" ]] || {
  echo "release bundle: checksum subject set mismatch" >&2
  exit 1
}
(
  cd "$bundle_dir"
  sha256sum --check --strict SHA256SUMS >/dev/null
)

archive_check_dir=$(mktemp -d)
trap 'rm -rf "$archive_check_dir"' EXIT
if ! release_archive >"$archive_check_dir/expected.tar"; then
  echo "release bundle: failed to archive the expected commit" >&2
  exit 1
fi
if ! gzip -cd -- "$bundle_dir/$archive" >"$archive_check_dir/actual.tar"; then
  echo "release bundle: source archive is not valid gzip" >&2
  exit 1
fi
if ! cmp -s "$archive_check_dir/expected.tar" "$archive_check_dir/actual.tar"; then
  echo "release bundle: source archive does not match the manifest commit tree" >&2
  exit 1
fi

if tar -tzf "$bundle_dir/$archive" | awk -v prefix="${artifact_name}/" \
  'index($0, prefix) != 1 { exit 1 }'; then
  :
else
  echo "release bundle: source archive contains an unexpected root" >&2
  exit 1
fi
