#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import os
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
REMOTE_LIBRARY_ALLOWED_RUN_LINES = frozenset(
    {
        "echo ok",
        "bash scripts/ci/check-workflow-secrets.sh",
        "bash scripts/ci/check-workflow-secrets_test.sh",
        'interpreter="$(bash scripts/ci/python-runner.sh --print-interpreter)"',
        "printf 'CI_PYTHON_BIN=%s\\n' \"$interpreter\" >>\"$GITHUB_ENV\"",
        "printf 'CI_PYTHON_RUNTIME_ROOT=%s\\n' \"$GITHUB_WORKSPACE\" >>\"$GITHUB_ENV\"",
        '\"$CI_PYTHON_BIN\" scripts/ci/check-workflow-ci-owner.py',
        '\"$CI_PYTHON_BIN\" scripts/ci/check-workflow-ci-owner_test.py',
        "bash scripts/ci/check-release-provenance.sh",
        "bash scripts/ci/check-crosscutting-boundaries.sh",
        "bash scripts/check-sql-ownership.sh",
        "bash scripts/ci/check-structure.sh --mode hard --format text",
        "mapfile -t files < <(find . -path './.git' -prune -o -name '*.go' -print)",
        "if (( ${#files[@]} == 0 )); then",
        "exit 0",
        "fi",
        'unformatted="$(gofmt -l "${files[@]}")"',
        'if [[ -n "${unformatted}" ]]; then',
        'echo "::error::gofmt required for the following files:"',
        'echo "${unformatted}"',
        "exit 1",
        "go vet ./...",
        "python -m pip install --disable-pip-version-check --no-cache-dir --no-deps uv==0.12.7",
        "bash scripts/check-hmac-boundary.sh",
        "bash scripts/check-hmac-boundary_test.sh",
        "bash scripts/ci/go-tooling.sh golangci-lint run -c .golangci.yml ./...",
        "go test -count=1 ./...",
        "go test -race",
        "go test -race -count=1 ./...",
        "go mod tidy -diff",
        "IRIS_CLIENT_VALKEY_TEST_ADDR=127.0.0.1:6379 go test -race -count=1 -v ./internal/dedup/",
        "go test -count=1 -run '^TestPromptGuardAllocationCeilings$' ./pkg/promptguard",
        "go test -count=1 -run '^TestLoggingAllocationCeilings$' ./pkg/logging",
    }
)
REMOTE_LIBRARY_CANONICAL_WORKFLOW_SHA256 = {
    "github.com/park285/iris-client-go/v2": "a7e3cc8323af96571cdc397ff9881437f9fb09c502a22f80b92b1abced0998bb",
    "github.com/park285/shared-go/v2": "51a7efbf7365ce8ae17ef369d28b4256795c80e585e591882012d08ebe726269",
}
REMOTE_LIBRARY_FIXTURE_MODULE = "example.invalid/workflow-ci-owner-fixture"
REMOTE_LIBRARY_FIXTURE_WORKFLOW_SHA256 = "132a3046c47792056c3253f2d0c1f42c084afba13368e8ff9eaa507c471ba973"
APP_CANONICAL_WORKFLOW_SHA256 = {
    "github.com/kapu/chat-bot-go-kakao": "408fd3ffd058a4daaf3b6e9826c1c739e6aec267d9c2d9c43c49835d55a07dfe",
    "github.com/park285/twentyq-bot": "ddd83e0b6cf49bf4572664ccf1f640ad174b51359e4bbc0bae8ca8046b6d721e",
    "github.com/kapu/hololive-bot-workspace": "ab0356c03645fd568ee1abb5ab7ef20b5e269ffbaf0aba7e538dcce0a6926fea",
}
LOCAL_APP_FIXTURE_MODULE = "example.invalid/workflow-ci-owner-local-app-fixture"
LOCAL_APP_FIXTURE_WORKFLOW_SHA256 = "6f937654a6c5204be51d19d4242e65d6a022f07b54780ac37e2b59c40de4fe86"
REMOTE_APP_FIXTURE_MODULE = "example.invalid/workflow-ci-owner-remote-app-fixture"
REMOTE_APP_FIXTURE_WORKFLOW_SHA256 = "2d35debf1a90023b08a6c598268a9e47635d6358d593ee4eb7780beb3c26a13e"


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
        event_indent: int | None = None
        for child in lines[index + 1 :]:
            if not meaningful(child):
                continue
            child_indent = indent(child)
            if child_indent == 0:
                break
            if event_indent is None:
                event_indent = child_indent
            if child_indent != event_indent:
                continue
            child_source = strip_yaml_comment(child).strip()
            if child_source.startswith("- "):
                event = unquote(child_source[2:].strip())
            else:
                child_parsed = parse_key_value(child)
                if child_parsed is None:
                    continue
                event = child_parsed[0]
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


