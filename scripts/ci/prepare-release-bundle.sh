#!/usr/bin/env bash
set -euo pipefail

if (( $# != 4 )); then
  echo "usage: $0 <owner/repository> <tag> <commit> <output-dir>" >&2
  exit 2
fi

repository="$1"
tag="$2"
commit="$3"
output_dir="$4"

[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || {
  echo "release bundle: invalid repository identity" >&2
  exit 1
}
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "release bundle: tag must be strict semver" >&2
  exit 1
}
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
  echo "release bundle: commit must be a full lowercase Git SHA" >&2
  exit 1
}
[[ "$(git rev-parse --verify HEAD)" == "$commit" ]] || {
  echo "release bundle: checkout does not match requested commit" >&2
  exit 1
}
[[ "$(git rev-parse --verify "refs/tags/${tag}^{commit}")" == "$commit" ]] || {
  echo "release bundle: tag target does not match requested commit" >&2
  exit 1
}
if [[ -n "${GITHUB_REF:-}" && "$GITHUB_REF" != "refs/tags/${tag}" ]]; then
  echo "release bundle: GitHub ref does not match requested tag" >&2
  exit 1
fi
if [[ -n "${GITHUB_SHA:-}" && "$GITHUB_SHA" != "$commit" ]]; then
  echo "release bundle: GitHub SHA does not match requested commit" >&2
  exit 1
fi
if git show-ref --verify --quiet refs/remotes/origin/main \
  && ! git merge-base --is-ancestor "$commit" refs/remotes/origin/main; then
  echo "release bundle: tag target is not contained in origin/main" >&2
  exit 1
fi
if [[ -e "$output_dir" ]]; then
  echo "release bundle: output path already exists" >&2
  exit 1
fi

repository_name="${repository##*/}"
archive="${repository_name}-${tag}.tar.gz"
sbom="${repository_name}-${tag}.spdx.json"
mkdir -m 0700 "$output_dir"
git archive --format=tar --prefix="${repository_name}-${tag}/" "$commit" \
  | gzip -n >"${output_dir}/${archive}"
chmod 0600 "${output_dir}/${archive}"

printf '{"schemaVersion":1,"repository":"%s","ref":"refs/tags/%s","tag":"%s","commit":"%s","sourceArchive":"%s","sbom":"%s"}\n' \
  "$repository" "$tag" "$tag" "$commit" "$archive" "$sbom" \
  >"${output_dir}/release-manifest.json"
chmod 0600 "${output_dir}/release-manifest.json"
