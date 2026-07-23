#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

PROFILE_PATH = Path("scripts/ci/workflow-gate-profile")
OWNER_PATH = Path("scripts/ci/workflow-ci-owner")
PRIMARY_PATH = Path(".github/workflows/ci.yml")
SECURITY_CANDIDATES = (
    Path(".github/workflows/security.yml"),
    Path(".github/workflows/security.yaml"),
    Path(".github/workflows/security-full.yml"),
    Path(".github/workflows/security-full.yaml"),
)
KNOWN_EVENTS = {
    "pull_request",
    "pull_request_target",
    "push",
    "schedule",
    "workflow_call",
    "workflow_dispatch",
    "workflow_run",
}


class ContractError(ValueError):
    pass


def meaningful(raw: str) -> bool:
    value = raw.strip()
    return bool(value) and not value.startswith("#")


def indent(raw: str) -> int:
    return len(raw) - len(raw.lstrip(" "))


def strip_yaml_comment(raw: str) -> str:
    out: list[str] = []
    quote: str | None = None
    escaped = False
    for char in raw:
        if escaped:
            out.append(char)
            escaped = False
            continue
        if quote == '"' and char == "\\":
            out.append(char)
            escaped = True
            continue
        if char in {"'", '"'}:
            if quote is None:
                quote = char
            elif quote == char:
                quote = None
            out.append(char)
            continue
        if char == "#" and quote is None:
            break
        out.append(char)
    return "".join(out).rstrip()


def parse_key_value(raw: str) -> tuple[str, str] | None:
    if not meaningful(raw):
        return None
    source = strip_yaml_comment(raw).strip()
    if source.startswith("- "):
        source = source[2:].strip()
    match = re.match(r"^(['\"]?)([A-Za-z0-9_-]+)\1\s*:\s*(.*)$", source)
    if not match:
        return None
    return match.group(2), match.group(3).strip()


def unquote(value: str) -> str:
    value = strip_yaml_comment(value).strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def inline_events(value: str) -> set[str]:
    value = unquote(value)
    if not value:
        return set()
    if value.startswith("[") and value.endswith("]"):
        return {unquote(item.strip()) for item in value[1:-1].split(",") if item.strip()}
    if re.fullmatch(r"[A-Za-z0-9_-]+", value):
        return {value}
    raise ContractError(f"unsupported top-level on value: {value!r}")


def workflow_events(text: str) -> set[str]:
    lines = text.splitlines()
    for index, raw in enumerate(lines):
        if not meaningful(raw) or indent(raw) != 0:
            continue
        parsed = parse_key_value(raw)
        if parsed is None or parsed[0].lower() != "on":
            continue
        events = inline_events(parsed[1])
        if parsed[1]:
            return events
        for child in lines[index + 1 :]:
            if not meaningful(child):
                continue
            if indent(child) == 0:
                break
            child_source = strip_yaml_comment(child).strip()
            if child_source.startswith("- "):
                event = unquote(child_source[2:].strip())
            else:
                child_parsed = parse_key_value(child)
                if child_parsed is None:
                    continue
                event = child_parsed[0]
            if event in KNOWN_EVENTS:
                events.add(event)
        return events
    raise ContractError("workflow is missing a top-level on declaration")


def read_declaration(root: Path, relative: Path, allowed: set[str]) -> str:
    path = root / relative
    try:
        declaration = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ContractError(f"{relative}: explicit declaration is required ({exc})") from exc
    expected = {f"{value}\n" for value in allowed}
    if declaration not in expected:
        raise ContractError(f"{relative}: expected exact {' or '.join(sorted(allowed))} declaration")
    return declaration.strip()


def read_workflow(root: Path, relative: Path) -> str:
    path = root / relative
    try:
        return path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ContractError(f"cannot read {relative}: {exc}") from exc


def find_security_workflow(root: Path) -> tuple[Path, str]:
    found = [(path, root / path) for path in SECURITY_CANDIDATES if (root / path).is_file()]
    if len(found) != 1:
        raise ContractError(f"expected exactly one security workflow, found {len(found)}")
    relative, absolute = found[0]
    return relative, absolute.read_text(encoding="utf-8")


def require_markers(label: str, text: str, markers: tuple[str, ...]) -> list[str]:
    return [f"{label}: required marker missing: {marker}" for marker in markers if marker not in text]


def validate(root: Path) -> tuple[str, str, list[str]]:
    profile = read_declaration(root, PROFILE_PATH, {"app", "lib"})
    owner = read_declaration(root, OWNER_PATH, {"local", "remote"})
    primary = read_workflow(root, PRIMARY_PATH)
    security_path, security = find_security_workflow(root)

    failures: list[str] = []
    try:
        primary_events = workflow_events(primary)
    except ContractError as exc:
        failures.append(f"{PRIMARY_PATH}: {exc}")
        primary_events = set()
    try:
        security_events = workflow_events(security)
    except ContractError as exc:
        failures.append(f"{security_path}: {exc}")
        security_events = set()

    if "pull_request_target" in primary_events | security_events:
        failures.append("pull_request_target is not allowed in app/library CI ownership workflows")
    if {"pull_request", "pull_request_target"} & security_events:
        failures.append(f"{security_path}: security workflow must not run on PR events")

    if owner == "local":
        expected_primary = {"workflow_dispatch"}
        expected_security = {"workflow_dispatch"}
        if primary_events != expected_primary:
            failures.append(
                f"{PRIMARY_PATH}: local owner events {sorted(primary_events)} != {sorted(expected_primary)}"
            )
        if security_events != expected_security:
            failures.append(
                f"{security_path}: local owner events {sorted(security_events)} != {sorted(expected_security)}"
            )
        if profile == "app":
            failures.extend(require_markers(str(PRIMARY_PATH), primary, ("go test -run '^$'",)))
    else:
        expected_primary = {"pull_request", "push", "workflow_dispatch"}
        expected_security = {"push", "schedule", "workflow_dispatch"}
        if primary_events != expected_primary:
            failures.append(
                f"{PRIMARY_PATH}: remote owner events {sorted(primary_events)} != {sorted(expected_primary)}"
            )
        if security_events != expected_security:
            failures.append(
                f"{security_path}: remote owner events {sorted(security_events)} != {sorted(expected_security)}"
            )
        if profile == "app":
            failures.extend(
                require_markers(
                    str(PRIMARY_PATH),
                    primary,
                    ("public-pr-go-gate.sh", "public-pr-frontend-gate.sh"),
                )
            )
        elif "make test-race" not in primary and "go test -race" not in primary:
            failures.append(f"{PRIMARY_PATH}: remote library PR gate must contain a race-test marker")

    return profile, owner, failures


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    root = parse_args().root.resolve()
    try:
        profile, owner, failures = validate(root)
    except ContractError as exc:
        print(f"FAIL: workflow CI ownership contract: {exc}", file=sys.stderr)
        return 1
    if failures:
        print(
            f"FAIL: workflow CI ownership contract (profile={profile}, owner={owner}, failures={len(failures)})",
            file=sys.stderr,
        )
        for failure in failures:
            print(f" - {failure}", file=sys.stderr)
        return 1
    print(f"ok: workflow CI ownership contract passed (profile={profile}, owner={owner})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
