#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ast
import fnmatch
import io
import json
import re
import subprocess
import sys
import tempfile
import tokenize
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

EXEMPT_GLOBS = ("docs/*", "*/vendor/*", "vendor/*")
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
PYTHON_NAME = re.compile(r"python(?:[0-9]+(?:\.[0-9]+)*)?")
PYTHON_VARIABLES = frozenset({"$CI_PYTHON_BIN", "${CI_PYTHON_BIN}"})
DELIMITER_LANGUAGES = {"PY": "python", "PYTHON": "python", "SH": "shell", "BASH": "shell", "REMOTE": "shell"}


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


ANSI_C_ESCAPES = {"a": "\a", "b": "\b", "e": "\x1b", "E": "\x1b", "f": "\f", "n": "\n", "r": "\r", "t": "\t", "v": "\v",
                  "\\": "\\", "'": "'", '"': '"', "?": "?"}
ANSI_C_NUMERIC = re.compile(r"[0-7]{1,3}|x[0-9A-Fa-f]{1,2}|u[0-9A-Fa-f]{1,4}|U[0-9A-Fa-f]{1,8}|c.", re.S)
PLAIN_DELIMITER = re.compile(r"""[A-Za-z_][A-Za-z0-9_]*|'[A-Za-z_][A-Za-z0-9_]*'|"[A-Za-z_][A-Za-z0-9_]*"|\\[A-Za-z_][A-Za-z0-9_]*""")


def ansi_c_text(body: str) -> str:
    out: list[str] = []
    index = 0
    while index < len(body):
        if body[index] != "\\":
            out.append(body[index])
            index += 1
        elif (escape := body[index + 1:index + 2]) in ANSI_C_ESCAPES:
            out.append(ANSI_C_ESCAPES[escape])
            index += 2
        elif number := ANSI_C_NUMERIC.match(body, index + 1):
            token = number[0]
            if token[0] == "c":
                value = ord(token[1]) & 0x1F
            elif token[0] in "xuU":
                value = int(token[1:], 16)
            else:
                value = int(token, 8)
            out.append(chr(value) if value <= 0x10FFFF else "\ufffd")
            index = number.end()
        else:
            out.append(body[index:index + 2])
            index += 2
    return "".join(out)


def unquoted_shell_word(word: str) -> str:
    out: list[str] = []
    index = 0
    while index < len(word):
        char = word[index]
        if char == "\\":
            if word[index + 1:index + 2] != "\n":
                out.append(word[index + 1:index + 2])
            index += 2
        elif word.startswith("$'", index):
            end = ansi_c_quote_end(word, index) or len(word) + 1
            out.append(ansi_c_text(word[index + 2:end - 1]))
            index = end
        elif word.startswith('$"', index):
            index += 1
        elif char == "'":
            end = word.find("'", index + 1)
            end = len(word) if end < 0 else end
            out.append(word[index + 1:end])
            index = end + 1
        elif char == '"':
            index += 1
            while index < len(word) and word[index] != '"':
                if word[index] == "\\" and word[index + 1:index + 2] == "\n":
                    index += 2
                    continue
                if word[index] == "\\" and word[index + 1:index + 2] in ("$", "`", '"', "\\"):
                    index += 1
                out.append(word[index:index + 1])
                index += 1
            index += 1
        else:
            out.append(char)
            index += 1
    return "".join(out)


def is_python(word: str) -> bool:
    return word in PYTHON_VARIABLES or PYTHON_NAME.fullmatch(word.rsplit("/", 1)[-1]) is not None


def uncertain(word: str) -> bool:
    return isinstance(word, Dynamic) or "$" in word or "`" in word


def python_reads_stdin(arguments: list[str]) -> bool | None:
    index = 0
    inspect = False
    while index < len(arguments):
        word = arguments[index]
        if uncertain(word):
            return None
        if word == "-":
            return True
        if word == "--":
            following = arguments[index + 1:index + 2]
            if following and uncertain(following[0]):
                return None
            return inspect or following in ([], ["-"]) or following[0] in STDIN_PATHS
        if word == "--check-hash-based-pycs":
            if any(uncertain(value) for value in arguments[index + 1:index + 2]):
                return None
            index += 2
            continue
        if not word.startswith("-"):
            return inspect or word in STDIN_PATHS
        if word.startswith("--"):
            return None
        for position, flag in enumerate(word[1:], 1):
            inspect |= flag == "i"
            if flag in "cm":
                return inspect
            if flag in "WX":
                if position == len(word) - 1:
                    index += 1
                    if any(uncertain(value) for value in arguments[index:index + 1]):
                        return None
                break
            if flag not in "bBdEiIOPqRsSuvx":
                return None
        index += 1
    return True


def python_language(arguments: list[str]) -> str | None:
    reads = python_reads_stdin(arguments)
    return None if reads is None else "python" if reads else "data"


