#!/usr/bin/env python3
from __future__ import annotations

import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
EXCLUDED_DIRS = {
    ".git",
    "dist",
    "build",
    "node_modules",
    "target",
    "vendor",
    "docs",
    "fixtures",
    "testdata",
}
SQL_KEYWORD_RE = re.compile(
    r"\b("
    r"SELECT\s|INSERT\s+(?:INTO\s+)?|UPDATE\s|DELETE\s+FROM|"
    r"WITH\s+[A-Za-z_][A-Za-z0-9_]*\s+AS|ON\s+CONFLICT|"
    r"CREATE\s+(?:TABLE|INDEX|ROLE)|ALTER\s+(?:TABLE|ROLE)|"
    r"DROP\s+(?:TABLE|INDEX)?|TRUNCATE\b|GRANT\s|REVOKE\s|PRAGMA\s|"
    r"set_config\s*\(|pg_try_advisory_lock\s*\(|pg_advisory_unlock\s*\("
    r")"
)
DDL_RE = re.compile(
    r"\b(CREATE\s+(?:TABLE|INDEX|ROLE)|ALTER\s+(?:TABLE|ROLE)|DROP\s+(?:TABLE|INDEX)?|TRUNCATE\b|GRANT\s|REVOKE\s)\b",
    re.IGNORECASE,
)
# dbmigrate ledger의 literal 치환 금지 토큰.
# 퇴역 가드가 아니라 재도입을 막는 영구 계약 CI 게이트다. ledger 기록은 filename을 bind
# parameter($1)로만 넘기고, 값을 SQL 문자열에 끼워 넣는 helper(quoteSQLString)나 템플릿
# 자리(filename_literal)를 두지 않는다. 판정은 존재 기준이다: pkg/dbmigrate 아래 테스트가
# 아닌 Go 소스와 SQL 자산 어디에든(주석 포함) 토큰이 나타나면 실패한다. 파일 이름을 바꾸거나
# 쪼개도 빠지지 않도록 고정 경로가 아니라 패키지 전체를 본다. 검사할 파일이 하나도 없으면
# 판정할 수 없으므로 통과가 아니라 실패로 보고한다.
# 도입: 7ed8747(2026-07-07, "refactor(dbmigrate): require parameterized exec")에서 literal
# 치환 helper를 지우며 추가했다. 운영 env나 데이터에 묶인 퇴역 대상이 아니므로 제거 조건과
# 재검토 날짜를 두지 않는다. dbmigrate의 bind parameter 계약을 바꾸는 결정이 생길 때만 함께
# 고친다(stack-audit 2026-09-26 T17에서 퇴역 가드 목록에서 영구 계약으로 재분류).
DBMIGRATE_DIR_PARTS = ("pkg", "dbmigrate")
DBMIGRATE_LITERAL_SUBSTITUTION_BAN = ("quoteSQLString", "filename_literal")


@dataclass(frozen=True)
class Finding:
    path: Path
    line: int
    reason: str
    excerpt: str


