#!/usr/bin/env python3
from __future__ import annotations

import os
import subprocess
import tempfile
from pathlib import Path

CHECKER = Path(__file__).with_name("check-workflow-ci-owner.py")


def write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def workflow(events: list[str], markers: tuple[str, ...] = ()) -> str:
    lines = ["name: fixture", "", "on:"]
    for event in events:
        if event == "push":
            lines.extend(["  push:", "    branches: [main]"])
        elif event == "schedule":
            lines.extend(["  schedule:", '    - cron: "11 11 * * 0"'])
        else:
            lines.append(f"  {event}:")
    lines.extend(
        [
            "",
            "permissions:",
            "  contents: read",
            "",
            "jobs:",
            "  verify:",
            "    runs-on: ubuntu-latest",
            "    timeout-minutes: 5",
            "    steps:",
            "      - run: echo ok",
        ]
    )
    lines.extend(f"      # {marker}" for marker in markers)
    return "\n".join(lines) + "\n"


def fixture(root: Path, profile: str, owner: str) -> None:
    write(root / "scripts/ci/workflow-gate-profile", profile + "\n")
    write(root / "scripts/ci/workflow-ci-owner", owner + "\n")
    if owner == "local":
        primary_events = ["workflow_dispatch"]
        security_events = ["workflow_dispatch"]
        markers = ("go test -run '^$'",) if profile == "app" else ()
    else:
        primary_events = ["workflow_dispatch", "pull_request", "push"]
        security_events = ["workflow_dispatch", "push", "schedule"]
        markers = (
            ("public-pr-go-gate.sh", "public-pr-frontend-gate.sh")
            if profile == "app"
            else ("go test -race",)
        )
    write(root / ".github/workflows/ci.yml", workflow(primary_events, markers))
    write(root / ".github/workflows/security.yml", workflow(security_events, ("security",)))


def run(root: Path, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    return subprocess.run(
        ["python3", str(CHECKER), "--root", str(root)],
        text=True,
        capture_output=True,
        check=False,
        env=merged,
    )


def expect_success(label: str, result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode != 0:
        raise AssertionError(f"{label}: expected success\nstdout={result.stdout}\nstderr={result.stderr}")


def expect_failure(label: str, result: subprocess.CompletedProcess[str], marker: str) -> None:
    if result.returncode == 0 or marker not in result.stderr:
        raise AssertionError(
            f"{label}: expected failure containing {marker!r}\nstdout={result.stdout}\nstderr={result.stderr}"
        )


def main() -> int:
    with tempfile.TemporaryDirectory() as raw:
        base = Path(raw)

        local_app = base / "local-app"
        fixture(local_app, "app", "local")
        expect_success("valid local app", run(local_app))
        write(
            local_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request"], ("go test -run '^$'",)),
        )
        expect_failure("local PR trigger", run(local_app), "local owner events")

        remote_app = base / "remote-app"
        fixture(remote_app, "app", "remote")
        expect_success("valid remote app", run(remote_app, {"WORKFLOW_CI_OWNER": "local"}))
        write(
            remote_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"], ("public-pr-go-gate.sh",)),
        )
        expect_failure("missing remote app marker", run(remote_app), "public-pr-frontend-gate.sh")

        remote_lib = base / "remote-lib"
        fixture(remote_lib, "lib", "remote")
        expect_success("valid remote library", run(remote_lib))
        write(
            remote_lib / ".github/workflows/security.yml",
            workflow(["workflow_dispatch", "push", "schedule", "pull_request"], ("security",)),
        )
        expect_failure("security PR trigger", run(remote_lib), "security workflow must not run on PR events")

        missing_owner = base / "missing-owner"
        fixture(missing_owner, "app", "local")
        (missing_owner / "scripts/ci/workflow-ci-owner").unlink()
        expect_failure("missing owner", run(missing_owner), "explicit declaration is required")

        invalid_owner = base / "invalid-owner"
        fixture(invalid_owner, "app", "local")
        write(invalid_owner / "scripts/ci/workflow-ci-owner", "hybrid\n")
        expect_failure("invalid owner", run(invalid_owner), "expected exact local or remote declaration")

    print("ok: workflow CI ownership contract fixtures passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