HEREDOC_RULE = """id: heredoc
language: Bash
ignores: ['docs/**', '**/vendor/**']
rule:
  kind: heredoc_redirect
  inside: {kind: redirected_statement, pattern: $STMT}
  has: {kind: heredoc_start, pattern: $DELIMITER}
---
id: heredoc-unparsed
language: Bash
ignores: ['docs/**', '**/vendor/**']
rule:
  pattern: $DELIMITER
  any:
    - {kind: heredoc_start, inside: {kind: ERROR, stopBy: end}}
    - {kind: ERROR, regex: '<'}
"""
SHELLS = frozenset({"sh", "bash", "dash", "zsh", "ksh", "mksh", "ash"})
PREFIX_OPTIONS = {"exec": ("a", frozenset()), "command": ("", frozenset()), "builtin": ("", frozenset()), "nohup": ("", frozenset()),
                  "time": ("fo", frozenset({"--format", "--output"})), "nice": ("n", frozenset({"--adjustment"})),
                  "!": ("", frozenset())}
COMMAND_RUNNERS = frozenset({"sudo", "doas", "env", "nsenter", "chroot", "setpriv", "flock", "timeout", "systemd-run",
                             "busybox", "unshare", "uv", "stdbuf", "ionice", "setsid"})
SSH_OPTIONS_WITH_VALUE = frozenset("BbcDEeFIiJLlmOoPpQRSWw")
PIPELINE_BREAKS = frozenset({";", "&", "&&", "||", ";;", "(", ")"})
REDIRECTION = re.compile(r"[0-9]*(?:[<>]&?|>>|&>>?|<>|<<-?|<<<)-?")
SHEBANG_SHELL = re.compile(r"#!\s*(?:\S*/)?(?:env\s+(?:-\S+\s+)*)?(?:" + "|".join(sorted(SHELLS)) + r")(?:\s|$)")
SHEBANG_PYTHON = re.compile(r"#!\s*(?:\S*/)?(?:env\s+(?:-\S+\s+)*)?python[0-9.]*(?:\s|$)")
MAX_HEREDOC_DEPTH = 8
COMPOUND_ENDS = frozenset({"}", "done", "fi", "esac"})
RESERVED_PREFIXES = frozenset({"if", "then", "elif", "else", "do", "while", "until", "{"})


@dataclass
class Origin:
    file: str
    delta: int
    pinned: int | None = None

    def line(self, offset: int) -> int:
        return self.pinned if self.pinned is not None else self.delta + offset


def substitution_end(text: str, start: int) -> int | None:
    if text[start] == "`":
        index = start + 1
        while index < len(text):
            if text[index] == "\\":
                index += 2
            elif text[index] == "`":
                return index + 1
            else:
                index += 1
        return None
    depth, index = 0, start + 1
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == "'":
            end = text.find("'", index + 1)
            if end < 0:
                return None
            index = end + 1
            continue
        if char == '"':
            end = double_quote_end(text, index)
            if end is None:
                return None
            index = end
            continue
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if not depth:
                return index + 1
        index += 1
    return None


def double_quote_end(text: str, start: int) -> int | None:
    index = start + 1
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
        elif char == '"':
            return index + 1
        elif char == "`" or text.startswith("$(", index):
            end = substitution_end(text, index)
            if end is None:
                return None
            index = end
        else:
            index += 1
    return None


def ansi_c_quote_end(text: str, start: int) -> int | None:
    index = start + 2
    while index < len(text):
        if text[index] == "\\":
            index += 2
        elif text[index] == "'":
            return index + 1
        else:
            index += 1
    return None


def brace_end(text: str, start: int) -> int | None:
    depth, index = 0, start + 1
    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == "'":
            end = text.find("'", index + 1)
            if end < 0:
                return None
            index = end + 1
            continue
        if char == '"':
            end = double_quote_end(text, index)
            if end is None:
                return None
            index = end
            continue
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if not depth:
                return index + 1
        index += 1
    return None


SHELL_OPERATOR = re.compile(r"<<<|<<-|<<|<>|<&|>&|>>|>\||&>>|&>|;;|&&|\|\||\|&|[<>;&|()]")


class Dynamic(str):
    pass


def has_unquoted_pattern(raw: str) -> bool:
    index = 0
    while index < len(raw):
        char = raw[index]
        if char == "\\":
            index += 2
        elif char == "'":
            index = (raw.find("'", index + 1) + 1) or len(raw)
        elif char == '"':
            index = double_quote_end(raw, index) or len(raw)
        elif raw.startswith("$'", index):
            index = ansi_c_quote_end(raw, index) or len(raw)
        elif raw.startswith("${", index):
            index = brace_end(raw, index) or len(raw)
        elif char in "*?[{":
            return True
        else:
            index += 1
    return False


def arithmetic_expansion(group: str) -> bool:
    if not (group.startswith("$((") and group.endswith("))")):
        return False
    depth = 0
    for char in group[3:-2]:
        depth += {"(": 1, ")": -1}.get(char, 0)
        if depth < 0:
            return False
    return depth == 0