def workflow_run_scripts(text: str) -> list[str]:
    lines = text.splitlines()
    scripts: list[str] = []
    for index, raw in enumerate(lines):
        parsed = parse_key_value(raw)
        if parsed is None or parsed[0] != "run":
            continue
        value = unquote(parsed[1])
        if re.fullmatch(r"[|>](?:[1-9][+-]?|[+-][1-9]?)?", value):
            base_indent = indent(raw)
            block: list[str] = []
            for child in lines[index + 1 :]:
                if child.strip() and indent(child) <= base_indent:
                    break
                block.append(child)
            scripts.append("\n".join(block))
        elif value:
            scripts.append(value)
    return scripts


def executable_shell_text(script: str) -> str:
    lines = [strip_shell_comment(line) for line in script.replace("\\\n", " ").splitlines()]
    return "\n".join(line for line in lines if line.strip())


def strip_shell_comment(raw: str) -> str:
    out: list[str] = []
    quote: str | None = None
    escaped = False
    for index, char in enumerate(raw):
        if escaped:
            out.append(char)
            escaped = False
            continue
        if char == "\\" and quote != "'":
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
        if (
            char == "#"
            and quote is None
            and (index == 0 or raw[index - 1].isspace() or raw[index - 1] in ";&|()")
        ):
            break
        out.append(char)
    return "".join(out).rstrip()


def invokes_command(script: str, command: str) -> bool:
    source = executable_shell_text(script)
    boundary = r"(?:^|[\s;&|(){}!'\"\x60])"
    executable_path = r"(?:[^\s;&|(){}!'\"\x60]*/)?"
    terminator = r"(?=$|[\s;&|(){}!'\"\x60])"
    return re.search(
        boundary + executable_path + re.escape(command) + terminator,
        source,
        re.MULTILINE,
    ) is not None


def has_only_canonical_remote_library_commands(scripts: list[str]) -> bool:
    return all(
        line.strip() in REMOTE_LIBRARY_ALLOWED_RUN_LINES
        for script in scripts
        for line in executable_shell_text(script).splitlines()
        if line.strip()
    )


def has_go_compile_smoke(scripts: list[str]) -> bool:
    pattern = re.compile(r"^go\s+test\s+-run\s+(['\"])[\^][$]\1(?:\s|$)")
    return any(
        pattern.search(line.strip()) is not None
        for script in scripts
        for line in executable_shell_text(script).splitlines()
        if line.strip()
    )


def invokes_repo_script_directly(scripts: list[str], script_path: str) -> bool:
    pattern = re.compile(r"^bash\s+" + re.escape(script_path) + r"(?:\s|$)")
    return any(
        pattern.search(line.strip()) is not None
        for script in scripts
        for line in executable_shell_text(script).splitlines()
        if line.strip()
    )


def module_path(root: Path) -> str:
    path = root / "go.mod"
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise ContractError(f"cannot read go.mod: {exc}") from exc
    for raw in lines:
        line = raw.strip()
        if not line or line.startswith("//"):
            continue
        match = re.fullmatch(r"module\s+([^\s]+)", line)
        if match is None:
            raise ContractError("go.mod must declare module before other directives")
        return match.group(1)
    raise ContractError("go.mod is missing a module declaration")


def expected_remote_library_workflow_sha256(root: Path) -> str:
    module = module_path(root)
    expected = REMOTE_LIBRARY_CANONICAL_WORKFLOW_SHA256.get(module)
    if expected is not None:
        return expected
    if (
        module == REMOTE_LIBRARY_FIXTURE_MODULE
        and os.environ.get("WORKFLOW_CI_OWNER_CONTRACT_FIXTURE") == "1"
    ):
        return REMOTE_LIBRARY_FIXTURE_WORKFLOW_SHA256
    raise ContractError(f"remote library module has no canonical workflow snapshot: {module}")


