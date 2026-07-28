#!/usr/bin/env bash
set -euo pipefail

python3 - <<'PY'
from pathlib import Path
import re

release = Path(".github/workflows/release.yml").read_text(encoding="utf-8")
ci = Path(".github/workflows/ci.yml").read_text(encoding="utf-8")
security = Path(".github/workflows/security.yml").read_text(encoding="utf-8")

required = [
    "push:",
    "tags:",
    "contents: write",
    "id-token: write",
    "attestations: write",
    "scripts/ci/prepare-release-bundle.sh",
    "scripts/ci/finalize-release-bundle.sh",
    "scripts/ci/publish-release.sh",
    "--source-ref",
    "--source-digest",
    "--signer-workflow",
    "gh release verify",
]
for token in required:
    if token not in release:
        raise SystemExit(f"release provenance: missing workflow contract: {token}")
for forbidden in ("pull_request:", "workflow_dispatch:"):
    if forbidden in release:
        raise SystemExit(f"release provenance: signing workflow must not expose {forbidden}")
for permission in ("contents: write", "id-token: write", "attestations: write"):
    if release.count(permission) != 1:
        raise SystemExit(f"release provenance: {permission} must exist in exactly one job")
for workflow in (ci, security):
    if "id-token: write" in workflow or "attestations: write" in workflow:
        raise SystemExit("release provenance: PR/general workflows must not receive signing permissions")

uses = re.findall(r"uses:\s*([^\s#]+)", release)
if not uses or any(not re.fullmatch(r"[^@\s]+@[0-9a-f]{40}", value) for value in uses):
    raise SystemExit("release provenance: every action must use a full commit SHA")
expected = {
    "anchore/sbom-action@e22c389904149dbc22b58101806040fa8d37a610",
    "actions/attest@f7c74d28b9d84cb8768d0b8ca14a4bac6ef463e6",
}
if not expected.issubset(set(uses)):
    raise SystemExit("release provenance: pinned SBOM or attestation action changed")
PY

bash scripts/ci/check-release-provenance_test.sh
echo "release provenance contract passed"