def outer_expansion(text: str) -> bool:
    index = 0
    while index < len(text):
        if text[index] == "\\":
            index += 2
        elif text.startswith("$((", index) and arithmetic_expansion(text[index:substitution_end(text, index) or len(text)]):
            index = substitution_end(text, index) or len(text)
        elif text[index] in "$`":
            return True
        else:
            index += 1
    return False


def code_or_unknown(language: str | None, expanded: bool) -> str | None:
    return language if not expanded or language in {"shell", "python"} else None


def has_active_substitution(text: str) -> bool:
    index = 0
    while index < len(text):
        if text[index] == "\\":
            index += 2
        elif text.startswith("$((", index) and arithmetic_expansion(text[index:substitution_end(text, index) or len(text)]):
            index += 3
        elif text.startswith("$(", index) or text[index] == "`":
            return True
        else:
            index += 1
    return False


def shell_tokens(text: str) -> list[tuple[str, str]]:
    tokens: list[tuple[str, str]] = []
    word: list[str] = []

    substituted = False

    def flush() -> None:
        nonlocal substituted
        if word:
            tokens.append(("word", "".join(word)))
            word.clear()
        if substituted:
            tokens.append(("substitution", "$("))
            substituted = False

    index = 0
    while index < len(text):
        char = text[index]
        if char in " \t":
            flush()
            index += 1
        elif char == "\n":
            flush()
            tokens.append(("op", ";"))
            index += 1
        elif char == "\\":
            if text[index + 1:index + 2] != "\n":
                word.append(text[index:index + 2])
            index += 2
        elif char == "#" and not word:
            end = text.find("\n", index)
            index = len(text) if end < 0 else end
        elif char == "'" or char == '"' or text.startswith("$'", index):
            end = ansi_c_quote_end(text, index) if char == "$" else (
                text.find("'", index + 1) + 1 if char == "'" else double_quote_end(text, index))
            end = end or len(text)
            word.append(text[index:end])
            substituted |= char == '"' and has_active_substitution(text[index:end])
            index = end
        elif text.startswith("${", index):
            end = brace_end(text, index) or len(text)
            word.append(text[index:end])
            substituted |= has_active_substitution(text[index:end])
            index = end
        elif text.startswith(("<(", ">("), index):
            flush()
            tokens.append(("process", text[index:index + 2]))
            index += 2
        elif char == "`" or text.startswith("$(", index):
            end = substitution_end(text, index)
            if end is None:
                flush()
                opener = "`" if char == "`" else "$("
                tokens.append(("open", opener))
                index += len(opener)
            else:
                word.append(text[index:end])
                arithmetic = arithmetic_expansion(text[index:end])
                substituted |= not arithmetic or has_active_substitution(text[index + 3:end - 2])
                index = end
        elif operator := SHELL_OPERATOR.match(text, index):
            flush()
            tokens.append(("op", operator[0]))
            index = operator.end()
        else:
            word.append(char)
            index += 1
    flush()
    return tokens


def command_words(tokens: list[tuple[str, str]]) -> list[str]:
    words: list[str] = []
    skip = False
    for position, (kind, value) in enumerate(tokens):
        if skip:
            skip = False
        elif kind == "op" and value[0] in "<>&" and value not in {"&", "&&"}:
            skip = True
        elif kind == "word" and value.isdigit() and tokens[position + 1:position + 2][:1] and tokens[position + 1][1][0] in "<>":
            continue
        elif kind == "word":
            word = unquoted_shell_word(value)
            dynamic = tokens[position + 1:position + 2] == [("substitution", "$(")] or has_unquoted_pattern(value)
            words.append(Dynamic(word) if dynamic else word)
    return words


STARTUP_VARIABLE = re.compile(r"(?:^-e|^|[^A-Za-z0-9_])(?:PYTHONINSPECT|PYTHONSTARTUP|BASH_ENV|ENV)=")
STDIN_PATHS = frozenset({"/dev/stdin", "/dev/fd/0", "/proc/self/fd/0"})
DATA_COMMANDS = frozenset({"nft", "psql", "${psql[@]}", "sql", "write_report"})
STREAM_COMMANDS = frozenset({"cat"})
COMMAND_BREAKS = frozenset({";", "&", "&&", "||", ";;", "|", "|&", "(", ")"})
CONTAINER_EXEC_FLAGS = frozenset({"--interactive", "--tty", "--privileged", "--detach"})
CONTAINER_EXEC_VALUES = frozenset({"--env", "--env-file", "--user", "--workdir", "--detach-keys"})
CONTAINER_EXEC_SHORT_FLAGS = frozenset("itd")
CONTAINER_EXEC_SHORT_VALUES = frozenset("euw")


def script_language(text: str) -> str | None:
    stages: list[list[tuple[str, str]]] = [[]]
    for token in shell_tokens(text):
        if token in {("op", "|"), ("op", "|&")}:
            stages.append([])
        elif token[0] in {"open", "process", "substitution"} or (token[0] == "op" and token[1] in COMMAND_BREAKS):
            return None
        else:
            stages[-1].append(token)
    language = None
    for position, tokens in enumerate(stages):
        language = stage_language(tokens)
        if language != "stream" or position == len(stages) - 1:
            break
    return language


