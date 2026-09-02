#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any


class PolicyError(RuntimeError):
    pass


REQUIRED_BUDGETS = {
    "production_file_lines",
    "test_file_lines",
    "funcs_per_file",
    "methods_per_type",
    "struct_fields",
    "aggregate_fields",
    "function_lines",
}
SKIP_PARTS = {
    ".git", ".claude", ".worktrees", "vendor", "bin", "artifacts",
    "logs", ".tmp", "generated",
}
TOP_DECL_RE = re.compile(r"^(func|type|const|var)\b")
FUNC_RE = re.compile(r"^func\s+")
FUNC_NAME_RE = re.compile(r"^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)")
METHOD_RE = re.compile(r"^func\s+\(([^)]*)\)\s+([A-Za-z_][A-Za-z0-9_]*)")
STRUCT_RE = re.compile(r"^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+struct\s*{")
PACKAGE_RE = re.compile(r"^package\s+(\w+)")
PARTITION_FILE_RE = re.compile(r"_part[0-9]+(?:_test)?\.go$")


@dataclass(frozen=True)
class Finding:
    id: str
    rule: str
    path: str
    actual: int
    advisory_limit: int
    hard_limit: int
    level: str
    message: str
    symbol: str | None = None
    evidence_paths: tuple[str, ...] = ()

    def payload(self) -> dict[str, Any]:
        value = asdict(self)
        if self.symbol is None:
            value.pop("symbol")
        if not self.evidence_paths:
            value.pop("evidence_paths")
        else:
            value["evidence_paths"] = list(self.evidence_paths)
        return value


def reject_duplicate_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise PolicyError(f"duplicate policy key: {key}")
        result[key] = value
    return result


def load_policy(path: Path) -> dict[str, Any]:
    try:
        policy = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=reject_duplicate_pairs)
    except (OSError, UnicodeError, json.JSONDecodeError, PolicyError) as exc:
        raise PolicyError(f"read policy: {exc}") from exc
    if not isinstance(policy, dict) or policy.get("schema_version") != 1:
        raise PolicyError("policy schema_version must be 1")
    budgets = policy.get("budgets")
    if not isinstance(budgets, dict) or set(budgets) != REQUIRED_BUDGETS:
        raise PolicyError("policy budgets must contain the exact required rule set")
    for name, budget in budgets.items():
        if not isinstance(budget, dict) or set(budget) != {"advisory", "hard"}:
            raise PolicyError(f"budget {name} must contain advisory and hard")
        advisory, hard = budget["advisory"], budget["hard"]
        if not isinstance(advisory, int) or not isinstance(hard, int) or advisory < 1 or hard < advisory:
            raise PolicyError(f"budget {name} is invalid")
    allowlist = policy.get("struct_field_allowlist", [])
    baselines = policy.get("legacy_hard_baselines", {})
    if not isinstance(allowlist, list) or any(not isinstance(item, str) or ":" not in item for item in allowlist):
        raise PolicyError("struct_field_allowlist is invalid")
    if len(allowlist) != len(set(allowlist)):
        raise PolicyError("struct_field_allowlist contains duplicates")
    if not isinstance(baselines, dict) or any(
        not isinstance(key, str) or not isinstance(value, int) or value < 1
        for key, value in baselines.items()
    ):
        raise PolicyError("legacy_hard_baselines is invalid")
    if not isinstance(policy.get("forbid_partition_files", False), bool):
        raise PolicyError("forbid_partition_files must be a boolean")
    return policy


def package_name(lines: list[str]) -> str:
    for line in lines:
        match = PACKAGE_RE.match(line)
        if match:
            return match.group(1)
    return "unknown"


def package_key(path: Path, package: str) -> str:
    parent = path.parent.as_posix()
    return f"{parent}.{package}" if parent != "." else package


def receiver_type(receiver: str) -> str:
    fields = receiver.strip().split()
    if not fields:
        return "unknown"
    raw = fields[-1].lstrip("*")
    match = re.match(r"([A-Za-z_][A-Za-z0-9_]*)(?:\[[^\]]+\])?$", raw)
    return match.group(1) if match else raw


