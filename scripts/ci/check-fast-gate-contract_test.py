#!/usr/bin/env python3
from __future__ import annotations

import gzip
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW_CHECKER = ROOT / "scripts/ci/check-workflow-ci-owner.py"
GENERATOR = ROOT / "pkg/internal/guardtext/genconfusables.go"
CONFUSABLES = ROOT / "pkg/internal/guardtext/testdata/confusables-17.0.0.txt.gz"
UNICODE_BASELINE = ROOT / "pkg/internal/guardtext/testdata/UnicodeData-15.0.0.txt.gz"
UNICODE_CURRENT = ROOT / "pkg/internal/guardtext/testdata/UnicodeData-17.0.0.txt.gz"
GENERATED = ROOT / "pkg/internal/guardtext/confusables_table_generated.go"


def run(command: list[str], cwd: Path) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["GOWORK"] = "off"
    return subprocess.run(
        command,
        cwd=cwd,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )


def expect_success(label: str, result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode != 0:
        raise AssertionError(
            f"{label}: expected success\nstdout={result.stdout}\nstderr={result.stderr}"
        )


def expect_failure(label: str, result: subprocess.CompletedProcess[str], marker: str) -> None:
    if result.returncode == 0 or marker not in result.stderr:
        raise AssertionError(
            f"{label}: expected failure containing {marker!r}\n"
            f"stdout={result.stdout}\nstderr={result.stderr}"
        )


def generator_command(
    confusables: Path,
    baseline: Path,
    current: Path,
    output: Path,
) -> list[str]:
    return [
        "go",
        "run",
        str(GENERATOR),
        "-confusables-source",
        str(confusables),
        "-unicode-data-baseline-source",
        str(baseline),
        "-unicode-data-source",
        str(current),
        "-output",
        str(output),
        "-check",
    ]


def verify_tidy_contract(root: Path) -> None:
    fixture = root / "tidy"
    fixture.mkdir()
    (fixture / "go.mod").write_text(
        "module example.invalid/tidyfixture\n\ngo 1.27.0\n",
        encoding="utf-8",
    )
    (fixture / "fixture.go").write_text("package fixture\n", encoding="utf-8")
    expect_success("clean module", run(["go", "mod", "tidy", "-diff"], fixture))

    (fixture / "go.mod").write_text(
        "module example.invalid/tidyfixture\n\n"
        "go 1.27.0\n\n"
        "require example.invalid/stale v0.0.0\n",
        encoding="utf-8",
    )
    stale = run(["go", "mod", "tidy", "-diff"], fixture)
    if stale.returncode == 0 or "diff current/go.mod tidy/go.mod" not in stale.stdout:
        raise AssertionError(
            "stale module: expected nonzero tidy diff\n"
            f"stdout={stale.stdout}\nstderr={stale.stderr}"
        )


def verify_generator_contract(root: Path) -> None:
    output = root / GENERATED.name
    shutil.copy2(GENERATED, output)
    expect_success(
        "canonical generated table",
        run(generator_command(CONFUSABLES, UNICODE_BASELINE, UNICODE_CURRENT, output), ROOT),
    )

    output.write_bytes(output.read_bytes() + b"\n")
    expect_failure(
        "generated table mutation",
        run(generator_command(CONFUSABLES, UNICODE_BASELINE, UNICODE_CURRENT, output), ROOT),
        "generated confusables table is stale",
    )

    for label, source in (
        ("confusables", CONFUSABLES),
        ("baseline UnicodeData", UNICODE_BASELINE),
        ("current UnicodeData", UNICODE_CURRENT),
    ):
        mutated = root / source.name
        with gzip.open(source, "rb") as reader:
            content = reader.read()
        mutated.write_bytes(gzip.compress(content + b"\n", mtime=0))

        paths = {
            CONFUSABLES: CONFUSABLES,
            UNICODE_BASELINE: UNICODE_BASELINE,
            UNICODE_CURRENT: UNICODE_CURRENT,
        }
        paths[source] = mutated
        expect_failure(
            f"{label} checksum mutation",
            run(
                generator_command(
                    paths[CONFUSABLES],
                    paths[UNICODE_BASELINE],
                    paths[UNICODE_CURRENT],
                    GENERATED,
                ),
                ROOT,
            ),
            "checksum =",
        )


def verify_stage_ownership(root: Path) -> None:
    fixture = root / "workflow"
    for relative in (
        Path(".github/workflows/ci.yml"),
        Path(".github/workflows/security.yml"),
        Path(".github/actions/python-runtime/action.yml"),
        Path("scripts/ci/workflow-gate-profile"),
        Path("scripts/ci/workflow-ci-owner"),
        Path("go.mod"),
    ):
        destination = fixture / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / relative, destination)

    interpreter = os.environ.get("CI_PYTHON_BIN", sys.executable)
    command = [interpreter, str(WORKFLOW_CHECKER), "--root", str(fixture)]
    expect_success("canonical workflow", run(command, ROOT))

    workflow_path = fixture / ".github/workflows/ci.yml"
    canonical = workflow_path.read_text(encoding="utf-8")
    for label, marker in (
        ("tidy stage deletion", "GOWORK=off go mod tidy -diff"),
        (
            "generator stage deletion",
            "GOWORK=off go run ./pkg/internal/guardtext/genconfusables.go",
        ),
    ):
        workflow_path.write_text(
            canonical.replace(marker, "echo deleted-stage", 1),
            encoding="utf-8",
        )
        expect_failure(
            label,
            run(command, ROOT),
            "exact canonical workflow snapshot",
        )


def main() -> int:
    with tempfile.TemporaryDirectory() as raw:
        root = Path(raw)
        verify_tidy_contract(root)
        verify_generator_contract(root)
        verify_stage_ownership(root)

    print("ok: shared-go fast gate mutation fixtures passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