def container_exec_language(arguments: list[str]) -> str | None:
    if arguments[:1] != ["exec"]:
        return None
    index = 1
    while index < len(arguments) and arguments[index].startswith("-") and arguments[index] != "--":
        option = arguments[index]
        index += 1
        if option.startswith("--"):
            name = option.split("=", 1)[0]
            if name in CONTAINER_EXEC_VALUES:
                index += "=" not in option
            elif name not in CONTAINER_EXEC_FLAGS:
                return None
        else:
            for position, flag in enumerate(option[1:], 1):
                if flag in CONTAINER_EXEC_SHORT_VALUES:
                    index += position == len(option) - 1
                    break
                if flag not in CONTAINER_EXEC_SHORT_FLAGS:
                    return None
    index += index < len(arguments) and arguments[index] == "--"
    return command_language(arguments[index + 1:]) if index + 1 < len(arguments) else None


def ssh_receiver(arguments: list[str]) -> str | None:
    index = 0
    while index < len(arguments) and arguments[index].startswith("-") and arguments[index] != "--":
        cluster = arguments[index][1:]
        index += 1
        for position, option in enumerate(cluster):
            if option in SSH_OPTIONS_WITH_VALUE:
                index += position == len(cluster) - 1
                break
    if index < len(arguments) and arguments[index] == "--":
        index += 1
    remote = arguments[index + 1:]
    if not remote:
        return "shell"
    if any(isinstance(word, Dynamic) for word in remote):
        return None
    return code_or_unknown(script_language(" ".join(remote)), any(outer_expansion(word) for word in remote))


def shell_stdin_language(arguments: list[str]) -> str | None:
    index = 0
    reads_stdin = False
    command_string = False
    while index < len(arguments):
        option = arguments[index]
        if option == "-":
            return "shell"
        if option == "--":
            index += 1
            break
        if option in {"--rcfile", "--init-file"}:
            index += 2
            continue
        if option.startswith("--"):
            index += 1
            continue
        if option[:1] not in {"-", "+"} or len(option) == 1:
            break
        values = 0
        for flag in option[1:]:
            command_string |= flag == "c"
            reads_stdin |= flag == "s"
            values += flag in "oO"
        index += 1 + values
    if command_string:
        if index >= len(arguments) or isinstance(arguments[index], Dynamic):
            return None
        return code_or_unknown(script_language(arguments[index]), outer_expansion(arguments[index]))
    if reads_stdin or index >= len(arguments):
        return "shell"
    return command_language(arguments[index:])


def is_receiver(word: str) -> bool:
    name = word.rsplit("/", 1)[-1]
    return name in SHELLS or name == "python-runner.sh" or is_python(word)


def command_language(words: list[str]) -> str | None:
    if not words:
        return None
    name = words[0].rsplit("/", 1)[-1]
    if is_python(words[0]):
        return python_language(words[1:])
    if name == "python-runner.sh" and words[1:2] == ["--"]:
        return python_language(words[2:])
    if name in STREAM_COMMANDS:
        return "stream"
    if name in DATA_COMMANDS or words[0] in DATA_COMMANDS:
        return "data"
    if name in SHELLS:
        return shell_stdin_language(words[1:])
    if name == "ssh":
        return ssh_receiver(words[1:])
    if name in {"docker", "podman"}:
        return container_exec_language(words[1:])
    if name in COMMAND_RUNNERS:
        for position in range(1, len(words)):
            word = words[position]
            if is_receiver(word) or word.rsplit("/", 1)[-1] in DATA_COMMANDS | STREAM_COMMANDS:
                before = words[position - 1]
                if before.startswith("-") and before != "--" and "=" not in before and position > 1:
                    return None
                language = command_language(words[position:])
                if language in {"data", "stream"} and any(is_receiver(later) for later in words[position + 1:]):
                    return None
                return language
    return None


def stage_language(tokens: list[tuple[str, str]]) -> str | None:
    words = command_words(tokens)
    index = 0
    while index < len(words):
        word = words[index]
        if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=.*", word, re.S):
            index += 1
        elif word in RESERVED_PREFIXES:
            index += 1
        elif word in PREFIX_OPTIONS:
            short, long = PREFIX_OPTIONS[word]
            index += 1
            while index < len(words) and words[index].startswith("-") and words[index] not in {"-", "--"}:
                option = words[index]
                index += 1
                if option.startswith("--"):
                    index += "=" not in option and option in long
                    continue
                for position, flag in enumerate(option[1:], 1):
                    if flag in short:
                        index += position == len(option) - 1
                        break
            index += index < len(words) and words[index] == "--"
        else:
            break
    language = command_language(words[index:])
    if language in {"data", "stream"} and any(STARTUP_VARIABLE.search(word) for word in words):
        return None
    return language


