#!/usr/bin/env bash
set -euo pipefail

python3 - "$@" <<'PY'
from __future__ import annotations

import re
import sys
from pathlib import Path


SECRET_EXPR_RE = re.compile(r"\$\{\{(?P<body>.*?)\}\}", re.DOTALL)
DOT_SECRET_RE = re.compile(r"secrets\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)")
BRACKET_SECRET_RE = re.compile(r"secrets\s*\[\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]\s*\]")
SECRETS_INHERIT_RE = re.compile(r"^\s*secrets\s*:\s*inherit\s*(?:#.*)?$")


def workflow_paths(args: list[str]) -> list[Path]:
    if not args:
        args = [".github/workflows"]

    paths: list[Path] = []
    for arg in args:
        path = Path(arg)
        if path.is_dir():
            paths.extend(sorted(path.glob("*.yml")))
            paths.extend(sorted(path.glob("*.yaml")))
        else:
            paths.append(path)

    return paths


def meaningful(raw: str) -> bool:
    stripped = raw.strip()
    return bool(stripped) and not stripped.startswith("#")


def indent(raw: str) -> int:
    return len(raw) - len(raw.lstrip(" "))


def has_pull_request_trigger(text: str) -> bool:
    in_on = False
    on_indent = 0

    for raw in text.splitlines():
        if not meaningful(raw):
            continue

        current_indent = indent(raw)
        stripped = raw.strip()

        match = re.match(r"^(\s*)on\s*:\s*(.*)$", raw)
        if match:
            in_on = True
            on_indent = len(match.group(1))
            value = match.group(2).strip()
            if re.search(r"(^|[^A-Za-z0-9_])pull_request([^A-Za-z0-9_]|$)", value):
                return True
            continue

        if in_on:
            if current_indent <= on_indent and re.match(r"^\S", raw):
                in_on = False
            elif (
                re.match(r"^\s*pull_request\s*:", raw)
                or re.match(r"^\s*-\s*pull_request\s*$", stripped)
            ):
                return True

    return False


def mask_comment_lines(text: str) -> str:
    masked: list[str] = []
    for raw in text.splitlines(keepends=True):
        if raw.strip().startswith("#"):
            masked.append(re.sub(r"[^\n]", " ", raw))
        else:
            masked.append(raw)
    return "".join(masked)


