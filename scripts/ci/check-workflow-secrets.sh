#!/usr/bin/env bash
# 스택 공용 단일 정본 — 4개 레포(hololive-bot, chat-bot-go-kakao, iris-client-go,
# shared-go)에 바이트 동일 사본으로 배포되고, iris-stack 메타레포의
# tools/check-ci-consistency.sh 가 사본 동일성을 강제한다. 한 곳만 고치지 말 것.
#
# 프로필: scripts/ci/pre-push-gate.sh 가 있으면 app, 없으면 lib.
# PR heavy 검사(go test 전체/race 포함)는 app 전용 — lib 레포는 PR fast gate 가
# 전체 go test 를 정당하게 실행하므로 lib 프로필에서는 적용하지 않는다.
set -euo pipefail

python3 - "$@" <<'PY'
from __future__ import annotations

import os
import re
import sys
from pathlib import Path

SECRET_EXPR_RE = re.compile(r"\$\{\{(?P<body>.*?)\}\}", re.DOTALL)
SECRETS_INHERIT_RE = re.compile(r"^\s*secrets\s*:\s*inherit\s*(?:#.*)?$")
SECURITY_WORKFLOWS = {"security.yml", "security.yaml", "security-full.yml", "security-full.yaml"}

PR_HEAVY_LINE_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("local full CI gate", re.compile(r"\./scripts/ci/local-ci\.sh")),
    ("canonical local build gate", re.compile(r"\./scripts/build-go-binaries\.sh")),
    ("golangci-lint full run", re.compile(r"\bgolangci-lint\s+run\b")),
    ("govulncheck", re.compile(r"\bgovulncheck\b")),
    ("gosec", re.compile(r"\bgosec\b")),
    ("private module token", re.compile(r"\bMODULES_TOKEN\b")),
    ("local race switch", re.compile(r"\bRUN_RACE_TESTS\b")),
    ("local dependency hygiene switch", re.compile(r"\bRUN_DEPENDENCY_HYGIENE\b")),
    ("admin frontend full install", re.compile(r"\bnpm\s+ci\b")),
    ("admin frontend full build", re.compile(r"\bnpm\s+run\s+build\b")),
]


def resolve_profile() -> str:
    override = os.environ.get("WORKFLOW_GATE_PROFILE", "").strip()
    if override:
        if override not in {"app", "lib"}:
            print(f"unsupported WORKFLOW_GATE_PROFILE={override}; expected app or lib", file=sys.stderr)
            raise SystemExit(2)
        return override
    return "app" if Path("scripts/ci/pre-push-gate.sh").is_file() else "lib"


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


def strip_yaml_comment_and_quotes(raw: str) -> str:
    out: list[str] = []
    quote: str | None = None
    i = 0
    while i < len(raw):
        ch = raw[i]
        if quote is not None:
            out.append(" ")
            if quote == '"' and ch == "\\" and i + 1 < len(raw):
                i += 1
                out.append(" ")
            elif ch == quote:
                quote = None
        elif ch in {"'", '"'}:
            quote = ch
            out.append(" ")
        elif ch == "#":
            out.extend(" " for _ in raw[i:])
            break
        else:
            out.append(ch)
        i += 1
    return "".join(out)


def strip_yaml_comment(raw: str) -> str:
    out: list[str] = []
    quote: str | None = None
    i = 0
    while i < len(raw):
        ch = raw[i]
        if quote is not None:
            out.append(ch)
            if quote == '"' and ch == "\\" and i + 1 < len(raw):
                i += 1
                out.append(raw[i])
            elif ch == quote:
                quote = None
        elif ch in {"'", '"'}:
            quote = ch
            out.append(ch)
        elif ch == "#":
            break
        else:
            out.append(ch)
        i += 1
    return "".join(out).rstrip()


