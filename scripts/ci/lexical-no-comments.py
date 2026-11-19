#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ast
import fnmatch
import io
import json
import re
import shlex
import subprocess
import sys
import tokenize
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

EXEMPT_GLOBS = ("docs/*", "*/testdata/*", "testdata/*", "*/fixtures/*", "fixtures/*", "*/vendor/*", "vendor/*")
FRAGMENT_DIRECTIVE = re.compile(r"/\* fragment:[a-z][a-z0-9_]* \*/")
REGULAR_FILE_MODES = {"100644", "100755"}
SQL_BODY_LANGUAGES = frozenset({"sql", "plpgsql"})
SQL_STRING_PREFIXES = frozenset({"e", "u", "n", "b", "x"})
PSQL_LINE_COMMANDS = frozenset({"set", "unset", "echo", "if", "elif", "else", "endif", "quit", "q", "i", "ir", "include", "include_relative", "pset", "getenv"})
PSQL_LINE_COMMAND = re.compile(r"\\([A-Za-z_]+)([^\n]*)")
PSQL_TRAILING_COMMAND = re.compile(r"\\(?:g|gx|gexec|gset(?:[ \t]+[A-Za-z_][A-Za-z0-9_]*)?)[ \t]*\r?$", re.MULTILINE)
PSQL_CONDITIONAL_COMMANDS = frozenset({"if", "elif", "else", "endif"})
QUOTE_CONTINUATION = re.compile(r"[ \t\f\r\v]*(?:--[^\n]*)?\n(?:[ \t\f\r\n\v]+|--[^\n]*)*")
PYTHON_DIRECTIVE = re.compile(r"^(# -\*- coding|# ?noqa|# ?type: ?ignore|# ?pyright:|# ?pragma)")
PYTHON_BINARY = r"(?:[\w./+-]*/)?python(?:[0-9]+(?:\.[0-9]+)*)?"
PYTHON_VARIABLE = r"\$(?:CI_PYTHON_BIN|\{CI_PYTHON_BIN\})"
PYTHON_COMMAND = re.compile(
    rf"""^(?:exec\s+)?(?:uv\s+run\s+)?(?:{PYTHON_BINARY}|'{PYTHON_BINARY}'|"{PYTHON_BINARY}"|{PYTHON_VARIABLE}|"{PYTHON_VARIABLE}")(?=\s|$)"""
)


@dataclass(frozen=True)
class Span:
    start: int
    end: int
    line: int
    unsupported: bool = False


@dataclass
class Statement:
    tokens: list[tuple[str, int]]
    bodies: list[tuple[int, int]]
    start: int = -1


def span_at(text: str, start: int, end: int, unsupported: bool = False) -> Span:
    return Span(start, end, text.count("\n", 0, start) + 1, unsupported)


def is_identifier_char(char: str) -> bool:
    return char.isalnum() or char == "_"


def is_sql_identifier_char(char: str) -> bool:
    return char == "_" or ord(char) >= 0x80 or (char.isascii() and char.isalnum())


def dollar_tag(text: str, index: int) -> str | None:
    if text[index] != "$" or (index > 0 and is_sql_identifier_char(text[index - 1])):
        return None
    end = index + 1
    if end < len(text) and text[end] in "0123456789":
        return None
    while end < len(text) and is_sql_identifier_char(text[end]):
        end += 1
    if end < len(text) and text[end] == "$":
        return text[index : end + 1]
    return None


def nested_block_end(text: str, start: int, opener: str, closer: str) -> int:
    depth = 1
    cursor = start + len(opener)
    while cursor < len(text) and depth > 0:
        if text.startswith(opener, cursor):
            depth += 1
            cursor += len(opener)
        elif text.startswith(closer, cursor):
            depth -= 1
            cursor += len(closer)
        else:
            cursor += 1
    return cursor


def quoted_end(text: str, start: int, quote: str, backslash_escapes: bool) -> int:
    cursor = start + 1
    while cursor < len(text):
        if backslash_escapes and text[cursor] == "\\":
            cursor += 2
            continue
        if text[cursor] == quote:
            if text.startswith(quote * 2, cursor):
                cursor += 2
                continue
            return cursor + 1
        cursor += 1
    return len(text)


def opens_routine_body(tokens: list[tuple[str, int]]) -> bool:
    upper = [token.upper() for token, _ in tokens[-3:]]
    if upper and upper[-1] in ("AS", "DO"):
        return True
    return len(upper) == 3 and upper[0] == "DO" and upper[1] == "LANGUAGE"