def top_level_chunks(lines: list[str]) -> list[tuple[int, int]]:
    starts = [index for index, line in enumerate(lines) if TOP_DECL_RE.match(line)]
    return [(start, starts[index + 1] if index + 1 < len(starts) else len(lines))
            for index, start in enumerate(starts)]


def read_changed_paths(path: Path | None) -> set[str] | None:
    if path is None:
        return None
    try:
        raw = path.read_bytes()
    except OSError as exc:
        raise PolicyError(f"read changed paths: {exc}") from exc
    return {item.decode("utf-8") for item in raw.split(b"\0") if item}


def scan(root: Path, policy: dict[str, Any]) -> tuple[int, list[Finding]]:
    budgets: dict[str, dict[str, int]] = policy["budgets"]
    allowlist = set(policy.get("struct_field_allowlist", []))
    baselines: dict[str, int] = policy.get("legacy_hard_baselines", {})
    forbid_partition: bool = policy.get("forbid_partition_files", False)
    findings: list[Finding] = []
    over_hard_ids: set[str] = set()
    method_counts: Counter[tuple[str, str]] = Counter()
    method_paths: defaultdict[tuple[str, str], set[str]] = defaultdict(set)
    structs: dict[tuple[str, str], tuple[str, int, list[str]]] = {}
    scanned_files = 0

    def add(rule: str, path: str, actual: int, symbol: str | None = None,
            evidence_paths: tuple[str, ...] = ()) -> None:
        budget = budgets[rule]
        advisory, hard = budget["advisory"], budget["hard"]
        if actual <= advisory:
            return
        stable_id = f"{rule}:{path}" + (f":{symbol}" if symbol else "")
        level = "advisory"
        if actual > hard:
            over_hard_ids.add(stable_id)
            if stable_id not in baselines:
                level = "hard_ceiling"
            elif actual > baselines[stable_id]:
                level = "hard_ratchet"
        findings.append(Finding(
            id=stable_id, rule=rule, path=path, symbol=symbol, actual=actual,
            advisory_limit=advisory, hard_limit=hard, level=level,
            message=f"{rule} actual={actual} advisory={advisory} hard={hard}",
            evidence_paths=evidence_paths,
        ))

    files = sorted(path for path in root.rglob("*.go")
                   if path.is_file()
                   and not path.name.endswith("_generated.go")
                   and not any(part in SKIP_PARTS for part in path.relative_to(root).parts))
    for absolute in files:
        scanned_files += 1
        relative = absolute.relative_to(root)
        path = relative.as_posix()
        if forbid_partition and PARTITION_FILE_RE.search(absolute.name):
            # DEC-20260902-structure-gate-cohesion-over-line-budget: 줄 수 예산이 만든 _partN 분할은 invariant 위반이다.
            findings.append(Finding(
                id=f"partition_file:{path}", rule="partition_file", path=path, actual=1,
                advisory_limit=0, hard_limit=0, level="hard_invariant",
                message="file name carries a _partN split suffix; merge it or rename it by responsibility",
            ))
        try:
            lines = absolute.read_text(encoding="utf-8").splitlines()
        except (OSError, UnicodeError) as exc:
            raise PolicyError(f"read source {path}: {exc}") from exc
        is_test = absolute.name.endswith("_test.go")
        add("test_file_lines" if is_test else "production_file_lines", path, len(lines))
        if is_test:
            continue
        package = package_key(relative, package_name(lines))
        add("funcs_per_file", path, sum(1 for line in lines if FUNC_RE.match(line)))
        for line in lines:
            method = METHOD_RE.match(line)
            if method:
                key = (package, receiver_type(method.group(1)))
                method_counts[key] += 1
                method_paths[key].add(path)
        for start, end in top_level_chunks(lines):
            first = lines[start]
            if FUNC_RE.match(first):
                match = FUNC_NAME_RE.match(first)
                name = match.group(1) if match else "unknown"
                add("function_lines", path, end - start, name)
        for index, line in enumerate(lines):
            match = STRUCT_RE.match(line)
            if not match:
                continue
            name = match.group(1)
            fields = 0
            direct = 0
            embeds: list[str] = []
            cursor = index + 1
            while cursor < len(lines) and not lines[cursor].strip().startswith("}"):
                current = lines[cursor].strip()
                if current and not current.startswith("//"):
                    fields += 1
                    code = current.split("`")[0].split("//")[0].strip()
                    if code and " " not in code and "\t" not in code:
                        embeds.append(code.lstrip("*"))
                    else:
                        direct += 1
                cursor += 1
            structs[(package, name)] = (path, direct, embeds)
            if f"{path}:{name}" not in allowlist:
                add("struct_fields", path, fields, name)

    for (package, receiver), actual in sorted(method_counts.items()):
        evidence = tuple(sorted(method_paths[(package, receiver)]))
        add("methods_per_type", package, actual, receiver, evidence)

    memo: dict[tuple[str, str], int] = {}
    def reachable(key: tuple[str, str], stack: frozenset[tuple[str, str]]) -> int:
        if key in memo:
            return memo[key]
        if key in stack:
            return 0
        entry = structs.get(key)
        if entry is None:
            return 1
        _, direct, embeds = entry
        total = direct
        for embed in embeds:
            child = (key[0], embed)
            total += reachable(child, stack | {key}) if child in structs else 1
        memo[key] = total
        return total

    for key, (path, _, embeds) in sorted(structs.items()):
        package, name = key
        if f"{path}:{name}" in allowlist:
            continue
        if any((package, embed) in structs for embed in embeds):
            add("aggregate_fields", path, reachable(key, frozenset()), name)

    stale = sorted(set(baselines) - over_hard_ids)
    if stale:
        raise PolicyError("stale legacy_hard_baselines: " + ", ".join(stale))
    return scanned_files, sorted(findings, key=lambda item: item.id)