def unquote_scalar(value: str) -> str:
    value = strip_yaml_comment(value).strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def structural_yaml_failures(text: str) -> list[tuple[int, str]]:
    failures: list[tuple[int, str]] = []
    block_scalar_indent: int | None = None
    for line_no, raw in enumerate(text.splitlines(), start=1):
        if block_scalar_indent is not None:
            if meaningful(raw) and indent(raw) <= block_scalar_indent:
                block_scalar_indent = None
            else:
                continue
        if not meaningful(raw):
            continue
        if raw[: indent(raw)].find("\t") != -1:
            failures.append((line_no, "tabs in workflow indentation are unsupported"))
        scrubbed = strip_yaml_comment_and_quotes(raw)
        if re.search(r"^\s*<<\s*:", scrubbed):
            failures.append((line_no, "YAML merge keys are unsupported in workflow policy checks"))
        if re.search(r"(^|[\s:{,\[])[&*][A-Za-z0-9_-]+\b", scrubbed):
            failures.append((line_no, "YAML anchors and aliases are unsupported in workflow policy checks"))
        if re.search(r"(^|[\s:{,\[])(![A-Za-z0-9_/.-]+|!<[^>]+>)", scrubbed):
            failures.append((line_no, "YAML tags are unsupported in workflow policy checks"))
        stripped = scrubbed.strip()
        if re.match(r"^(?:-\s*)?[\[{]\s*$", stripped):
            failures.append((line_no, "multi-line YAML flow collections are unsupported in workflow policy checks"))
        kv = parse_key_value(raw)
        if kv is not None:
            _, value = kv
            normalized = unquote_scalar(value)
            if normalized.startswith("[") and not normalized.endswith("]"):
                failures.append((line_no, "multi-line YAML flow collections are unsupported in workflow policy checks"))
            if normalized.startswith("{") and normalized != "{}":
                failures.append((line_no, "YAML flow mappings are unsupported in workflow policy checks"))
        match = re.match(r"^\s*(?:-\s*)?(?:['\"]?[A-Za-z0-9_-]+['\"]?\s*:\s*)?([|>][+-]?)\s*(?:#.*)?$", raw)
        if match:
            block_scalar_indent = indent(raw)
    return failures


def parse_key_value(raw: str) -> tuple[str, str] | None:
    if not meaningful(raw):
        return None
    stripped = strip_yaml_comment(raw).strip()
    if stripped.startswith("- "):
        stripped = stripped[2:].strip()
    match = re.match(r"^(['\"]?)([A-Za-z0-9_-]+)\1\s*:\s*(.*)$", stripped)
    if not match:
        return None
    return match.group(2), match.group(3).strip()


def inline_event_names(value: str) -> set[str]:
    value = strip_yaml_comment(value).strip()
    if not value:
        return set()
    if value.startswith("[") and value.endswith("]"):
        names: set[str] = set()
        for raw in value[1:-1].split(","):
            item = raw.strip().strip("'\"")
            if item:
                names.add(item)
        return names
    if re.fullmatch(r"['\"]?[A-Za-z0-9_-]+['\"]?", value):
        return {value.strip("'\"")}
    return set()


def event_triggered(text: str, event_name: str) -> bool:
    lines = text.splitlines()
    for index, raw in enumerate(lines):
        if not meaningful(raw) or indent(raw) != 0:
            continue
        parsed = parse_key_value(raw)
        if parsed is None:
            continue
        key, value = parsed
        if key.lower() != "on":
            continue
        if event_name in inline_event_names(value):
            return True
        if value:
            continue
        on_indent = indent(raw)
        for child in lines[index + 1 :]:
            if not meaningful(child):
                continue
            child_indent = indent(child)
            if child_indent <= on_indent:
                break
            child_stripped = strip_yaml_comment(child).strip()
            if child_stripped.startswith("- "):
                if child_stripped[2:].strip().strip("'\"") == event_name:
                    return True
                continue
            child_parsed = parse_key_value(child)
            if child_parsed is not None and child_parsed[0] == event_name:
                return True
    return False