def scans_routine_body(tokens: list[tuple[str, int]]) -> bool:
    candidates = [
        following.strip("'\"").lower()
        for (token, depth), (following, _) in zip(tokens, tokens[1:])
        if depth == 0 and token.upper() == "LANGUAGE" and (following[0] in "'\"" or is_sql_identifier_char(following[0]))
    ]
    if not candidates:
        return bool(tokens) and tokens[0][0].upper() == "DO"
    return any(candidate in SQL_BODY_LANGUAGES or candidate in SQL_STRING_PREFIXES for candidate in candidates)


def copies_from_stdin(tokens: list[tuple[str, int]]) -> bool:
    words = [token.upper() for token, depth in tokens if depth == 0]
    return bool(words) and words[0] == "COPY" and any(
        word == "FROM" and following == "STDIN" for word, following in zip(words, words[1:])
    )


def at_line_start(text: str, index: int) -> bool:
    return text[text.rfind("\n", 0, index) + 1 : index].strip() == ""


def line_end_at(text: str, index: int) -> int:
    end = text.find("\n", index)
    return len(text) if end == -1 else end


def supported_line_command(text: str, index: int, buffer_empty: bool) -> int | None:
    if not at_line_start(text, index):
        return None
    command = PSQL_LINE_COMMAND.match(text, index)
    if command is None or command.group(1) not in PSQL_LINE_COMMANDS or "\\" in command.group(2):
        return None
    if command.group(1) in PSQL_CONDITIONAL_COMMANDS and not buffer_empty:
        return None
    return command.end()


def sql_spans_raw(text: str) -> list[Span]:
    spans: list[Span] = []
    statement = Statement([], [])
    paren_depth = 0
    bracket_depth = 0
    in_backtick = False
    index = 0
    length = len(text)
    escape_string_end = -1

    def push(token: str, position: int) -> None:
        if statement.start < 0:
            statement.start = position
        statement.tokens.append((token, paren_depth))

    def finish_statement() -> None:
        nonlocal paren_depth, bracket_depth, in_backtick
        if copies_from_stdin(statement.tokens):
            spans.append(span_at(text, statement.start, line_end_at(text, statement.start), unsupported=True))
        if statement.bodies and scans_routine_body(statement.tokens):
            for body_start, body_end in statement.bodies:
                spans.extend(
                    span_at(text, body_start + inner.start, body_start + inner.end, inner.unsupported)
                    for inner in sql_spans_raw(text[body_start:body_end])
                )
        statement.tokens.clear()
        statement.bodies.clear()
        statement.start = -1
        paren_depth = 0
        bracket_depth = 0
        in_backtick = False

    while index < length:
        char = text[index]
        pair = text[index : index + 2]
        if pair == "--":
            end = line_end_at(text, index)
            spans.append(span_at(text, index, end))
            index = end
        elif pair == "/*":
            end = nested_block_end(text, index, "/*", "*/")
            spans.append(span_at(text, index, end))
            index = end
        elif char == "\\" and bracket_depth == 0 and not in_backtick:
            if (trailing := PSQL_TRAILING_COMMAND.match(text, index)) is not None:
                finish_statement()
                index = trailing.end()
            elif (end := supported_line_command(text, index, not statement.tokens)) is not None:
                index = end
            else:
                end = line_end_at(text, index)
                spans.append(span_at(text, index, end, unsupported=True))
                index = end
        elif char in "'\"":
            escaped = char == "'" and (
                (index > 0 and text[index - 1] in "eE" and (index < 2 or not is_sql_identifier_char(text[index - 2])))
                or (escape_string_end >= 0 and QUOTE_CONTINUATION.fullmatch(text, escape_string_end, index) is not None)
            )
            end = quoted_end(text, index, char, escaped)
            escape_string_end = end if escaped else -1
            push(text[index:end], index)
            index = end
        elif (tag := dollar_tag(text, index)) is not None:
            body_start = index + len(tag)
            close = text.find(tag, body_start)
            body_end = length if close == -1 else close
            if paren_depth == 0 and opens_routine_body(statement.tokens):
                statement.bodies.append((body_start, body_end))
            push("$", index)
            index = length if close == -1 else close + len(tag)
        elif is_sql_identifier_char(char):
            end = index
            while end < length and (is_sql_identifier_char(text[end]) or text[end] == "$"):
                end += 1
            push(text[index:end], index)
            index = end
        elif char == ";" and bracket_depth == 0 and not in_backtick:
            finish_statement()
            index += 1
        else:
            if char == "(":
                paren_depth += 1
            elif char == ")":
                paren_depth = max(0, paren_depth - 1)
            elif char == "[":
                bracket_depth += 1
            elif char == "]":
                bracket_depth = max(0, bracket_depth - 1)
            elif char == "`":
                in_backtick = not in_backtick
            if not char.isspace():
                push(char, index)
            index += 1
    finish_statement()
    return spans


