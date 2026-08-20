#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

python3 - <<'PY'
from __future__ import annotations

from pathlib import Path
import re
import sys
from collections import Counter, defaultdict

ROOT = Path('.')
PROD_FILE_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_FILE_LIMIT', '400'))
TEST_FILE_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_TEST_FILE_LIMIT', '400'))
FUNC_FILE_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_FUNCS_PER_FILE_LIMIT', '40'))
METHOD_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_METHODS_PER_TYPE_LIMIT', '40'))
STRUCT_FIELD_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_STRUCT_FIELDS_LIMIT', '45'))
FUNC_LINES_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_FUNC_LINES_LIMIT', '180'))
AGGREGATE_REACHABLE_FIELDS_MAX = int(__import__('os').environ.get('GO_RESPONSIBILITY_AGGREGATE_FIELDS_LIMIT', '90'))

STRUCT_FIELD_ALLOWLIST: set[tuple[str, str]] = set()

BASELINE_PATH = Path('scripts/responsibility-baseline.txt')

SKIP_PARTS = {'.git', '.claude', '.worktrees', 'vendor', 'bin', 'artifacts', 'logs', '.tmp'}
TOP_DECL_RE = re.compile(r'^(func|type|const|var)\b')
FUNC_RE = re.compile(r'^func\s+')
FUNC_NAME_RE = re.compile(r'^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)')
METHOD_RE = re.compile(r'^func\s+\(([^)]*)\)\s+([A-Za-z_][A-Za-z0-9_]*)')
STRUCT_RE = re.compile(r'^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+struct\s*{')
PACKAGE_RE = re.compile(r'^package\s+(\w+)')

failures: list[tuple[str, str]] = []
method_counts: Counter[tuple[str, str]] = Counter()
method_files: defaultdict[tuple[str, str], list[str]] = defaultdict(list)
struct_registry: dict[tuple[str, str], tuple[str, int, list[str]]] = {}


def is_go_file(path: Path) -> bool:
    if path.suffix != '.go':
        return False
    if path.name.endswith('_generated.go'):
        return False
    return not any(part in SKIP_PARTS for part in path.parts)


def rel(path: Path) -> str:
    return path.as_posix()


def package_name(lines: list[str]) -> str:
    for line in lines:
        match = PACKAGE_RE.match(line)
        if match:
            return match.group(1)
    return ''


def receiver_type(receiver: str) -> str:
    fields = receiver.strip().split()
    if not fields:
        return ''

    raw_type = fields[-1].lstrip('*')
    match = re.match(r'([A-Za-z_][A-Za-z0-9_]*)(?:\[[^\]]+\])?$', raw_type)

    return match.group(1) if match else raw_type


def top_level_chunks(lines: list[str]) -> list[tuple[int, int]]:
    starts = [idx for idx, line in enumerate(lines) if TOP_DECL_RE.match(line)]
    return [(start, starts[i + 1] if i + 1 < len(starts) else len(lines)) for i, start in enumerate(starts)]


for path in sorted(p for p in ROOT.rglob('*.go') if is_go_file(p)):
    text = path.read_text(encoding='utf-8')
    lines = text.splitlines()
    path_s = rel(path)
    line_limit = TEST_FILE_MAX if path.name.endswith('_test.go') else PROD_FILE_MAX
    if len(lines) > line_limit:
        failures.append((f'lines:{path_s}', f'{path_s}: {len(lines)} lines exceeds {line_limit}'))

    if path.name.endswith('_test.go'):
        continue

    pkg = package_name(lines)
    func_count = sum(1 for line in lines if FUNC_RE.match(line))
    if func_count > FUNC_FILE_MAX:
        failures.append((f'funcs:{path_s}', f'{path_s}: {func_count} funcs exceeds {FUNC_FILE_MAX}'))

    for idx, line in enumerate(lines, 1):
        method = METHOD_RE.match(line)
        if method:
            recv = receiver_type(method.group(1))
            key = (pkg, recv)
            method_counts[key] += 1
            method_files[key].append(f'{path_s}:{idx}')

    for start, end in top_level_chunks(lines):
        first = lines[start]
        if FUNC_RE.match(first) and (end - start) > FUNC_LINES_MAX:
            name_match = FUNC_NAME_RE.match(first)
            func_name = name_match.group(1) if name_match else first.strip()
            failures.append((
                f'span:{path_s}:{func_name}',
                f'{path_s}:{start + 1}: function spans {end - start} lines exceeds {FUNC_LINES_MAX}: {first.strip()}',
            ))

    for idx, line in enumerate(lines):
        struct = STRUCT_RE.match(line)
        if not struct:
            continue
        name = struct.group(1)
        fields = 0
        direct_named = 0
        embeds: list[str] = []
        if '}' not in line[line.find('{') + 1:]:
            j = idx + 1
            while j < len(lines):
                current = lines[j].strip()
                if current.startswith('}'):
                    break
                if current and not current.startswith('//'):
                    fields += 1
                    code = current.split('`')[0].split('//')[0].strip()
                    if code and ' ' not in code and '\t' not in code:
                        embeds.append(code.lstrip('*'))
                    else:
                        direct_named += 1
                j += 1
        struct_registry[(pkg, name)] = (path_s, direct_named, embeds)
        if fields > STRUCT_FIELD_MAX and (path_s, name) not in STRUCT_FIELD_ALLOWLIST:
            failures.append((f'fields:{path_s}:{name}', f'{path_s}: struct {name} has {fields} fields exceeds {STRUCT_FIELD_MAX}'))

