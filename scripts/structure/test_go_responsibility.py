#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ANALYZER = Path(__file__).with_name("go_responsibility.py")
BUDGETS = {
    "production_file_lines": {"advisory": 4, "hard": 8},
    "test_file_lines": {"advisory": 4, "hard": 8},
    "funcs_per_file": {"advisory": 2, "hard": 4},
    "methods_per_type": {"advisory": 2, "hard": 4},
    "struct_fields": {"advisory": 2, "hard": 4},
    "aggregate_fields": {"advisory": 2, "hard": 4},
    "function_lines": {"advisory": 3, "hard": 6},
}


class AnalyzerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.policy = self.root / "policy.json"
        self.write_policy()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def write_policy(self, *, baselines: dict[str, int] | None = None) -> None:
        self.policy.write_text(json.dumps({
            "schema_version": 1,
            "budgets": BUDGETS,
            "struct_field_allowlist": [],
            "legacy_hard_baselines": baselines or {},
        }), encoding="utf-8")

    def run_analyzer(
        self,
        mode: str,
        changed: list[str] | None = None,
        env: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        command = [sys.executable, str(ANALYZER), "--root", str(self.root), "--policy", str(self.policy),
                   "--mode", mode, "--format", "json"]
        if changed is not None:
            path = self.root / "changed"
            path.write_bytes(b"\0".join(item.encode() for item in changed) + b"\0")
            command += ["--changed-paths-file", str(path)]
        return subprocess.run(
            command,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            env=env,
        )

    def test_soft_and_hard_ceiling(self) -> None:
        (self.root / "soft.go").write_text("package p\n" + "\n" * 4, encoding="utf-8")
        advisory = self.run_analyzer("advisory")
        self.assertEqual(advisory.returncode, 0)
        self.assertEqual(json.loads(advisory.stdout)["findings"][0]["level"], "advisory")
        (self.root / "hard.go").write_text("package p\n" + "\n" * 8, encoding="utf-8")
        self.assertEqual(self.run_analyzer("hard").returncode, 1)

    def test_receiver_aggregate_and_changed_filter(self) -> None:
        for index in range(3):
            (self.root / f"m{index}.go").write_text(
                f"package p\nfunc (s *Service) M{index}() {{}}\n", encoding="utf-8")
        report = json.loads(self.run_analyzer("advisory", ["m1.go"]).stdout)
        aggregate = [item for item in report["findings"] if item["rule"] == "methods_per_type"]
        self.assertEqual(len(aggregate), 1)
        self.assertEqual(aggregate[0]["evidence_paths"], ["m0.go", "m1.go", "m2.go"])

    def test_duplicate_policy_and_stale_baseline_are_errors(self) -> None:
        self.policy.write_text('{"schema_version":1,"schema_version":1}', encoding="utf-8")
        self.assertEqual(self.run_analyzer("hard").returncode, 2)
        self.write_policy(baselines={"file_lines:missing.go": 9})
        self.assertEqual(self.run_analyzer("hard").returncode, 2)

    def test_legacy_baseline_ratchets(self) -> None:
        target = self.root / "legacy.go"
        target.write_text("package p\n" + "\n" * 8, encoding="utf-8")
        self.write_policy(baselines={"production_file_lines:legacy.go": 9})
        self.assertEqual(self.run_analyzer("hard").returncode, 0)
        target.write_text("package p\n" + "\n" * 9, encoding="utf-8")
        report = self.run_analyzer("hard")
        self.assertEqual(report.returncode, 1)
        self.assertIn("hard_ratchet", report.stdout)

    def test_environment_cannot_override_policy(self) -> None:
        (self.root / "soft.go").write_text("package p\n" + "\n" * 4, encoding="utf-8")
        env = os.environ.copy()
        env.update({
            "GO_RESPONSIBILITY_FILE_LIMIT": "1",
            "GO_RESPONSIBILITY_TEST_FILE_LIMIT": "1",
            "GO_RESPONSIBILITY_FUNCS_PER_FILE_LIMIT": "1",
            "GO_RESPONSIBILITY_METHODS_PER_TYPE_LIMIT": "1",
            "GO_RESPONSIBILITY_STRUCT_FIELDS_LIMIT": "1",
            "GO_RESPONSIBILITY_FUNC_LINES_LIMIT": "1",
            "GO_RESPONSIBILITY_AGGREGATE_FIELDS_LIMIT": "1",
        })
        report = self.run_analyzer("hard", env=env)
        self.assertEqual(report.returncode, 0)
        self.assertEqual(json.loads(report.stdout)["findings"][0]["level"], "advisory")


if __name__ == "__main__":
    unittest.main()