def sql_spans(text: str) -> list[Span]:
    return sorted(
        (span for span in sql_spans_raw(text) if not FRAGMENT_DIRECTIVE.fullmatch(text[span.start : span.end])),
        key=lambda span: span.start,
    )


def tla_spans(text: str) -> list[Span]:
    spans: list[Span] = []
    index = 0
    while index < len(text):
        if text.startswith("\\*", index):
            end = text.find("\n", index)
            end = len(text) if end == -1 else end
            spans.append(span_at(text, index, end))
            index = end
        elif text.startswith("(*", index):
            end = nested_block_end(text, index, "(*", "*)")
            spans.append(span_at(text, index, end))
            index = end
        elif text[index] == '"':
            index = quoted_end(text, index, '"', True)
        else:
            index += 1
    return spans


def lean_raw_string_end(text: str, index: int) -> int | None:
    match = re.compile(r'r(#*)"').match(text, index)
    if match is None or (index > 0 and is_identifier_char(text[index - 1])):
        return None
    closer = '"' + match.group(1)
    close = text.find(closer, match.end())
    return len(text) if close == -1 else close + len(closer)


def lean_char_end(text: str, index: int) -> int | None:
    if index > 0 and (is_identifier_char(text[index - 1]) or text[index - 1] in "'."):
        return None
    match = re.compile(r"'(?:\\(?:x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}|.)|[^\\'\n])'").match(text, index)
    return match.end() if match else None


def lean_spans(text: str) -> list[Span]:
    spans: list[Span] = []
    index = 0
    while index < len(text):
        if text.startswith("--", index):
            end = text.find("\n", index)
            end = len(text) if end == -1 else end
            spans.append(span_at(text, index, end))
            index = end
        elif text.startswith("/-", index):
            end = nested_block_end(text, index, "/-", "-/")
            spans.append(span_at(text, index, end))
            index = end
        elif text[index] == '"':
            index = quoted_end(text, index, '"', True)
        elif text[index] == "r" and (end := lean_raw_string_end(text, index)) is not None:
            index = end
        elif text[index] == "'" and (end := lean_char_end(text, index)) is not None:
            index = end
        else:
            index += 1
    return spans


def unquoted_shell_word(word: str) -> str:
    try:
        return "".join(shlex.split(word))
    except ValueError:
        return word


def reads_python_stdin(command: str) -> bool:
    command = command.replace("\\\n", "")
    executable = PYTHON_COMMAND.match(command)
    if not executable:
        return False
    words = shlex.shlex(command[executable.end():], posix=False, punctuation_chars=";&|<>()")
    words.whitespace_split = True
    for raw in words:
        if raw in {";", "&", "&&", "|", "||", ")"}:
            return True
        word = unquoted_shell_word(raw)
        if word == "-":
            return True
        if word == "--":
            return unquoted_shell_word(next(words, "-")) == "-"
        if word in {"-W", "-X", "--check-hash-based-pycs"}:
            if next(words, None) is None:
                return False
        elif not re.fullmatch(r"-[bBdEiIOPqRsSuvx]+|-[WX].+", word):
            return False
    return True


def python_heredoc_findings(match: dict) -> list[tuple[int, str]]:
    nodes = match["metaVariables"]["single"]
    source = match["text"].encode("utf-8")
    start = match["range"]["byteOffset"]["start"]

    def offset(node: str, edge: str) -> int:
        return nodes[node]["range"]["byteOffset"][edge] - start

    command_end = offset("COMMAND", "end")
    command = source[offset("EXE", "start"):command_end].decode("utf-8")
    before_delimiter = source[command_end:offset("DELIMITER", "start")]
    if re.match(rb"\s*-(?=\s|[<>])", before_delimiter.replace(b"\\\n", b"")):
        command += " -"
    if "AFTER" in nodes:
        command += " " + source[offset("AFTER", "start"):offset("BODY", "start")].decode("utf-8")
    if not reads_python_stdin(command):
        return []

    header_line = nodes["DELIMITER"]["range"]["end"]["line"]
    end = nodes["END"]["range"]["start"]["line"]
    if not nodes["END"]["text"]:
        return [(header_line + 1, "Python heredoc 종료 구분자가 없습니다.")]
    first = nodes["BODY"]["range"]["start"]["line"]
    lines = match["text"].split("\n")
    base = match["range"]["start"]["line"]
    while first > header_line + 1 and not lines[first - 1 - base].strip(" \t"):
        first -= 1
    body = lines[first - base:end - base]
    if before_delimiter.rstrip().endswith(b"<<-"):
        body = [line.lstrip("\t") for line in body]
    program = "".join(line + "\n" for line in body).encode("utf-8")
    try:
        tree = ast.parse(program, filename=match["file"])
        findings = [
            (first + token.start[0], "Python heredoc 주석은 금지됩니다.")
            for token in tokenize.tokenize(io.BytesIO(program).readline)
            if token.type == tokenize.COMMENT
            and not PYTHON_DIRECTIVE.match(token.string)
            and not (token.start == (1, 0) and token.string.startswith("#!"))
        ]
    except SyntaxError as error:
        return [(first + (error.lineno or 1), f"Python heredoc을 해석할 수 없습니다: {error.msg}")]
    except tokenize.TokenError as error:
        message, (line, _) = error.args
        return [(first + line, f"Python heredoc을 해석할 수 없습니다: {message}")]
    for statement in ast.walk(tree):
        if not isinstance(statement, ast.Expr):
            continue
        value = statement.value
        if (isinstance(value, ast.Constant) and isinstance(value.value, (str, bytes))) or (
            isinstance(value, (ast.JoinedStr, getattr(ast, "TemplateStr", ast.JoinedStr)))
            and all(isinstance(part, ast.Constant) for part in value.values)
        ):
            findings.append((first + statement.lineno, "Python heredoc 문자열 단독 문장은 금지됩니다."))
    return sorted(findings)


