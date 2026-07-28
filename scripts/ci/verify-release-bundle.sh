#!/usr/bin/env bash
set -euo pipefail

if (( $# != 4 )); then
  echo "usage: $0 <bundle-dir> <owner/repository> <tag> <commit>" >&2
  exit 2
fi

bundle_dir="$1"
repository="$2"
tag="$3"
commit="$4"
repository_name="${repository##*/}"
archive="${repository_name}-${tag}.tar.gz"
sbom="${repository_name}-${tag}.spdx.json"

python3 - "$bundle_dir/release-manifest.json" "$repository" "$tag" "$commit" "$archive" "$sbom" <<'PY'
import json
import sys

path, repository, tag, commit, archive, sbom = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    manifest = json.load(handle)
expected = {
    "schemaVersion": 1,
    "repository": repository,
    "ref": f"refs/tags/{tag}",
    "tag": tag,
    "commit": commit,
    "sourceArchive": archive,
    "sbom": sbom,
}
if manifest != expected:
    raise SystemExit("release bundle: manifest identity mismatch")
PY

python3 - "$bundle_dir/$sbom" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    sbom = json.load(handle)
if not str(sbom.get("spdxVersion", "")).startswith("SPDX-2."):
    raise SystemExit("release bundle: SBOM is not SPDX JSON")
if sbom.get("SPDXID") != "SPDXRef-DOCUMENT":
    raise SystemExit("release bundle: SBOM document identity is missing")
PY

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

if tar -tzf "$bundle_dir/$archive" | awk -v prefix="${repository_name}-${tag}/" \
  'index($0, prefix) != 1 { exit 1 }'; then
  :
else
  echo "release bundle: source archive contains an unexpected root" >&2
  exit 1
fi