def rel(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def line_number(source: str, offset: int) -> int:
    return source.count("\n", 0, offset) + 1


def iter_go_literals(source: str):
    i = 0
    n = len(source)
    in_line_comment = False
    in_block_comment = False
    while i < n:
        ch = source[i]
        if in_line_comment:
            if ch == "\n":
                in_line_comment = False
            i += 1
            continue
        if in_block_comment:
            if ch == "*" and i + 1 < n and source[i + 1] == "/":
                in_block_comment = False
                i += 2
                continue
            i += 1
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "/":
            in_line_comment = True
            i += 2
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "*":
            in_block_comment = True
            i += 2
            continue
        if ch == "`":
            start = i
            i += 1
            end = source.find("`", i)
            if end < 0:
                break
            yield start, source[i:end]
            i = end + 1
            continue
        if ch == '"':
            start = i
            i += 1
            value: list[str] = []
            while i < n:
                if source[i] == "\\":
                    value.append(source[i : i + 2])
                    i += 2
                    continue
                if source[i] == '"':
                    break
                value.append(source[i])
                i += 1
            yield start, "".join(value)
            i += 1
            continue
        i += 1


def iter_rust_literals(source: str):
    i = 0
    n = len(source)
    in_line_comment = False
    in_block_comment = False
    while i < n:
        ch = source[i]
        if in_line_comment:
            if ch == "\n":
                in_line_comment = False
            i += 1
            continue
        if in_block_comment:
            if ch == "*" and i + 1 < n and source[i + 1] == "/":
                in_block_comment = False
                i += 2
                continue
            i += 1
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "/":
            in_line_comment = True
            i += 2
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "*":
            in_block_comment = True
            i += 2
            continue
        literal_start = i
        if ch == "b" and i + 1 < n and source[i + 1] == "r":
            i += 1
            ch = source[i]
        if ch == "r":
            j = i + 1
            hashes = 0
            while j < n and source[j] == "#":
                hashes += 1
                j += 1
            if j < n and source[j] == '"':
                end_marker = '"' + ("#" * hashes)
                content_start = j + 1
                end = source.find(end_marker, content_start)
                if end < 0:
                    break
                yield literal_start, source[content_start:end]
                i = end + len(end_marker)
                continue
        if ch == '"':
            start = i
            i += 1
            value: list[str] = []
            while i < n:
                if source[i] == "\\":
                    value.append(source[i : i + 2])
                    i += 2
                    continue
                if source[i] == '"':
                    break
                value.append(source[i])
                i += 1
            yield start, "".join(value)
            i += 1
            continue
        i += 1


def should_skip_dir(path: Path) -> bool:
    return any(part in EXCLUDED_DIRS for part in path.relative_to(ROOT).parts)


def source_files() -> list[Path]:
    result: list[Path] = []
    for path in ROOT.rglob("*"):
        if not path.is_file() or should_skip_dir(path.parent):
            continue
        if path.suffix not in {".go", ".rs"}:
            continue
        if path.name.endswith("_test.go") or path.name.endswith("_test.rs"):
            continue
        result.append(path)
    return result


def sql_asset_files() -> list[Path]:
    return [
        path
        for path in ROOT.rglob("*")
        if path.is_file()
        and path.suffix in {".sql", ".tpl"}
        and not should_skip_dir(path.parent)
        and (path.name.endswith(".sql") or path.name.endswith(".sql.tpl"))
    ]


def allowed_sql_asset(path: Path) -> bool:
    parts = path.relative_to(ROOT).parts
    if "queries" in parts:
        return True
    if parts[:2] in {("scripts", "migrations"), ("scripts", "maintenance"), ("scripts", "init-db")}:
        return True
    return False


def migration_command_asset(path: Path) -> bool:
    return rel(path).startswith("pkg/dbmigrate/queries/")


def excerpt(value: str) -> str:
    return " ".join(value.strip().split())[:140]


def check_source_literals() -> list[Finding]:
    findings: list[Finding] = []
    for path in source_files():
        source = path.read_text(encoding="utf-8")
        literals = iter_go_literals(source) if path.suffix == ".go" else iter_rust_literals(source)
        for start, value in literals:
            match = SQL_KEYWORD_RE.search(value)
            if not match:
                continue
            findings.append(
                Finding(
                    path=path,
                    line=line_number(source, start),
                    reason=f"SQL string literal contains {match.group(1).strip()}",
                    excerpt=excerpt(value),
                )
            )
    return findings


def check_sql_asset_locations() -> list[Finding]:
    findings: list[Finding] = []
    for path in sql_asset_files():
        text = path.read_text(encoding="utf-8")
        if not allowed_sql_asset(path):
            findings.append(Finding(path, 1, "SQL asset is outside allowed locations", ""))
            continue
        if "queries" in path.relative_to(ROOT).parts and DDL_RE.search(text) and not migration_command_asset(path):
            findings.append(Finding(path, 1, "runtime query asset contains DDL/operator SQL", excerpt(text)))
    return findings


def missing_contract_target(path: Path) -> Finding:
    return Finding(
        path,
        1,
        "dbmigrate bind parameter contract target is missing (permanent contract)",
        "move this check together with the file instead of letting it pass silently",
    )


def check_dbmigrate_parameterized_api() -> list[Finding]:
    # 같은 영구 계약의 양성 검사: bind parameter를 받는 Execer와 VALUES ($1) 기록.
    # 대상 파일이 없으면 검사가 사라진 것이므로 실패로 보고한다.
    findings: list[Finding] = []
    dbmigrate_go = ROOT.joinpath(*DBMIGRATE_DIR_PARTS, "dbmigrate.go")
    record_sql = ROOT.joinpath(*DBMIGRATE_DIR_PARTS, "queries", "record_ledger.sql.tpl")

    if not dbmigrate_go.is_file():
        findings.append(missing_contract_target(dbmigrate_go))
    else:
        source = dbmigrate_go.read_text(encoding="utf-8")
        if "type Execer func(context.Context, string, ...any) error" not in source:
            findings.append(
                Finding(
                    dbmigrate_go,
                    1,
                    "dbmigrate Execer must accept bind parameters",
                    "want: type Execer func(context.Context, string, ...any) error",
                )
            )
        if "ExecContext(ctx, query, args...)" not in source:
            findings.append(
                Finding(
                    dbmigrate_go,
                    1,
                    "dbmigrate SQLExec must forward bind parameters",
                    "want: ExecContext(ctx, query, args...)",
                )
            )

    if not record_sql.is_file():
        findings.append(missing_contract_target(record_sql))
    else:
        text = record_sql.read_text(encoding="utf-8")
        if "VALUES ($1)" not in text:
            findings.append(
                Finding(record_sql, 1, "dbmigrate ledger record must bind filename", excerpt(text))
            )

    return findings


def dbmigrate_ban_targets() -> list[Path]:
    dbmigrate_dir = ROOT.joinpath(*DBMIGRATE_DIR_PARTS)
    if not dbmigrate_dir.is_dir():
        return []
    return sorted(
        path
        for path in dbmigrate_dir.rglob("*")
        if path.is_file()
        and (
            (path.suffix == ".go" and not path.name.endswith("_test.go"))
            or path.name.endswith(".sql")
            or path.name.endswith(".sql.tpl")
        )
    )


def check_dbmigrate_literal_substitution_ban() -> list[Finding]:
    targets = dbmigrate_ban_targets()
    if not targets:
        return [
            Finding(
                ROOT.joinpath(*DBMIGRATE_DIR_PARTS),
                1,
                "dbmigrate literal substitution ban has no files to check (permanent contract)",
                "move this check together with the package instead of letting it pass silently",
            )
        ]
    findings: list[Finding] = []
    for path in targets:
        text = path.read_text(encoding="utf-8")
        for token in DBMIGRATE_LITERAL_SUBSTITUTION_BAN:
            offset = text.find(token)
            if offset >= 0:
                findings.append(
                    Finding(
                        path,
                        line_number(text, offset),
                        "dbmigrate must not reintroduce literal substitution (permanent contract)",
                        token,
                    )
                )
    return findings


def main() -> int:
    findings = (
        check_source_literals()
        + check_sql_asset_locations()
        + check_dbmigrate_parameterized_api()
        + check_dbmigrate_literal_substitution_ban()
    )
    if not findings:
        print("SQL ownership check passed")
        return 0
    print("SQL ownership violations:", file=sys.stderr)
    for finding in findings:
        print(
            f"{rel(finding.path)}:{finding.line}: {finding.reason}: {finding.excerpt}",
            file=sys.stderr,
        )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