def line_number_at(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def skip_expr_string(body: str, offset: int) -> int:
    quote = body[offset]
    i = offset + 1
    while i < len(body):
        if quote == '"' and body[i] == "\\":
            i += 2
            continue
        if body[i] == quote:
            return i + 1
        i += 1
    return len(body)


def parse_bracket_secret(body: str, offset: int) -> tuple[int, str | None]:
    depth = 1
    i = offset + 1
    while i < len(body):
        if body[i] in {"'", '"'}:
            i = skip_expr_string(body, i)
            continue
        if body[i] == "[":
            depth += 1
        elif body[i] == "]":
            depth -= 1
            if depth == 0:
                inner = body[offset + 1 : i].strip()
                literal = re.fullmatch(r"(['\"])([A-Za-z_][A-Za-z0-9_]*)\1", inner)
                return i + 1, literal.group(2) if literal else None
        i += 1
    return len(body), None


def secret_reference_failures(text: str) -> list[tuple[int, str]]:
    failures: list[tuple[int, str]] = []
    for expr in SECRET_EXPR_RE.finditer(text):
        body = expr.group("body")
        body_offset = expr.start("body")
        i = 0
        while i < len(body):
            if body[i] in {"'", '"'}:
                i = skip_expr_string(body, i)
                continue
            if not (
                body.startswith("secrets", i)
                and (i == 0 or not re.match(r"[A-Za-z0-9_]", body[i - 1]))
                and (i + 7 == len(body) or not re.match(r"[A-Za-z0-9_]", body[i + 7]))
            ):
                i += 1
                continue
            line_no = line_number_at(text, body_offset + i)
            j = i + 7
            while j < len(body) and body[j].isspace():
                j += 1
            if j < len(body) and body[j] == ".":
                j += 1
                while j < len(body) and body[j].isspace():
                    j += 1
                name_match = re.match(r"[A-Za-z_][A-Za-z0-9_]*", body[j:])
                name = name_match.group(0) if name_match else "<invalid>"
                if name != "GITHUB_TOKEN":
                    failures.append((line_no, f"secrets.{name}"))
                i = j + len(name)
                continue
            if j < len(body) and body[j] == "[":
                next_i, name = parse_bracket_secret(body, j)
                if name != "GITHUB_TOKEN":
                    ref = f"secrets.{name}" if name is not None else "secrets[dynamic]"
                    failures.append((line_no, ref))
                i = next_i
                continue
            failures.append((line_no, "the whole secrets object"))
            i = j
    return failures


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
    inline_value = unquote_scalar(inline_value)
    if inline_value:
        return inline_value in {"read-all", "{}"}
    saw_entry = False
    seen: set[str] = set()
    for _, raw in entries:
        if not meaningful(raw):
            continue
        line = strip_yaml_comment(raw)
        match = re.match(r"^\s*([A-Za-z0-9_-]+)\s*:\s*([A-Za-z0-9_-]+)\s*$", line)
        if not match:
            continue
        key = match.group(1)
        if key in seen:
            return False
        seen.add(key)
        saw_entry = True
        if match.group(2) not in {"read", "none"}:
            return False
    return saw_entry


def top_level_permissions_block_exists(text: str) -> bool:
    return any(block_indent == 0 for _, block_indent, _, _ in permission_blocks(text))


def non_readonly_permission_blocks(text: str) -> list[int]:
    return [
        line_no
        for line_no, _, inline_value, entries in permission_blocks(text)
        if not permissions_block_is_readonly(inline_value, entries)
    ]


def is_checkout_uses(raw: str) -> bool:
    parsed = parse_key_value(raw)
    if parsed is None:
        return False
    key, value = parsed
    return key == "uses" and unquote_scalar(value).startswith("actions/checkout@")


def step_item_bounds(lines: list[str], index: int) -> tuple[int, int, int]:
    current_indent = indent(lines[index])
    start = index
    step_indent = current_indent
    if not lines[index].strip().startswith("- "):
        for cursor in range(index - 1, -1, -1):
            if not meaningful(lines[cursor]):
                continue
            if indent(lines[cursor]) < current_indent and lines[cursor].strip().startswith("- "):
                start = cursor
                step_indent = indent(lines[cursor])
                break
    end = len(lines)
    for cursor in range(start + 1, len(lines)):
        if meaningful(lines[cursor]) and indent(lines[cursor]) <= step_indent:
            end = cursor
            break
    return start, end, step_indent


def checkout_credential_failures(text: str) -> list[int]:
    failures: list[int] = []
    lines = text.splitlines()
    for index, raw in enumerate(lines):
        if raw.strip().startswith("#"):
            continue
        if not is_checkout_uses(raw):
            continue
        _, end, step_indent = step_item_bounds(lines, index)
        with_indent: int | None = None
        with_index: int | None = None
        for cursor in range(index + 1, end):
            if not meaningful(lines[cursor]):
                continue
            if indent(lines[cursor]) <= step_indent:
                break
            parsed = parse_key_value(lines[cursor])
            if parsed is not None and parsed[0] == "with":
                with_indent = indent(lines[cursor])
                with_index = cursor
                break
        if with_indent is None or with_index is None:
            failures.append(index + 1)
            continue
        values: list[str] = []
        for cursor in range(with_index + 1, end):
            if not meaningful(lines[cursor]):
                continue
            if indent(lines[cursor]) <= with_indent:
                break
            parsed = parse_key_value(lines[cursor])
            if parsed is not None and parsed[0] == "persist-credentials":
                values.append(unquote_scalar(parsed[1]))
        if values != ["false"]:
            failures.append(index + 1)
    return failures


def pr_heavy_lines(text: str) -> list[tuple[int, str]]:
    failures: list[tuple[int, str]] = []
    for line_no, raw in enumerate(text.splitlines(), start=1):
        if not meaningful(raw):
            continue
        scrubbed = strip_yaml_comment_and_quotes(raw)
        for desc, pattern in PR_HEAVY_LINE_PATTERNS:
            if pattern.search(scrubbed):
                failures.append((line_no, desc))
        if re.search(r"\bgo\s+test\b", scrubbed):
            if re.search(r"\s-race\b", scrubbed):
                failures.append((line_no, "race test"))
            if "./..." in scrubbed and not re.search(r"-run\s+['\"]?\^\$['\"]?", scrubbed):
                failures.append((line_no, "full repository go test"))
    return failures


def main() -> int:
    profile = resolve_profile()
    failures: list[str] = []
    for path in workflow_paths(sys.argv[1:]):
        text = path.read_text(encoding="utf-8")
        for line_no, reason in structural_yaml_failures(text):
            failures.append(f"{path}:{line_no}: {reason}")
        has_pr = event_triggered(text, "pull_request")

        if event_triggered(text, "pull_request_target"):
            failures.append(f"{path}: pull_request_target workflow is not allowed")

        if path.name in SECURITY_WORKFLOWS and has_pr:
            failures.append(f"{path}: security workflow must not run on pull_request")

        if not has_pr:
            continue

        for line_no, ref in secret_reference_failures(text):
            failures.append(f"{path}:{line_no}: pull_request workflow must not reference {ref}")
        for line_no in secrets_inherit_lines(text):
            failures.append(f"{path}:{line_no}: pull_request workflow must not use secrets: inherit")
        for line_no in reusable_workflow_secret_lines(text):
            failures.append(f"{path}:{line_no}: pull_request reusable workflow secrets are not allowed")
        if not top_level_permissions_block_exists(text):
            failures.append(f"{path}: pull_request workflow must define top-level read-only permissions")
        for line_no in non_readonly_permission_blocks(text):
            failures.append(f"{path}:{line_no}: pull_request workflow must use read-only permissions or none")
        for line_no in checkout_credential_failures(text):
            failures.append(f"{path}:{line_no}: actions/checkout in pull_request workflow must set persist-credentials: false")
        if profile == "app":
            for line_no, desc in pr_heavy_lines(text):
                failures.append(f"{path}:{line_no}: PR fast gate must not reintroduce {desc}")

    if failures:
        print(f"FAIL: workflow PR boundary / quality ownership violation (profile={profile})", file=sys.stderr)
        for failure in failures:
            print(f" - {failure}", file=sys.stderr)
        return 1

    print(f"ok: workflow PR boundary / quality ownership check passed (profile={profile})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
PY
