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

# 게시 응답 직후 GitHub의 릴리스 attestation 조회는 404나 빈 배열일 수 있다.
# 조회 공백만 기다리며 게시·암호학적 검증 자체는 재시도하지 않는다.
tag_object="$(git rev-parse --verify "refs/tags/${tag}")"
attestation_deadline=$((SECONDS + 120))
while true; do
  if attestation_response="$(gh api --include \
    "repos/${repository}/attestations/sha1:${tag_object}?predicate_type=release" \
    --jq '.attestations | if type == "array" then length else error("invalid attestation response") end')"; then
    attestation_count="${attestation_response##*$'\n'}"
  elif [[ "${attestation_response%%$'\n'*}" == HTTP/*" 404 "* ]]; then
    attestation_count=0
  else
    exit 1
  fi
  if [[ "$attestation_count" != 0 ]]; then
    break
  fi
  if (( SECONDS >= attestation_deadline )); then
    echo "release publish: ${tag} was published but its release attestation is unavailable" >&2
    exit 1
  fi
  sleep 5
done

gh release verify "$tag" --repo "$repository"
