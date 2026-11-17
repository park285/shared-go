#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/prepare-release-bundle.sh
. "${SCRIPT_DIR}/prepare-release-bundle.sh"

if (( $# != 4 )); then
  echo "usage: $0 <owner/repository> <tag> <commit> <bundle-dir>" >&2
  exit 2
fi

release_identity "$1" "$2" "$3"
bundle_dir="$4"

bash "$SCRIPT_DIR/verify-release-bundle.sh" "$bundle_dir" "$repository" "$tag" "$commit"
existing_tags="$(gh api --paginate "repos/${repository}/releases" --jq '.[].tag_name')"
if grep -Fxq -- "$tag" <<<"$existing_tags"; then
  echo "release publish: ${tag} already exists; refusing duplicate or rerun" >&2
  exit 1
fi

for artifact in "$bundle_dir"/*; do
  gh attestation verify "$artifact" \
    --repo "$repository" \
    --signer-workflow "${repository}/.github/workflows/release.yml" \
    --cert-oidc-issuer https://token.actions.githubusercontent.com \
    --source-ref "refs/tags/${tag}" \
    --source-digest "$commit" \
    --deny-self-hosted-runners >/dev/null
done
gh attestation verify "$bundle_dir/$archive" \
  --repo "$repository" \
  --signer-workflow "${repository}/.github/workflows/release.yml" \
  --cert-oidc-issuer https://token.actions.githubusercontent.com \
  --source-ref "refs/tags/${tag}" \
  --source-digest "$commit" \
  --predicate-type https://spdx.dev/Document/v2.3 \
  --deny-self-hosted-runners >/dev/null

latest_args=()
if [[ "$module_dir" != "." ]]; then
  latest_args=(--latest=false)
fi

gh release create "$tag" \
  "$bundle_dir/$archive" \
  "$bundle_dir/$sbom" \
  "$bundle_dir/release-manifest.json" \
  "$bundle_dir/SHA256SUMS" \
  --repo "$repository" \
  --verify-tag \
  --title "$tag" \
  "${latest_args[@]}" \
  --generate-notes

gh release verify "$tag" --repo "$repository"
