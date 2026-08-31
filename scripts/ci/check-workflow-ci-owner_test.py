#!/usr/bin/env python3
from __future__ import annotations

import os
import subprocess
import sys
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
    lines.extend(f"      - run: {marker}" for marker in markers)
    return "\n".join(lines) + "\n"


def fixture(root: Path, profile: str, owner: str) -> None:
    write(root / "scripts/ci/workflow-gate-profile", profile + "\n")
    write(root / "scripts/ci/workflow-ci-owner", owner + "\n")
    if owner == "local":
        primary_events = ["workflow_dispatch"]
        security_events = ["workflow_dispatch"]
        markers = ("go test -run '^$'",) if profile == "app" else ()
        if profile == "app":
            write(root / "go.mod", "module example.invalid/workflow-ci-owner-local-app-fixture\n")
    else:
        primary_events = ["workflow_dispatch", "pull_request", "push"]
        security_events = ["workflow_dispatch", "push", "schedule"]
        markers = (
            (
                "bash scripts/ci/public-pr-go-gate.sh fixture test",
                "bash scripts/ci/public-pr-frontend-gate.sh",
            )
            if profile == "app"
            else ("go test -race",)
        )
        if profile == "lib":
            write(root / "go.mod", "module example.invalid/workflow-ci-owner-fixture\n")
        else:
            write(root / "go.mod", "module example.invalid/workflow-ci-owner-remote-app-fixture\n")
    write(root / ".github/workflows/ci.yml", workflow(primary_events, markers))
    write(root / ".github/workflows/security.yml", workflow(security_events, ("security",)))


