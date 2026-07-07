#!/usr/bin/env python3
import argparse
import sys
from pathlib import Path


DENY_TOKENS = (
    "LegacyDedupeKey",
    "legacy_dedupe_key",
    "BuildLegacyDedupeKey",
    "legacy-live",
    "legacy-schedule",
    "ValidateLegacyRoute",
    "LegacyIrisBotWebhookWorkerProfile",
    "normalizeLegacySession",
    "build_legacy_semantic_message_id",
    "legacy_state_repointed",
)

SCAN_SUFFIXES = {
    ".go",
    ".rs",
    ".sql",
    ".kt",
    ".kts",
    ".java",
    ".py",
    ".sh",
    ".toml",
    ".yaml",
    ".yml",
}

SKIP_DIRS = {
    ".git",
    ".gradle",
    ".idea",
    ".venv",
    "build",
    "dist",
    "docs",
    "node_modules",
    "target",
    "testdata",
    "vendor",
}

SKIP_FILENAMES = {
    "check-legacy-fadeout.py",
    "legacy_fadeout_check.py",
}


def should_scan(path: Path, root: Path) -> bool:
    rel_parts = path.relative_to(root).parts
    if any(part in SKIP_DIRS for part in rel_parts[:-1]):
        return False
    if any(part in {"test", "tests", "__tests__"} for part in rel_parts[:-1]):
        return False
    if path.name in SKIP_FILENAMES:
        return False
    if path.name.endswith(("_test.go", "_test.rs", "_test.sh")):
        return False
    return path.suffix in SCAN_SUFFIXES


def scan_file(path: Path, root: Path) -> list[tuple[str, int, str]]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except UnicodeDecodeError:
        return []
    findings = []
    rel = path.relative_to(root).as_posix()
    for lineno, line in enumerate(lines, 1):
        for token in DENY_TOKENS:
            if token in line:
                findings.append((rel, lineno, token))
    return findings


def main() -> int:
    parser = argparse.ArgumentParser(description="Check production sources for retired legacy fadeout tokens.")
    parser.add_argument("--repo-root", default=".", help="repository root to scan")
    args = parser.parse_args()

    root = Path(args.repo_root).resolve()
    findings: list[tuple[str, int, str]] = []
    for path in sorted(root.rglob("*")):
        if path.is_file() and should_scan(path, root):
            findings.extend(scan_file(path, root))

    if findings:
        print("[FAIL] legacy fadeout deny tokens found in production sources", file=sys.stderr)
        for rel, lineno, token in findings:
            print(f"{rel}:{lineno}: {token}", file=sys.stderr)
        return 1

    print("[PASS] legacy fadeout production token gate")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
