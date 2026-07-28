# Release provenance

Future releases use GitHub Actions OIDC and Sigstore keyless certificates as the signing authority.
No workstation or long-lived organization signing key is copied into CI. Existing tags and releases
are not moved, recreated, or retroactively signed.

The tag-only `.github/workflows/release.yml` accepts strict `vMAJOR.MINOR.PATCH` tags whose commit is
already contained in `main`. Its publish job alone receives `contents: write`, `id-token: write`, and
`attestations: write`; pull request, CI, and security workflows remain read-only and cannot mint a
release attestation.

Each release contains a deterministic source archive, SPDX JSON SBOM, identity manifest, and
`SHA256SUMS`. A provenance attestation covers every asset, and a separate SPDX attestation binds the
SBOM to the source archive. The workflow verifies repository, signer workflow, tag ref, commit SHA,
and GitHub-hosted runner identity before publication. GitHub release immutability then locks the tag
and assets and creates the release-level attestation. A duplicate release causes a hard failure.

Verify a downloaded release without a checksum-only fallback:

```bash
tag=vNEXT
repo=park285/shared-go
gh release download "$tag" --repo "$repo" --dir release
(
  cd release
  sha256sum --check --strict SHA256SUMS
)
gh attestation verify "release/shared-go-${tag}.tar.gz" \
  --repo "$repo" \
  --signer-workflow "$repo/.github/workflows/release.yml" \
  --source-ref "refs/tags/${tag}" \
  --source-digest "$(git rev-list -n 1 "$tag")" \
  --deny-self-hosted-runners
gh attestation verify "release/shared-go-${tag}.tar.gz" \
  --repo "$repo" \
  --signer-workflow "$repo/.github/workflows/release.yml" \
  --source-ref "refs/tags/${tag}" \
  --source-digest "$(git rev-list -n 1 "$tag")" \
  --predicate-type https://spdx.dev/Document/v2.3 \
  --deny-self-hosted-runners
gh release verify "$tag" --repo "$repo"
```

All three cryptographic checks are required. A checksum match alone is not acceptance. Consumer
version bumps begin only after the immutable release and these verification commands succeed.
