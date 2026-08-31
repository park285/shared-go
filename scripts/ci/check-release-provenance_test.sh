#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

repo="$fixture/repo"
mkdir "$repo"
git -C "$repo" init -q
git -C "$repo" config user.name fixture
git -C "$repo" config user.email fixture@example.invalid
printf 'module example.invalid/release-fixture\n\ngo 1.25\n' >"$repo/go.mod"
printf 'fixture\n' >"$repo/README.md"
git -C "$repo" add go.mod README.md
git -C "$repo" commit -qm fixture
git -C "$repo" tag v1.2.3
commit="$(git -C "$repo" rev-parse HEAD)"
remote="$fixture/remote.git"
git init -q --bare "$remote"
git -C "$repo" remote add origin "$remote"
git -C "$repo" push -q -u origin HEAD:main --tags

git -C "$repo" update-ref -d refs/remotes/origin/main
if (
  cd "$repo"
  bash "$root/scripts/ci/prepare-release-bundle.sh" owner/release-fixture v1.2.3 "$commit" missing-authority
) >/dev/null 2>&1; then
  echo "release provenance test: missing canonical origin/main was accepted" >&2
  exit 1
fi
git -C "$repo" fetch -q origin refs/heads/main:refs/remotes/origin/main

(
  cd "$repo"
  env -u GITHUB_REF -u GITHUB_SHA \
    bash "$root/scripts/ci/prepare-release-bundle.sh" owner/release-fixture v1.2.3 "$commit" dist
  printf '{"spdxVersion":"SPDX-2.3","SPDXID":"SPDXRef-DOCUMENT"}\n' \
    >dist/release-fixture-v1.2.3.spdx.json
  bash "$root/scripts/ci/finalize-release-bundle.sh" dist owner/release-fixture v1.2.3 "$commit"
  bash "$root/scripts/ci/verify-release-bundle.sh" dist owner/release-fixture v1.2.3 "$commit"

  cp -R dist tampered
  printf 'tampered\n' >>tampered/release-fixture-v1.2.3.tar.gz
  if bash "$root/scripts/ci/verify-release-bundle.sh" tampered owner/release-fixture v1.2.3 "$commit" >/dev/null 2>&1; then
    echo "release provenance test: modified archive was accepted" >&2
    exit 1
  fi
  if bash "$root/scripts/ci/verify-release-bundle.sh" dist attacker/release-fixture v1.2.3 "$commit" >/dev/null 2>&1; then
    echo "release provenance test: wrong repository identity was accepted" >&2
    exit 1
  fi
  if bash "$root/scripts/ci/verify-release-bundle.sh" dist owner/release-fixture v1.2.3 0000000000000000000000000000000000000000 >/dev/null 2>&1; then
    echo "release provenance test: wrong commit was accepted" >&2
    exit 1
  fi

  printf 'different commit tree\n' >README.md
  git add README.md
  git commit -qm alternate
  alternate_commit="$(git rev-parse HEAD)"
  cp -R dist retargeted
  git archive --format=tar --prefix='release-fixture-v1.2.3/' "$alternate_commit" \
    | gzip -n >retargeted/release-fixture-v1.2.3.tar.gz
  (
    cd retargeted
    sha256sum \
      release-fixture-v1.2.3.tar.gz \
      release-fixture-v1.2.3.spdx.json \
      release-manifest.json >SHA256SUMS
  )
  if bash "$root/scripts/ci/verify-release-bundle.sh" retargeted owner/release-fixture v1.2.3 "$commit" >/dev/null 2>&1; then
    echo "release provenance test: alternate commit archive with recomputed checksums was accepted" >&2
    exit 1
  fi
)

fake_bin="$fixture/bin"
mkdir "$fake_bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$fake_bin/gh"
chmod +x "$fake_bin/gh"
if (
  cd "$repo"
  PATH="$fake_bin:$PATH" bash "$root/scripts/ci/publish-release.sh" \
    owner/release-fixture v1.2.3 "$commit" dist >/dev/null 2>&1
); then
  echo "release provenance test: duplicate release was accepted" >&2
  exit 1
fi

echo "release provenance fixture tests passed"