for (pkg, recv), count in sorted(method_counts.items(), key=lambda item: item[1], reverse=True):
    if count > METHOD_MAX:
        locations = ', '.join(method_files[(pkg, recv)][:5])
        failures.append((f'methods:{pkg}.{recv}', f'{pkg}.{recv}: {count} methods exceeds {METHOD_MAX}; first locations: {locations}'))

aggregate_memo: dict[tuple[str, str], int] = {}


def aggregate_reachable(key: tuple[str, str], stack: frozenset[tuple[str, str]]) -> int:
    if key in aggregate_memo:
        return aggregate_memo[key]
    if key in stack:
        return 0
    entry = struct_registry.get(key)
    if entry is None:
        return 1
    _, direct, embeds = entry
    total = direct
    next_stack = stack | {key}
    for embed in embeds:
        if '.' in embed:
            total += 1
            continue
        embed_key = (key[0], embed)
        total += aggregate_reachable(embed_key, next_stack) if embed_key in struct_registry else 1
    aggregate_memo[key] = total
    return total


for (pkg, name), (path_s, _direct, embeds) in sorted(struct_registry.items()):
    if not any('.' not in embed and (pkg, embed) in struct_registry for embed in embeds):
        continue
    if (path_s, name) in STRUCT_FIELD_ALLOWLIST:
        continue
    reachable = aggregate_reachable((pkg, name), frozenset())
    if reachable > AGGREGATE_REACHABLE_FIELDS_MAX:
        failures.append((
            f'aggregate:{path_s}:{name}',
            f'{path_s}: struct {name} reaches {reachable} promoted fields via embedding exceeds {AGGREGATE_REACHABLE_FIELDS_MAX}',
        ))

baseline: set[str] = set()
if BASELINE_PATH.exists():
    for raw in BASELINE_PATH.read_text(encoding='utf-8').splitlines():
        entry = raw.strip()
        if entry and not entry.startswith('#'):
            baseline.add(entry)

failure_keys = {key for key, _ in failures}
active = [message for key, message in failures if key not in baseline]
suppressed = len(failures) - len(active)
stale = sorted(baseline - failure_keys)
for entry in stale:
    active.append(f'baseline entry no longer failing; remove it from {BASELINE_PATH.as_posix()}: {entry}')

if active:
    print('[responsibility-gate] failed', file=sys.stderr)
    for failure in active:
        print(f'- {failure}', file=sys.stderr)
    sys.exit(1)

print('[responsibility-gate] passed')
if suppressed:
    print(f'  baseline-suppressed pre-existing violations: {suppressed} (shrink-only, see {BASELINE_PATH.as_posix()})')
print(f'  production file lines <= {PROD_FILE_MAX}')
print(f'  test file lines <= {TEST_FILE_MAX}')
print(f'  funcs per production file <= {FUNC_FILE_MAX}')
print(f'  methods per receiver type <= {METHOD_MAX}')
print(f'  struct fields <= {STRUCT_FIELD_MAX} except documented data/config aggregates')
print(f'  embed-aggregate reachable fields <= {AGGREGATE_REACHABLE_FIELDS_MAX} except documented aggregates')
print(f'  function spans <= {FUNC_LINES_MAX} lines')
PY