def expected_app_workflow_sha256(root: Path, owner: str) -> str:
    module = module_path(root)
    expected = APP_CANONICAL_WORKFLOW_SHA256.get(module)
    if expected is not None:
        return expected
    if os.environ.get("WORKFLOW_CI_OWNER_CONTRACT_FIXTURE") == "1":
        if module == LOCAL_APP_FIXTURE_MODULE and owner == "local":
            return LOCAL_APP_FIXTURE_WORKFLOW_SHA256
        if module == REMOTE_APP_FIXTURE_MODULE and owner == "remote":
            return REMOTE_APP_FIXTURE_WORKFLOW_SHA256
    raise ContractError(f"{owner} app module has no canonical workflow snapshot: {module}")


def invokes_go_race_test(script: str) -> bool:
    source = executable_shell_text(script)
    prefix = r"(?:^|[;&|]\s*|\b(?:then|do)\s+)\s*"
    wrappers = r"(?:(?:env|command)\s+)*(?:[A-Za-z_][A-Za-z0-9_]*=[^\s;&|]+\s+)*"
    return (
        re.search(
            prefix + wrappers + r"go\s+test\b[^\n;&|]*?(?:^|\s)-race(?:\s|$)",
            source,
            re.MULTILINE,
        )
        is not None
    )


def validate(root: Path) -> tuple[str, str, list[str]]:
    profile = read_declaration(root, PROFILE_PATH, {"app", "lib"})
    owner = read_declaration(root, OWNER_PATH, {"local", "remote"})
    primary = read_workflow(root, PRIMARY_PATH)
    security_path, security = find_security_workflow(root)
    run_scripts = workflow_run_scripts(primary)

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

    if profile == "app":
        try:
            expected_workflow_sha256 = expected_app_workflow_sha256(root, owner)
        except ContractError as exc:
            failures.append(f"{PRIMARY_PATH}: {exc}")
        else:
            actual_workflow_sha256 = hashlib.sha256(primary.encode("utf-8")).hexdigest()
            if actual_workflow_sha256 != expected_workflow_sha256:
                failures.append(
                    f"{PRIMARY_PATH}: {owner} app gate must match its exact canonical workflow "
                    "snapshot, including triggers, permissions, env, defaults, uses, shells, "
                    "and ordered steps"
                )

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
            if not has_go_compile_smoke(run_scripts):
                failures.append(
                    f"{PRIMARY_PATH}: local app gate must execute go test -run '^$' directly"
                )
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
            for script_path in (
                "scripts/ci/public-pr-go-gate.sh",
                "scripts/ci/public-pr-frontend-gate.sh",
            ):
                if not invokes_repo_script_directly(run_scripts, script_path):
                    failures.append(
                        f"{PRIMARY_PATH}: remote app gate must invoke {script_path} directly"
                    )
        else:
            try:
                expected_workflow_sha256 = expected_remote_library_workflow_sha256(root)
            except ContractError as exc:
                failures.append(f"{PRIMARY_PATH}: {exc}")
            else:
                actual_workflow_sha256 = hashlib.sha256(primary.encode("utf-8")).hexdigest()
                if actual_workflow_sha256 != expected_workflow_sha256:
                    failures.append(
                        f"{PRIMARY_PATH}: remote library PR gate must match its exact canonical "
                        "workflow snapshot, including triggers, permissions, env, defaults, uses, "
                        "shells, and ordered steps"
                    )
            if not any(invokes_go_race_test(script) for script in run_scripts):
                failures.append(f"{PRIMARY_PATH}: remote library PR gate must execute go test -race directly")
            if not has_only_canonical_remote_library_commands(run_scripts):
                failures.append(
                    f"{PRIMARY_PATH}: remote library PR gate must use canonical direct commands; "
                    "PR-controlled Make targets and dynamic command construction are forbidden"
                )
            if any(invokes_command(script, "make") for script in run_scripts):
                failures.append(
                    f"{PRIMARY_PATH}: remote library PR gate must invoke security checks directly, not through PR-controlled Make targets"
                )

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