def current_command(prefix: str) -> list[tuple[str, str]]:
    contexts: list[list[tuple[str, str]]] = [[]]
    for token in shell_tokens(prefix):
        kind, value = token
        if kind == "open" or token == ("op", "("):
            contexts.append([])
        elif token == ("op", ")"):
            if len(contexts) > 1:
                contexts.pop()
            contexts[-1] = []
        elif kind == "op" and (value in PIPELINE_BREAKS or value in {"|", "|&"}):
            contexts[-1] = []
        else:
            contexts[-1].append(token)
    return contexts[-1]


def following_stages(tail: str) -> list[list[tuple[str, str]]] | None:
    stages: list[list[tuple[str, str]]] = [[]]
    tokens = shell_tokens(tail)
    if any(token in {("op", ")"), ("word", "}")} and any(later in {("op", "|"), ("op", "|&")} for later in tokens[position + 1:])
           for position, token in enumerate(tokens)):
        return None
    for token in tokens:
        kind, value = token
        if kind == "open" or token == ("op", ")"):
            break
        if token in {("op", "|"), ("op", "|&")}:
            stages.append([])
        elif kind == "op" and value in PIPELINE_BREAKS:
            break
        else:
            stages[-1].append(token)
    return stages


def has_later_heredoc(tail: str) -> bool:
    return any(token in {("op", "<<"), ("op", "<<-")} for token in shell_tokens(tail))


def unsupported_header(line: str, prefix: str, tail: str) -> str | None:
    tail_tokens = shell_tokens(tail)
    if any(kind == "process" for kind, _ in shell_tokens(line)):
        return "heredoc 명령 줄에 프로세스 치환 <(…)·>(…)를 쓰지 마세요."
    if tail_tokens and tail_tokens[-1] in {("op", "|"), ("op", "|&")}:
        return "heredoc 헤더 줄을 파이프로 끝내지 마세요. 파이프라인 전체를 한 줄에 쓰세요."
    for before, token in zip(tail_tokens, tail_tokens[1:]):
        if before in {("op", "|"), ("op", "|&")} and (token[0] == "open" or token in {("op", "("), ("word", "{")}):
            return "heredoc 헤더 줄의 파이프 뒤에는 하위 셸·그룹 대신 명령을 직접 쓰세요."
    words = command_words(current_command(prefix))
    if not words or words[0] in COMPOUND_ENDS:
        return "복합 명령·하위 셸에 heredoc을 붙이지 마세요. 본문을 받는 명령에 직접 붙이세요."
    return None


def heredoc_language(prefix: str, tail: str, first_line: str, delimiter: str) -> str | None:
    stages_after = following_stages(tail)
    if stages_after is None:
        return None
    head, *rest = stages_after
    if any(token == ("substitution", "$(") and position >= 2 and head[position - 2][0] == "op"
           and head[position - 2][1][0] in "<>&" for position, token in enumerate(head)):
        return None
    stages = [current_command(prefix) + head, *rest]
    startup = any(kind == "word" and STARTUP_VARIABLE.search(value) for tokens in stages for kind, value in tokens)
    language = None
    for position, tokens in enumerate(stages):
        if position and ("substitution", "$(") in tokens:
            return None
        language = stage_language(tokens)
        if language != "stream" or position == len(stages) - 1:
            break
    if startup and language not in {"python", "shell"}:
        return None
    if language in {"python", "shell", "data"}:
        return language
    if SHEBANG_PYTHON.match(first_line):
        return "python"
    if SHEBANG_SHELL.match(first_line):
        return "shell"
    return DELIMITER_LANGUAGES.get(delimiter) or ("data" if language in {"data", "stream"} else None)


def python_findings(file: str, first: int, body: list[str]) -> list[tuple[int, str]]:
    program = "".join(line + "\n" for line in body).encode("utf-8")
    try:
        tree = ast.parse(program, filename=file)
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


def heredoc_scan(ast_grep: str, config: str, root: Path, paths: list[str], rule: str) -> list[dict]:
    matches: list[dict] = []
    for start in range(0, len(paths), 256):
        result = subprocess.run(
            [ast_grep, "scan", "--config", config, *rule.split("\0"), "--json=stream", "--", *paths[start:start + 256]],
            cwd=root, capture_output=True, text=True,
        )
        if result.returncode not in (0, 1) or (result.returncode == 1 and "Scan succeeded" not in result.stderr):
            raise RuntimeError(f"ast-grep heredoc scan failed: {result.stderr.strip()}")
        matches.extend(json.loads(line) for line in result.stdout.splitlines() if line.strip())
    return matches


