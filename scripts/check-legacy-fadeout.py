#!/usr/bin/env python3
import argparse
import re
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
    "ActiveThreadID",
    "SessionModel",
    "CommandSessionModel",
    "drawStateScopes",
    "appendUniqueDrawStateKeys",
    "appendLegacySession",
    "PaidWorkKey",
    "legacySearches",
    "SessionTemperature",
    "notifiedDataSourceLegacyString",
    "UpdateLegacyNotifiedData",
    "LegacyDeliveryPath",
    "LegacyStatus",
    "LegacyPathActive",
    "legacy_delivery_path",
    "legacy_status",
    "legacy_path_active",
    "legacy_alarm_queue",
    "CalendarImageRendererContext",
    "RenderCalendarImage(",
    "settings.Load(",
)

RETIRED_SQL_STRUCTURES = (
    (
        "legacy_session CTE",
        re.compile(
            r"(?:\bWITH(?:[ \t\r\n]+RECURSIVE)?|,)[ \t\r\n]+legacy_session[ \t\r\n]+AS[ \t\r\n]*\(",
            re.IGNORECASE | re.MULTILINE,
        ),
    ),
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
    ".tools",
    ".venv",
    "build",
    "dist",
    "docs",
    "node_modules",
    "target",
    "vendor",
}

# Append-only migrations are immutable production history. Unlike the old
# blanket test/tests/testdata exclusion, these are the only source-like paths
# allowed to retain retired spellings.
IMMUTABLE_HISTORY_PREFIXES = (
    "scripts/migrations/",
    "hololive/hololive-api/scripts/migrations/",
)

SKIP_FILENAMES = {
    "check-legacy-fadeout.py",
    "legacy_fadeout_check.py",
    "legacy_fadeout_check_test.py",
}


def should_scan(path: Path, root: Path) -> bool:
    rel_parts = path.relative_to(root).parts
    rel = path.relative_to(root).as_posix()
    if any(part in SKIP_DIRS for part in rel_parts[:-1]):
        return False
    if any(rel.startswith(prefix) for prefix in IMMUTABLE_HISTORY_PREFIXES):
        return False
    if path.name in SKIP_FILENAMES:
        return False
    if path.name.endswith(("_test.go", "_test.rs", "_test.sh")):
        return False
    return path.suffix in SCAN_SUFFIXES


def scan_file(path: Path, root: Path) -> list[tuple[str, int, str]]:
    try:
        content = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        return []
    lines = content.splitlines()
    findings = []
    rel = path.relative_to(root).as_posix()
    for lineno, line in enumerate(lines, 1):
        for token in DENY_TOKENS:
            if token in line:
                findings.append((rel, lineno, token))
    if path.suffix == ".sql":
        sql_without_literals = re.sub(r"'(?:''|[^'])*'", "''", content)
        sql_without_comments = re.sub(r"--[^\n]*", "", sql_without_literals)
        for label, pattern in RETIRED_SQL_STRUCTURES:
            for match in pattern.finditer(sql_without_comments):
                lineno = sql_without_comments.count("\n", 0, match.start()) + 1
                findings.append((rel, lineno, label))
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