LANGUAGES: dict[str, tuple[str, Callable[[str], list[Span]]]] = {
    ".sql": ("SQL", sql_spans),
    ".sql.tpl": ("SQL", sql_spans),
    ".tla": ("TLA+", tla_spans),
    ".lean": ("Lean", lean_spans),
}


def language_for(name: str) -> tuple[str, Callable[[str], list[Span]]] | None:
    for suffix in sorted(LANGUAGES, key=len, reverse=True):
        if name.endswith(suffix):
            return LANGUAGES[suffix]
    return None


def git_paths(root: Path, tracked_only: bool) -> list[str]:
    patterns = [f"*{suffix}" for suffix in LANGUAGES]
    staged = subprocess.run(
        ["git", "-C", str(root), "ls-files", "-z", "-s", "--", *patterns], check=True, capture_output=True
    ).stdout.decode()
    names = []
    for entry in staged.split("\0"):
        if not entry:
            continue
        meta, name = entry.split("\t", 1)
        if meta.split(" ", 1)[0] in REGULAR_FILE_MODES:
            names.append(name)
    if not tracked_only:
        others = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--others", "--exclude-standard", "--", *patterns],
            check=True,
            capture_output=True,
        ).stdout.decode()
        names.extend(name for name in others.split("\0") if name and not (root / name).is_symlink())
    return sorted(set(names))


def main() -> int:
    parser = argparse.ArgumentParser(
        description="SQL(PostgreSQL 어휘 기준)·TLA+·Lean 파일과 셸에 포함된 Python의 주석을 찾아 보고합니다. 자동으로 고치지 않습니다."
    )
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--tracked-only", action="store_true")
    parser.add_argument("--heredocs-stdin", action="store_true", help="Bash heredoc의 ast-grep JSON 스트림을 읽습니다.")
    args = parser.parse_args()
    root = args.root.resolve()
    violations = 0
    if args.heredocs_stdin:
        for record in sys.stdin:
            match = json.loads(record)
            for line, message in python_heredoc_findings(match):
                print(f"{match['file']}:{line}: error[no-comments-lexical]: {message}")
                violations += 1
    for name in git_paths(root, args.tracked_only):
        if any(fnmatch.fnmatch(name, pattern) for pattern in EXEMPT_GLOBS):
            continue
        path = root / name
        language = language_for(name)
        if language is None or not path.is_file() or path.resolve() != path:
            continue
        label, find_spans = language
        for span in find_spans(path.read_text(encoding="utf-8")):
            violations += 1
            if span.unsupported:
                print(f"{name}:{span.line}: error[no-comments-lexical]: 검사기가 해석하지 않는 psql 구문입니다. 줄 첫 \\set·\\unset·\\echo·\\if·\\elif·\\else·\\endif·\\quit·\\q·\\i·\\ir·\\include·\\include_relative·\\pset·\\getenv(인자에 \\\\ 없음)와 줄 끝 \\g·\\gx·\\gexec·\\gset만 쓰고, COPY ... FROM STDIN 인라인 데이터는 별도 파일로 옮기세요.")
            else:
                print(f"{name}:{span.line}: error[no-comments-lexical]: {label} 주석은 금지됩니다. 의도는 이름·정의·COVERAGE.md·테스트로 옮기세요.")
    if violations:
        print(f"no-comments-lexical: {violations} finding(s); fix them by hand and rerun", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
