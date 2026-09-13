#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
. "${SCRIPT_DIR}/python-runtime.sh"
repo_python_init

"${CI_PYTHON_BIN}" - <<'PY'
from pathlib import Path
import re

release = Path(".github/workflows/release.yml").read_text(encoding="utf-8")
ci = Path(".github/workflows/ci.yml").read_text(encoding="utf-8")
security = Path(".github/workflows/security.yml").read_text(encoding="utf-8")
python_runtime_action = "./.github/actions/python-runtime"
action = Path(".github/actions/python-runtime/action.yml").read_text(encoding="utf-8")

# Python 부트스트랩은 저장소 composite action 이 소유한다. 사본 parity 와 핀 값은 iris-stack 이 본다.
action_required = [
    "using: composite",
    "actions/setup-python@5fda3b95a4ea91299a34e894583c3862153e4b97",
    "python-version-file: ${{ inputs.working-directory }}/.python-version",
    "python -m pip install --disable-pip-version-check --no-cache-dir --no-deps uv==0.12.13",
    "scripts/ci/python-runner.sh --print-interpreter",
    "CI_PYTHON_BIN",
]
for token in action_required:
    if token not in action:
        raise SystemExit(f"release provenance: missing python-runtime action contract: {token}")

required = [
    f"uses: {python_runtime_action}",
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

def sha_pinned(values: list[str]) -> bool:
    return bool(values) and all(re.fullmatch(r"[^@\s]+@[0-9a-f]{40}", value) for value in values)


uses = re.findall(r"uses:\s*([^\s#]+)", release)
local_uses = [value for value in uses if value.startswith("./")]
external_uses = [value for value in uses if not value.startswith("./")]
if not local_uses or any(value != python_runtime_action for value in local_uses):
    raise SystemExit(f"release provenance: the only local action must be {python_runtime_action}")
if not sha_pinned(external_uses):
    raise SystemExit("release provenance: every action must use a full commit SHA")
if not sha_pinned(re.findall(r"uses:\s*([^\s#]+)", action)):
    raise SystemExit("release provenance: python-runtime action must pin every action to a full commit SHA")
expected = {
    "anchore/sbom-action@3ad7283483fc7af8ff2b4ea19663c2d5ca935e26",
    "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6",
}
if not expected.issubset(set(external_uses)):
    raise SystemExit("release provenance: pinned SBOM or attestation action changed")
PY

bash scripts/ci/check-release-provenance_test.sh
echo "release provenance contract passed"