def line_number_at(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def secret_refs(text: str) -> list[tuple[int, str]]:
    refs: list[tuple[int, str]] = []
    masked = mask_comment_lines(text)
    for expr in SECRET_EXPR_RE.finditer(masked):
        body = expr.group("body")
        body_offset = expr.start("body")
        for pattern in (DOT_SECRET_RE, BRACKET_SECRET_RE):
            for match in pattern.finditer(body):
                refs.append((line_number_at(masked, body_offset + match.start()), match.group(1)))
    return refs


def secrets_inherit_lines(text: str) -> list[int]:
    lines: list[int] = []
    for line_no, raw in enumerate(text.splitlines(), start=1):
        if raw.strip().startswith("#"):
            continue
        if SECRETS_INHERIT_RE.match(raw):
            lines.append(line_no)
    return lines


def reusable_workflow_secret_lines(text: str) -> list[int]:
    secret_lines: list[int] = []
    lines = text.splitlines()
    i = 0

    while i < len(lines):
        raw = lines[i]
        if not meaningful(raw):
            i += 1
            continue

        match = re.match(r"^(\s*)jobs\s*:\s*(?:#.*)?$", raw)
        if not match:
            i += 1
            continue

        jobs_indent = len(match.group(1))
        i += 1
        while i < len(lines):
            job_raw = lines[i]
            if meaningful(job_raw) and indent(job_raw) <= jobs_indent:
                break

            job_match = re.match(r"^(\s*)[A-Za-z0-9_-]+\s*:\s*(?:#.*)?$", job_raw)
            if not job_match or indent(job_raw) <= jobs_indent:
                i += 1
                continue

            job_indent = len(job_match.group(1))
            job_property_indent = job_indent + 2
            has_job_uses = False
            job_secret_line: int | None = None
            i += 1

            while i < len(lines):
                entry = lines[i]
                if meaningful(entry) and indent(entry) <= job_indent:
                    break
                if meaningful(entry) and indent(entry) == job_property_indent:
                    if re.match(r"^\s*uses\s*:", entry):
                        has_job_uses = True
                    if re.match(r"^\s*secrets\s*:", entry):
                        job_secret_line = i + 1
                i += 1

            if has_job_uses and job_secret_line is not None:
                secret_lines.append(job_secret_line)

        continue

    return secret_lines


def permission_blocks(text: str) -> list[tuple[int, int, str, list[tuple[int, str]]]]:
    blocks: list[tuple[int, int, str, list[tuple[int, str]]]] = []
    lines = text.splitlines()
    i = 0

    while i < len(lines):
        raw = lines[i]
        if not meaningful(raw):
            i += 1
            continue

        match = re.match(r"^(\s*)permissions\s*:\s*(.*)$", raw)
        if not match:
            i += 1
            continue

        block_indent = len(match.group(1))
        line_no = i + 1
        inline_value = match.group(2).strip()
        entries: list[tuple[int, str]] = []
        i += 1

        while i < len(lines):
            entry = lines[i]
            if meaningful(entry) and indent(entry) <= block_indent:
                break
            entries.append((i + 1, entry))
            i += 1

        blocks.append((line_no, block_indent, inline_value, entries))

    return blocks


def permissions_block_is_readonly(inline_value: str, entries: list[tuple[int, str]]) -> bool:
    if inline_value:
        return inline_value in {"read-all", "{}"}

    saw_entry = False
    for _, raw in entries:
        if not meaningful(raw):
            continue
        match = re.match(r"^\s*[A-Za-z0-9_-]+\s*:\s*([A-Za-z0-9_-]+)\s*$", raw)
        if not match:
            continue
        saw_entry = True
        if match.group(1) not in {"read", "none"}:
            return False

    return saw_entry


def top_level_permissions_block_exists(text: str) -> bool:
    return any(block_indent == 0 for _, block_indent, _, _ in permission_blocks(text))


def non_readonly_permission_blocks(text: str) -> list[int]:
    failures: list[int] = []
    for line_no, _, inline_value, entries in permission_blocks(text):
        if not permissions_block_is_readonly(inline_value, entries):
            failures.append(line_no)
    return failures


def main() -> int:
    failures: list[str] = []

    for path in workflow_paths(sys.argv[1:]):
        text = path.read_text(encoding="utf-8")
        if not has_pull_request_trigger(text):
            continue

        refs = secret_refs(text)
        disallowed = [(line_no, name) for line_no, name in refs if name != "GITHUB_TOKEN"]
        for line_no, name in disallowed:
            failures.append(
                f"{path}:{line_no}: pull_request workflow must not reference secrets.{name}"
            )

        for line_no in secrets_inherit_lines(text):
            failures.append(
                f"{path}:{line_no}: pull_request workflow must not use secrets: inherit"
            )

        for line_no in reusable_workflow_secret_lines(text):
            failures.append(
                f"{path}:{line_no}: pull_request reusable workflow secrets are not allowed"
            )

        if not top_level_permissions_block_exists(text):
            failures.append(
                f"{path}: pull_request workflow must define top-level read-only permissions"
            )

        for line_no in non_readonly_permission_blocks(text):
            failures.append(
                f"{path}:{line_no}: pull_request workflow must use read-only permissions or none"
            )

    if failures:
        print("FAIL: workflow pull_request secret boundary violation", file=sys.stderr)
        for failure in failures:
            print(f" - {failure}", file=sys.stderr)
        return 1

    print("ok: workflow pull_request secret boundary check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
PY