RUN_BLOCK_RULE = """id: run-block
language: Yaml
rule:
  kind: block_mapping_pair
  all:
    - has: {field: key, regex: '^["'']?run["'']?$'}
    - has: {field: value, kind: block_node, has: {kind: block_scalar}, pattern: $BODY}
  not:
    inside:
      kind: block_mapping
      has:
        kind: block_mapping_pair
        all:
          - has: {field: key, regex: '^["'']?shell["'']?$'}
          - has: {field: value, not: {regex: '^(?:[*]|(?:[&!][^ ]*[[:space:]]+)*(?:[|>][-+0-9]*[[:space:]]+)?["'']?(?:/usr/bin/|/bin/)?(bash|sh)([^A-Za-z0-9_-]|$))'}}
---
id: run-flow
language: Yaml
rule:
  kind: block_mapping_pair
  all:
    - has: {field: key, regex: '^["'']?run["'']?$'}
    - has: {field: value, kind: flow_node, has: {any: [{kind: double_quote_scalar}, {kind: single_quote_scalar}], pattern: $BODY}}
  not:
    inside:
      kind: block_mapping
      has:
        kind: block_mapping_pair
        all:
          - has: {field: key, regex: '^["'']?shell["'']?$'}
          - has: {field: value, not: {regex: '^(?:[*]|(?:[&!][^ ]*[[:space:]]+)*(?:[|>][-+0-9]*[[:space:]]+)?["'']?(?:/usr/bin/|/bin/)?(bash|sh)([^A-Za-z0-9_-]|$))'}}
---
id: run-flow-pair
language: Yaml
rule:
  kind: flow_pair
  all:
    - has: {field: key, regex: '^["'']?run["'']?$'}
    - has: {field: value, kind: flow_node, has: {any: [{kind: double_quote_scalar}, {kind: single_quote_scalar}], pattern: $BODY}}
  not:
    inside:
      kind: flow_mapping
      has:
        kind: flow_pair
        all:
          - has: {field: key, regex: '^["'']?shell["'']?$'}
          - has: {field: value, not: {regex: '^(?:[*]|(?:[&!][^ ]*[[:space:]]+)*(?:[|>][-+0-9]*[[:space:]]+)?["'']?(?:/usr/bin/|/bin/)?(bash|sh)([^A-Za-z0-9_-]|$))'}}
---
id: run-alias
language: Yaml
rule:
  any: [{kind: block_mapping_pair}, {kind: flow_pair}]
  all:
    - has: {field: key, regex: '^["'']?run["'']?$'}
    - has: {field: value, has: {kind: alias, pattern: $BODY}}
"""


def github_expressions_masked(text: str) -> str:
    return re.sub(r"\$\{\{.*?\}\}", lambda expression: "x" * len(expression[0]), text)


YAML_ESCAPES = {"0": "\0", "a": "\a", "b": "\b", "t": "\t", "\t": "\t", "n": "\n", "v": "\v", "f": "\f",
                "r": "\r", "e": "\x1b", " ": " ", '"': '"', "/": "/", "\\": "\\", "N": "\x85", "_": "\xa0",
                "L": "\u2028", "P": "\u2029"}
YAML_HEX_ESCAPES = {"x": 2, "u": 4, "U": 8}


def flow_breaks(inner: str, index: int) -> tuple[list[str], int]:
    found = []
    while True:
        end = index
        while end < len(inner) and inner[end] in " \t":
            end += 1
        if end < len(inner) and inner[end] == "\n":
            found.append("\n")
            index = end + 1
        else:
            return found, end


def flow_scalar(text: str) -> str | None:
    if len(text) < 2 or text[0] not in "\"'" or text[-1] != text[0]:
        return None
    quote, inner = text[0], text[1:-1].replace("\r\n", "\n").replace("\r", "\n")
    decoded: list[str] = []
    index = 0
    while index < len(inner):
        char = inner[index]
        if char in " \t\n":
            end = index
            while end < len(inner) and inner[end] in " \t":
                end += 1
            if end < len(inner) and inner[end] == "\n":
                breaks, index = flow_breaks(inner, end + 1)
                decoded.append("".join(breaks) if breaks else " ")
            else:
                decoded.append(inner[index:end])
                index = end
        elif quote == "'" and inner.startswith("''", index):
            decoded.append("'")
            index += 2
        elif quote == '"' and char == "\\":
            escape = inner[index + 1:index + 2]
            if escape == "\n":
                breaks, index = flow_breaks(inner, index + 2)
                decoded.extend(breaks)
            elif escape in YAML_ESCAPES:
                decoded.append(YAML_ESCAPES[escape])
                index += 2
            elif escape in YAML_HEX_ESCAPES:
                digits = inner[index + 2:index + 2 + YAML_HEX_ESCAPES[escape]]
                if not re.fullmatch(r"[0-9A-Fa-f]{%d}" % YAML_HEX_ESCAPES[escape], digits):
                    return None
                decoded.append(chr(int(digits, 16)))
                index += 2 + len(digits)
            else:
                return None
        else:
            decoded.append(char)
            index += 1
    return "".join(decoded)


def folded_block(lines: list[str]) -> str:
    chunks: list[str] = []
    breaks: list[str] = []
    previous: str | None = None
    for line in lines:
        if not line:
            breaks.append("\n")
            continue
        if previous is None:
            chunks.extend(breaks)
        elif previous[0] not in " \t" and line[0] not in " \t":
            chunks.append("".join(breaks) or " ")
        else:
            chunks.append("\n" + "".join(breaks))
        breaks = []
        chunks.append(line)
        previous = line
    return "".join(chunks)