def emit_text(findings: list[Finding]) -> None:
    if not findings:
        return
    for finding in findings[:10]:
        print(f"[{finding.level}] {finding.id}: {finding.message}")
    if len(findings) > 10:
        print(f"... {len(findings) - 10} finding(s) omitted")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--policy", type=Path, required=True)
    parser.add_argument("--mode", choices=("advisory", "hard"), required=True)
    parser.add_argument("--format", choices=("text", "json"), default="text")
    parser.add_argument("--changed-paths-file", type=Path)
    args = parser.parse_args()
    try:
        root = args.root.resolve(strict=True)
        policy_path = args.policy if args.policy.is_absolute() else root / args.policy
        policy = load_policy(policy_path)
        changed = read_changed_paths(args.changed_paths_file)
        scanned_files, findings = scan(root, policy)
        if changed is not None:
            findings = [finding for finding in findings if changed.intersection(
                finding.evidence_paths or (finding.path,))]
        hard_failure = any(item.level in {"hard_ceiling", "hard_ratchet", "hard_invariant"}
                           for item in findings)
        report = {
            "schema_version": 1,
            "status": "findings" if findings else "ok",
            "mode": args.mode,
            "scanned_files": scanned_files,
            "findings": [item.payload() for item in findings],
            "errors": [],
        }
        if args.format == "json":
            print(json.dumps(report, ensure_ascii=False, sort_keys=True, separators=(",", ":")))
        else:
            emit_text(findings)
        return 1 if args.mode == "hard" and hard_failure else 0
    except (OSError, PolicyError) as exc:
        report = {
            "schema_version": 1, "status": "error", "mode": args.mode,
            "scanned_files": 0, "findings": [], "errors": [str(exc)],
        }
        if args.format == "json":
            print(json.dumps(report, ensure_ascii=False, sort_keys=True, separators=(",", ":")))
        else:
            print(f"[policy_error] {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