def run(root: Path, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    merged["WORKFLOW_CI_OWNER_CONTRACT_FIXTURE"] = "1"
    if env:
        merged.update(env)
    return subprocess.run(
        [sys.executable, str(CHECKER), "--root", str(root)],
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

        fixture(local_app, "app", "local")
        write(
            local_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch"]) + "      # go test -run '^$'\n",
        )
        expect_failure("comment-only local compile smoke", run(local_app), "must execute go test")

        local_durable_app = base / "local-durable-app"
        write(local_durable_app / "scripts/ci/workflow-gate-profile", "app\n")
        write(local_durable_app / "scripts/ci/workflow-ci-owner", "local\n")
        write(
            local_durable_app / "go.mod",
            "module example.invalid/workflow-ci-owner-local-durable-fast-app-fixture\n",
        )
        write(
            local_durable_app / ".github/workflows/ci.yml",
            workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("go test -run '^$'",),
            ),
        )
        write(
            local_durable_app / ".github/workflows/security.yml",
            workflow(["workflow_dispatch"], ("security",)),
        )
        expect_success("valid local durable fast app", run(local_durable_app))
        write(
            local_durable_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request"], ("go test -run '^$'",)),
        )
        expect_failure(
            "local durable fast app missing push",
            run(local_durable_app),
            "local owner events",
        )

        remote_app = base / "remote-app"
        fixture(remote_app, "app", "remote")
        expect_success("valid remote app", run(remote_app, {"WORKFLOW_CI_OWNER": "local"}))
        write(
            remote_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"], ("public-pr-go-gate.sh",)),
        )
        expect_failure("missing remote app marker", run(remote_app), "public-pr-frontend-gate.sh")

        fixture(remote_app, "app", "remote")
        write(
            remote_app / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"])
            + "      # bash scripts/ci/public-pr-go-gate.sh\n"
            + "      # bash scripts/ci/public-pr-frontend-gate.sh\n",
        )
        expect_failure(
            "comment-only remote app markers",
            run(remote_app),
            "must invoke scripts/ci/public-pr-go-gate.sh directly",
        )

        remote_app_snapshot_bypasses = {
            "remote app early exit": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                (
                    "bash scripts/ci/public-pr-go-gate.sh fixture test",
                    "bash scripts/ci/public-pr-frontend-gate.sh",
                ),
            ).replace(
                "      - run: bash scripts/ci/public-pr-go-gate.sh fixture test\n",
                "      - run: |\n"
                "          exit 0\n"
                "          bash scripts/ci/public-pr-go-gate.sh fixture test\n",
            ),
            "remote app custom shell": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                (
                    "bash scripts/ci/public-pr-go-gate.sh fixture test",
                    "bash scripts/ci/public-pr-frontend-gate.sh",
                ),
            ).replace(
                "      - run: bash scripts/ci/public-pr-go-gate.sh fixture test\n",
                "      - run: bash scripts/ci/public-pr-go-gate.sh fixture test\n"
                "        shell: bash -c 'exit 0' --\n",
            ),
            "remote app bash env": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                (
                    "bash scripts/ci/public-pr-go-gate.sh fixture test",
                    "bash scripts/ci/public-pr-frontend-gate.sh",
                ),
            ).replace(
                "permissions:\n",
                "env:\n  BASH_ENV: ./pr-controlled.sh\n\npermissions:\n",
            ),
            "remote app folded scalar": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("bash scripts/ci/public-pr-frontend-gate.sh",),
            )
            + "      - run: >\n"
            + "          exit 0\n"
            + "          bash scripts/ci/public-pr-go-gate.sh fixture test\n",
        }
        for label, mutated_workflow in remote_app_snapshot_bypasses.items():
            fixture(remote_app, "app", "remote")
            write(remote_app / ".github/workflows/ci.yml", mutated_workflow)
            expect_failure(label, run(remote_app), "exact canonical workflow snapshot")

        remote_lib = base / "remote-lib"
        fixture(remote_lib, "lib", "remote")
        expect_success("valid remote library", run(remote_lib))
        write(
            remote_lib / ".github/workflows/security.yml",
            workflow(["workflow_dispatch", "push", "schedule", "pull_request"], ("security",)),
        )
        expect_failure("security PR trigger", run(remote_lib), "security workflow must not run on PR events")

        fixture(remote_lib, "lib", "remote")
        write(
            remote_lib / ".github/workflows/security.yml",
            workflow(["workflow_dispatch", "push", "schedule", "issue_comment"], ("security",)),
        )
        expect_failure("unexpected security trigger", run(remote_lib), "remote owner events")

        fixture(remote_lib, "lib", "remote")
        write(
            remote_lib / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"], ("go test -race",)).replace(
                "      - run: echo ok", "      - run: make test-race"
            ),
        )
        expect_failure("make target bypass", run(remote_lib), "PR-controlled Make targets")

        fixture(remote_lib, "lib", "remote")
        write(
            remote_lib / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"]) + "      # go test -race\n",
        )
        expect_failure("comment race marker bypass", run(remote_lib), "execute go test -race directly")

        fixture(remote_lib, "lib", "remote")
        write(
            remote_lib / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"])
            + "      # go test -race\n"
            + "      - run: |\n"
            + "          make security-check\n",
        )
        expect_failure("multiline make target bypass", run(remote_lib), "PR-controlled Make targets")

        make_control_bypasses = {
            "if make target bypass": "if make security-check; then echo ok; fi",
            "negated make target bypass": "! make security-check",
            "subshell make target bypass": "( make security-check )",
            "absolute make target bypass": "/usr/bin/make security-check",
            "relative make target bypass": "./make security-check",
            "parameter trim before make bypass": "FOO=${VALUE#x}; make security-check",
            "concatenated quote make bypass": "m''ake security-check",
            "escaped make bypass": r"m\ake security-check",
            "variable command make bypass": "M=make; $M security-check",
        }
        for label, make_command in make_control_bypasses.items():
            fixture(remote_lib, "lib", "remote")
            write(
                remote_lib / ".github/workflows/ci.yml",
                workflow(["workflow_dispatch", "pull_request", "push"])
                + "      - run: |\n"
                + "          go test -race ./...\n"
                + f"          {make_command}\n",
            )
            expect_failure(label, run(remote_lib), "PR-controlled Make targets")

        fixture(remote_lib, "lib", "remote")
        write(
            remote_lib / ".github/workflows/ci.yml",
            workflow(["workflow_dispatch", "pull_request", "push"])
            + "      - run: |2-\n"
            + "          go test -race ./...\n"
            + "          make security-check\n",
        )
        expect_failure(
            "indented block scalar make bypass",
            run(remote_lib),
            "PR-controlled Make targets",
        )

        canonical_snapshot_bypasses = {
            "required gate deletion": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("go test -race",),
            ).replace("      - run: echo ok\n", ""),
            "early exit before race": workflow(
                ["workflow_dispatch", "pull_request", "push"],
            )
            + "      - run: |\n"
            + "          exit 0\n"
            + "          go test -race\n",
            "folded scalar": workflow(
                ["workflow_dispatch", "pull_request", "push"],
            )
            + "      - run: >\n"
            + "          echo ok\n"
            + "          go test -race\n",
            "flow mapping run": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("go test -race",),
            )
            + "      - { run: make security-gate }\n",
            "custom step shell": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("go test -race",),
            ).replace(
                "      - run: go test -race\n",
                "      - run: go test -race\n"
                "        shell: bash -c 'make security-check; bash {0}' --\n",
            ),
            "bash env injection": workflow(
                ["workflow_dispatch", "pull_request", "push"],
                ("go test -race",),
            ).replace(
                "permissions:\n",
                "env:\n"
                "  BASH_ENV: ./pr-controlled.sh\n"
                "\n"
                "permissions:\n",
            ),
        }
        for label, mutated_workflow in canonical_snapshot_bypasses.items():
            fixture(remote_lib, "lib", "remote")
            write(remote_lib / ".github/workflows/ci.yml", mutated_workflow)
            expect_failure(label, run(remote_lib), "exact canonical workflow snapshot")

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