def workflow_run_bodies(ast_grep: str, config: str, root: Path, paths: list[str], scratch: str,
                        origins: dict[str, Origin], findings: list[tuple[str, int, str]]) -> list[str]:
    candidates = [path for path in paths if not any(fnmatch.fnmatch(path, pattern) for pattern in EXEMPT_GLOBS)
                  and re.fullmatch(r"(?:.*/)?(?:\.github/workflows/[^/]+|action)\.ya?ml", path)]
    bodies = []
    for match in heredoc_scan(ast_grep, config, root, candidates, "--inline-rules\0" + RUN_BLOCK_RULE) if candidates else []:
        node = match["metaVariables"]["single"]["BODY"]
        if match["ruleId"] == "run-alias":
            findings.append((match["file"], node["range"]["start"]["line"] + 1, "workflow run 값에 별칭(*)을 쓸 수 없습니다. 본문을 직접 쓰세요."))
            continue
        if match["ruleId"] in {"run-flow", "run-flow-pair"}:
            value = flow_scalar(node["text"])
            if value is None:
                findings.append((match["file"], node["range"]["start"]["line"] + 1, "workflow run 값을 YAML 규칙으로 해석할 수 없습니다. 블록(run: |)으로 쓰세요."))
                continue
            body = github_expressions_masked(value)
            name = f"run-{len(origins)}.sh"
            Path(scratch, name).write_text(":\n" + body + "\n", encoding="utf-8")
            start = node["range"]["start"]["line"]
            origins[str(Path(scratch, name))] = Origin(match["file"], start - 1, start + 1)
            bodies.append(str(Path(scratch, name)))
            continue
        header, *rest = node["text"].split("\n")
        lines = [github_expressions_masked(line.removesuffix("\r")) for line in rest]
        indent = min((len(line) - len(line.lstrip(" ")) for line in lines if line.strip()), default=0)
        name = f"run-{len(origins)}.sh"
        start = node["range"]["start"]["line"]
        if re.search(r"(?:^|\s)>", header):
            Path(scratch, name).write_text(":\n" + folded_block([line[indent:] for line in lines]) + "\n", encoding="utf-8")
            origins[str(Path(scratch, name))] = Origin(match["file"], start - 1, start + 1)
        else:
            Path(scratch, name).write_text(":\n" + "".join(line[indent:] + "\n" for line in lines), encoding="utf-8")
            origins[str(Path(scratch, name))] = Origin(match["file"], start)
        bodies.append(str(Path(scratch, name)))
    return bodies


