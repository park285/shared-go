#!/usr/bin/env bash
set -euo pipefail

# 기존 직접 스크립트들이 같은 태그·모듈·산출물 규칙을 사용한다.
release_identity() {
  repository="$1"
  tag="$2"
  commit="$3"
  [[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || {
    echo "release bundle: invalid repository identity" >&2; return 1;
  }
  case "$tag" in
    v2.*) module_name=root; module_dir=.; module_path="github.com/${repository}/v2"; version="$tag" ;;
    core/v1.*|postgres/v1.*|transport/v1.*|openai/v1.*)
      module_name="${tag%%/*}"
      module_dir="$module_name"
      module_path="github.com/${repository}/${module_name}"
      version="${tag#*/}"
      ;;
    *) echo "release bundle: unsupported module/tag pair" >&2; return 1 ;;
  esac
  [[ "$version" =~ ^v(1|2)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
    echo "release bundle: tag must be strict stable semver without leading zeroes" >&2; return 1;
  }
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
    echo "release bundle: commit must be a full lowercase Git SHA" >&2; return 1;
  }
  repository_name="${repository##*/}"
  artifact_name="${repository_name}-${module_name}-${version}"
  archive="${artifact_name}.tar.gz"
  sbom="${artifact_name}.spdx.json"
}

release_source_identity() {
  [[ "$(git rev-parse --verify HEAD)" == "$commit" ]] || {
    echo "release bundle: checkout does not match requested commit" >&2; return 1;
  }
  [[ "$(git rev-parse --verify "refs/tags/${tag}^{commit}")" == "$commit" ]] || {
    echo "release bundle: tag target does not match requested commit" >&2; return 1;
  }
  if [[ -n "${GITHUB_REF:-}" && "$GITHUB_REF" != "refs/tags/${tag}" ]]; then
    echo "release bundle: GitHub ref does not match requested tag" >&2; return 1
  fi
  if [[ -n "${GITHUB_SHA:-}" && "$GITHUB_SHA" != "$commit" ]]; then
    echo "release bundle: GitHub SHA does not match requested commit" >&2; return 1
  fi
  git show-ref --verify --quiet refs/remotes/origin/main || {
    echo "release bundle: canonical origin/main authority is unavailable" >&2; return 1;
  }
  git merge-base --is-ancestor "$commit" refs/remotes/origin/main || {
    echo "release bundle: tag target is not contained in origin/main" >&2; return 1;
  }
  local module_prefix=""
  [[ "$module_dir" == . ]] || module_prefix="${module_dir}/"
  local metadata
  for metadata in "${module_prefix}go.mod" "${module_prefix}go.sum" LICENSE THIRD_PARTY_NOTICES.md; do
    git cat-file -e "${commit}:${metadata}" || {
      echo "release bundle: required module/license metadata is missing" >&2; return 1;
    }
  done
  [[ "$(git show "${commit}:${module_prefix}go.mod" | awk '$1 == "module" {print $2}')" == "$module_path" ]] || {
    echo "release bundle: module path does not match tag" >&2; return 1;
  }
}

release_archive() {
  if [[ "$module_dir" == . ]]; then
    git archive --format=tar --prefix="${artifact_name}/" "$commit" -- . \
      ':(exclude)core' ':(exclude)postgres' ':(exclude)transport' ':(exclude)openai'
  else
    git archive --format=tar --prefix="${artifact_name}/" "$commit" -- \
      "$module_dir" LICENSE THIRD_PARTY_NOTICES.md
  fi
}

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  return
fi

if (( $# != 4 )); then
  echo "usage: $0 <owner/repository> <tag> <commit> <output-dir>" >&2
  exit 2
fi

release_identity "$1" "$2" "$3"
output_dir="$4"
release_source_identity
if [[ -e "$output_dir" ]]; then
  echo "release bundle: output path already exists" >&2
  exit 1
fi

mkdir -m 0700 "$output_dir"
release_archive | gzip -n >"${output_dir}/${archive}"
chmod 0600 "${output_dir}/${archive}"

printf '{"schemaVersion":2,"repository":"%s","ref":"refs/tags/%s","tag":"%s","commit":"%s","module":"%s","moduleDirectory":"%s","version":"%s","artifactName":"%s","signerWorkflow":"%s/.github/workflows/release.yml","oidcIssuer":"https://token.actions.githubusercontent.com","sourceArchive":"%s","sbom":"%s"}\n' \
  "$repository" "$tag" "$tag" "$commit" "$module_path" "$module_dir" "$version" "$artifact_name" "$repository" "$archive" "$sbom" \
  >"${output_dir}/release-manifest.json"
chmod 0600 "${output_dir}/release-manifest.json"