def heredoc_violations(ast_grep: str, config: str, root: Path, paths: list[str]) -> list[tuple[str, int, str]]:
    findings: list[tuple[str, int, str]] = []
    origins = {path: Origin(path, 0) for path in paths}
    sources: dict[str, bytes] = {}
    with tempfile.TemporaryDirectory(prefix="heredoc-bodies.") as scratch:
        runs = workflow_run_bodies(ast_grep, config, root, paths, scratch, origins, findings)
        for match in heredoc_scan(ast_grep, config, root, runs, "--filter\0^no-comments-bash$") if runs else []:
            origin = origins[match["file"]]
            findings.append((origin.file, origin.line(match["range"]["start"]["line"] + 1), "workflow run 셸 주석은 금지됩니다."))
        pending = paths + runs
        for depth in range(MAX_HEREDOC_DEPTH + 1):
            if not pending:
                break
            bodies: list[str] = []
            matches = heredoc_scan(ast_grep, config, root, pending, "--inline-rules\0" + HEREDOC_RULE)
            matches.sort(key=lambda match: (match["file"], match["metaVariables"]["single"]["DELIMITER"]["range"]["byteOffset"]["start"]))
            for match in matches:
                origin = origins[match["file"]]
                if match["file"] not in sources:
                    sources[match["file"]] = (root / match["file"]).read_bytes()
                source = sources[match["file"]]
                nodes = match["metaVariables"]["single"]

                def offset(node: str, edge: str) -> int:
                    return nodes[node]["range"]["byteOffset"][edge]

                header_line = nodes["DELIMITER"]["range"]["end"]["line"]
                report_line = origin.line(header_line + 1)
                if match.get("ruleId") == "heredoc-unparsed":
                    findings.append((origin.file, report_line, "heredoc을 해석할 수 없습니다. 본문과 종료 구분자 줄을 갖춘 << 형식으로 쓰세요."))
                    continue
                raw = nodes["DELIMITER"]["text"]
                if not PLAIN_DELIMITER.fullmatch(raw):
                    findings.append((origin.file, report_line, "heredoc 구분자는 EOF·'EOF'·\"EOF\"·\\EOF처럼 이름 전체를 한 방식으로만 인용하세요."))
                    continue
                start = match["range"]["byteOffset"]["start"]
                operator = source[start:offset("DELIMITER", "start")].strip()
                line_start = source.rfind(b"\n", 0, start) + 1
                if re.match(rb"[0-9]+<<", operator) or re.search(rb"(?:^|[\s;&|(])[0-9]+$", source[line_start:start]):
                    findings.append((origin.file, report_line, "0<< 같은 파일 기술자 지정 heredoc은 구문 분석기가 본문 경계를 잘못 읽으므로 <<를 쓰세요."))
                    continue
                if depth == MAX_HEREDOC_DEPTH:
                    findings.append((origin.file, report_line, "heredoc 중첩이 너무 깊어 검사할 수 없습니다."))
                    continue
                lines = source.decode("utf-8").split("\n")
                prefix = source[offset("STMT", "start"):start].decode("utf-8")
                line_end = source.find(b"\n", offset("DELIMITER", "end"))
                tail = source[offset("DELIMITER", "end"):line_end if line_end >= 0 else len(source)].decode("utf-8")
                if re.search(r"(?<!\\)(?:\\\\)*\\$", tail):
                    findings.append((origin.file, report_line, "heredoc 헤더 줄을 역슬래시로 잇지 마세요. 파이프라인 전체를 한 줄에 쓰세요."))
                    continue
                header_end = source.find(b"\n", start)
                physical = source[line_start:header_end if header_end >= 0 else len(source)].decode("utf-8")
                if message := unsupported_header(physical, prefix, tail):
                    findings.append((origin.file, report_line, message))
                    continue
                if has_later_heredoc(tail):
                    findings.append((origin.file, report_line, "한 줄에 heredoc을 여러 개 쓰지 마세요. 각 heredoc을 별도 명령으로 나누세요."))
                    continue
                strip_tabs = operator.endswith(b"<<-")
                word = unquoted_shell_word(raw)
                end = next((index for index in range(header_line + 1, len(lines))
                            if (lines[index].lstrip("\t") if strip_tabs else lines[index]) == word), None)
                if end is None:
                    findings.append((origin.file, report_line, "heredoc 종료 구분자 줄을 찾을 수 없습니다. 구분자만 있는 줄로 끝내세요."))
                    continue
                first = header_line + 1
                body = lines[first:end]
                if strip_tabs:
                    body = [line.lstrip("\t") for line in body]
                language = heredoc_language(prefix, tail, body[0] if body else "", word)
                if language is None:
                    findings.append((origin.file, report_line, "heredoc을 받는 명령을 판정할 수 없습니다. 셸·Python이나 허용된 데이터 명령(cat, psql 등)에 직접 붙이세요."))
                elif language == "python" and not re.search(r"['\"\\\\]", raw) and any(re.search(r"[$`\\\\]", line) for line in body):
                    findings.append((origin.file, report_line, "따옴표 없는 구분자의 Python heredoc 본문에 셸 확장 문자($, `, \\)가 있습니다. 구분자를 인용하고 값은 인자로 넘기세요."))
                elif language == "python":
                    findings.extend((origin.file, origin.line(line), message) for line, message in python_findings(origin.file, first, body))
                elif language == "shell":
                    leading = next((index for index, line in enumerate(body) if line.strip()), None)
                    if leading is not None and body[leading].lstrip().startswith("#!") and (leading or body[0].startswith((" ", "\t"))):
                        findings.append((origin.file, origin.line(first + leading + 1), "셸 heredoc 주석은 금지됩니다."))
                    name = f"body-{len(origins)}.sh"
                    Path(scratch, name).write_text("".join(line + "\n" for line in body), encoding="utf-8")
                    origins[str(Path(scratch, name))] = Origin(origin.file, origin.delta + first, origin.pinned)
                    bodies.append(str(Path(scratch, name)))
            for match in heredoc_scan(ast_grep, config, root, bodies, "--filter\0^no-comments-bash$") if bodies else []:
                origin = origins[match["file"]]
                findings.append((origin.file, origin.line(match["range"]["start"]["line"] + 1), "셸 heredoc 주석은 금지됩니다."))
            pending = bodies
    return sorted(set(findings))


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
        description="SQL(PostgreSQL 어휘 기준)·TLA+·Lean 파일과 셸·Python을 수신 명령으로 하는 Bash heredoc 본문의 주석을 찾아 보고합니다. 자동으로 고치지 않습니다."
    )
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--tracked-only", action="store_true")
    parser.add_argument("--heredocs-from", type=Path, help="Bash heredoc을 찾을 파일 목록(NUL 구분, --root 기준 상대 경로)")
    parser.add_argument("--ast-grep", help="heredoc 검사에 쓸 ast-grep 실행 파일")
    parser.add_argument("--config", help="heredoc 검사에 쓸 ast-grep sgconfig.yml")
    args = parser.parse_args()
    root = args.root.resolve()
    violations = 0
    if args.heredocs_from:
        if not args.ast_grep or not args.config:
            parser.error("--heredocs-from requires --ast-grep and --config")
        paths = [name for name in args.heredocs_from.read_bytes().decode("utf-8").split("\0") if name]
        for name, line, message in heredoc_violations(args.ast_grep, args.config, root, paths):
            print(f"{name}:{line}: error[no-comments-lexical]: {message}")
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
