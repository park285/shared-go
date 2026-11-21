#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ast
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
from dataclasses import dataclass, replace
from functools import cache
from importlib.util import module_from_spec, spec_from_file_location
from itertools import combinations, islice, takewhile
from pathlib import Path


PREAMBLE = re.compile(rb"\s+|//[^\n]*|/\*.*?\*/", re.DOTALL)
GENERATED = re.compile(rb"^// Code generated .* DO NOT EDIT\.\r?$", re.MULTILINE)
GO_TOKEN = re.compile(rb'\s+|//[^\n]*|/\*.*?\*/|"(?:\\.|[^"\\])*"|`[^`]*`|[\w\x80-\xff]+|.', re.DOTALL)
GO_ESCAPE = re.compile(rb'\\(x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}|[0-7]{3}|.)', re.DOTALL)
TOKEN = re.compile(r"[\w.]+|&&|\|\||[^ \t]")
IDENTIFIER = re.compile(r"[\w.]+")
UNIX = set("aix android darwin dragonfly freebsd hurd illumos ios linux netbsd openbsd solaris".split())
OS_ALIAS = {"android": "linux", "illumos": "solaris", "ios": "darwin"}
COMPILED = ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles")
CGO_INPUTS = ("CgoFiles", "SwigFiles", "SwigCXXFiles")
SEARCH_LIMIT = 100_000
EXTERNAL_LINK_WITHOUT_CGO = {("android", arch) for arch in ("386", "amd64", "arm")} | {("ios", "arm64")}
TOKEN_LIMIT = 256
MODULE_RUN_LIMIT = 16
TOTAL_RUN_LIMIT = 64
FEATURES = {
    "386": ("GO386", ("sse2", "softfloat")),
    "amd64": ("GOAMD64", ("v1", "v2", "v3", "v4")),
    "arm": ("GOARM", ("5", "6", "7")),
    "arm64": ("GOARM64", tuple(f"v8.{n}" for n in range(10)) + tuple(f"v9.{n}" for n in range(6))),
    "mips": ("GOMIPS", ("hardfloat", "softfloat")),
    "mips64": ("GOMIPS64", ("hardfloat", "softfloat")),
    "ppc64": ("GOPPC64", ("power8", "power9", "power10")),
    "riscv64": ("GORISCV64", ("rva20u64", "rva22u64", "rva23u64")),
    "wasm": ("GOWASM", ("satconv", "signext", "satconv,signext")),
}
for family in ("mips", "mips64", "ppc64"):
    FEATURES[family + "le"] = FEATURES[family]
FEATURE_TAGS = {
    f"{arch}.{level}" for arch, (_, levels) in FEATURES.items() for level in levels if "," not in level
} | {"386.387"}


def parse(expression, budget):
    words = [m[0] for m in islice(TOKEN.finditer(expression), TOKEN_LIMIT + 1)]
    if len(words) > TOKEN_LIMIT:
        budget.exceeded(f"constraint exceeds {TOKEN_LIMIT} tokens")
    tokens = iter(words)
    current = next(tokens, None)

    def take():
        nonlocal current
        token, current = current, next(tokens, None)
        return token

    def operand():
        token = take()
        if token == "!":
            if current == "!":
                raise ValueError(f"double negation in {expression!r}")
            return ("!", operand())
        if token == "(":
            group = disjunction()
            if take() != ")":
                raise ValueError("expected ')' in build constraint")
            return group
        if token is None or not IDENTIFIER.fullmatch(token):
            raise ValueError(f"expected a build tag in {expression!r}")
        return token

    def operation(operator, term):
        parts = [term()]
        while current == operator:
            take()
            parts.append(term())
        if len(parts) == 1:
            return parts[0]
        return (operator, *parts)

    def disjunction():
        return operation("||", lambda: operation("&&", operand))

    result = disjunction()
    if current is not None:
        raise ValueError(f"unexpected token {current!r} in {expression!r}")
    return result


def evaluate(expr, lookup):
    if isinstance(expr, bool):
        return expr
    if isinstance(expr, str):
        return lookup(expr)
    if expr[0] == "!":
        return not evaluate(expr[1], lookup)
    values = (evaluate(child, lookup) for child in expr[1:])
    return all(values) if expr[0] == "&&" else any(values)


def without_cgo(expr):
    if isinstance(expr, str):
        return False if expr == "cgo" else expr
    return (expr[0], *(without_cgo(child) for child in expr[1:]))


def legacy_constraint(header):
    lines = []
    accepted = 0
    for raw in header.split(b"\n")[:-1]:
        line = raw.strip()
        if not line:
            accepted = len(lines)
        elif not line.startswith(b"//"):
            break
        else:
            fields = line[2:].decode("utf-8", "replace").split()
            if fields[:1] == ["+build"]:
                lines.append(fields[1:])
    conjunction = []
    for fields in lines[:accepted]:
        clauses = [clause.split(",") for clause in fields]
        if sum(map(len, clauses)) > 101:
            continue
        alternatives = []
        for clause in clauses:
            terms = []
            for literal in clause:
                tag = literal.removeprefix("!")
                if IDENTIFIER.fullmatch(tag):
                    terms.append(literal)
                elif literal.startswith("!") and tag and not tag.startswith("!"):
                    terms.append("!ignore")
                else:
                    terms.append("ignore")
            alternatives.append(" && ".join(terms))
        conjunction.append("(" + " || ".join(alternatives or ["ignore"]) + ")")
    return " && ".join(conjunction)


def unquote_import(token):
    if token.startswith(b"`"):
        return token[1:-1].replace(b"\r", b"")

    def unescape(match):
        code = match[1]
        if code.startswith((b"u", b"U")):
            return chr(int(code[1:], 16)).encode()
        if code.startswith(b"x"):
            return bytes([int(code[1:], 16)])
        if len(code) == 3:
            return bytes([int(code, 8)])
        escapes = {b"a": b"\a", b"b": b"\b", b"f": b"\f", b"n": b"\n", b"r": b"\r",
                   b"t": b"\t", b"v": b"\v", b'"': b'"', b"\\": b"\\"}
        if code not in escapes:
            raise ValueError(f"invalid Go import escape {code!r}")
        return escapes[code]

    return GO_ESCAPE.sub(unescape, token[1:-1])


def import_paths(source, offset):
    tokens = (
        match[0] for match in GO_TOKEN.finditer(source, offset)
        if not match[0].isspace() and not match[0].startswith((b"//", b"/*"))
    )
    if next(tokens, None) != b"package":
        return
    next(tokens, None)
    for token in tokens:
        if token == b";":
            continue
        if token != b"import":
            break
        spec = next(tokens, b"")
        if spec == b"(":
            paths = takewhile(lambda item: item != b")", tokens)
        else:
            paths = (spec if spec.startswith((b'"', b"`")) else next(tokens, b""),)
        for item in paths:
            if item.startswith((b'"', b"`")):
                yield unquote_import(item)


def read_preamble(path):
    if path.resolve() != path.absolute():
        raise ValueError(f"{path}: tracked source path traverses a symbolic link")
    source = path.read_bytes().removeprefix(b"\xef\xbb\xbf")
    offset = 0
    constraints = []
    generated = False
    while token := PREAMBLE.match(source, offset):
        if GENERATED.search(token[0]):
            generated = True
        if directive := re.fullmatch(rb"//go:build(?:[ \t]+(.*?))?\s*", token[0]):
            line_start = source.rfind(b"\n", 0, offset) + 1
            if not source[line_start:offset].strip():
                constraints.append(directive[1] or b"")
        offset = token.end()
    return source, offset, generated, constraints


@dataclass(frozen=True)
class Source:
    constraint: str
    main: bool
    generated: bool
    forbidden: frozenset


def source_header(path):
    source, offset, generated, constraints = read_preamble(path)
    forbidden = frozenset(import_paths(source, offset)) & {b"C", b"unsafe"}
    if forbidden:
        return Source("", False, generated, forbidden)
    if len(constraints) > 1:
        raise ValueError("multiple //go:build directives")
    constraint = constraints[0].decode() if constraints else legacy_constraint(source[:offset])
    if constraints and not constraint:
        raise ValueError("empty //go:build directive")
    main = False
    if re.search(r"(?<![\w.])ignore(?![\w.])", constraint) and source.startswith(b"package", offset):
        offset += len(b"package")
        while token := PREAMBLE.match(source, offset):
            offset = token.end()
        main = re.match(rb"main\b", source[offset:]) is not None
    return Source(constraint, main and not path.name.endswith("_test.go"), generated, forbidden)


@dataclass(frozen=True)
class Config:
    goos: str
    arch: str
    cgo: bool
    instrument: str = ""
    tags: tuple = ()
    feature: str | None = None
    experiments: tuple = ()

    def build_tags(self):
        return ",".join((*self.tags, self.instrument) if self.instrument else self.tags)

    def describe(self):
        feature = f" {FEATURES[self.arch][0]}={self.feature}" if self.feature is not None else ""
        experiments = f" GOEXPERIMENT={','.join(self.experiments)}" if self.experiments else ""
        return f"{self.goos}/{self.arch} CGO_ENABLED={int(self.cgo)} tags={self.build_tags() or '-'}{feature}{experiments}"

    def environment(self, base_env):
        instrument = f"-{self.instrument}" if self.instrument else "-race=false"
        env = base_env | {
            "GOOS": self.goos, "GOARCH": self.arch, "CGO_ENABLED": str(int(self.cgo)),
            "GOEXPERIMENT": ",".join(self.experiments) or ",",
            "GOFLAGS": f"-mod=readonly -tags={self.build_tags()} {instrument}",
        }
        if self.feature is not None:
            env[FEATURES[self.arch][0]] = self.feature
        return env


class Matrix:
    def __init__(self, module, env, lint_version, workspace_key):
        self.module = module
        self.directory = module.resolve()
        self.env = env | {"PWD": str(self.directory)}
        self.lint_version = lint_version
        self.workspace_key = workspace_key
        self.remaining = SEARCH_LIMIT
        self.responsible = ()
        self.provisional_runs = 1
        host = self.go_json("env", "-json", "GOHOSTOS", "GOHOSTARCH")
        self.env.update(GOOS=host["GOHOSTOS"], GOARCH=host["GOHOSTARCH"])
        self.env.pop("CGO_ENABLED", None)
        cgo = self.go_json("env", "-json", "CGO_ENABLED")["CGO_ENABLED"]
        self.host = Config(host["GOHOSTOS"], host["GOHOSTARCH"], cgo == "1")
        ports = self.go_json("tool", "dist", "list", "-json")
        self.ports = sorted(ports, key=lambda p: (
            p["GOOS"] != self.host.goos, p["GOARCH"] != self.host.arch, not p["FirstClass"],
            p["GOOS"], p["GOARCH"],
        ))
        self.oses = {p["GOOS"] for p in ports} | UNIX | {"nacl", "zos"}
        self.arches = {p["GOARCH"] for p in ports} | set(
            "amd64p32 armbe arm64be mips64p32 mips64p32le ppc riscv s390 sparc sparc64".split()
        )

    def go_json(self, *args):
        return json.loads(go_output(["go", *args], cwd=self.directory, env=self.env))

    def exceeded(self, reason):
        paths = ", ".join(str(self.module / path) for path in sorted(self.responsible))
        raise PlanningLimit(f"{self.module}: {reason}; expected full-module runs so far: "
                            f"{self.provisional_runs} (incomplete plan); responsible files: {paths}")

    def charge(self):
        self.remaining -= 1
        if self.remaining < 0:
            self.exceeded(f"constraint search exceeds {SEARCH_LIMIT} steps")

    @cache
    def environment(self, config):
        env = config.environment(self.env)
        keys = ["GOCACHE", "GOVERSION", "GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GO111MODULE", "GOWORK"]
        if config.arch in FEATURES:
            keys.append(FEATURES[config.arch][0])
        identity = json.loads(go_output(["go", "env", "-json", *keys], cwd=self.directory, env=env))
        if identity["GOEXPERIMENT"] != env["GOEXPERIMENT"] or identity["GOWORK"] != env["GOWORK"]:
            raise ValueError(f"{self.module}: Go did not use the planned GOEXPERIMENT and GOWORK")
        identity["GOWORK"] = self.workspace_key
        root = Path(identity.pop("GOCACHE"))
        if not root.is_absolute():
            raise ValueError(f"{self.module}: Go must supply an absolute GOCACHE, got {root}")
        identity.update(instrument=config.instrument, tags=config.tags, golangci_lint=self.lint_version,
                        packages_driver=env["GOPACKAGESDRIVER"])
        digest = hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()
        directory = root / "iris-go-lint-matrix" / digest
        return env | {"GOCACHE": str(directory / "go-build"), "GOLANGCI_LINT_CACHE": str(directory / "golangci-lint")}

    def filename_tags(self, path):
        parts = path.name.partition(".")[0].removesuffix("_test").split("_")[1:]
        if parts and parts[-1] in self.arches:
            yield parts.pop()
        if parts and parts[-1] in self.oses:
            yield parts[-1]

    @cache
    def tool_tags(self, goos, arch, feature, experiments=()):
        template = '{{context.Compiler}} {{join context.ToolTags " "}} {{join context.ReleaseTags " "}}'
        env = Config(goos, arch, False, feature=feature, experiments=experiments).environment(self.env)
        result = captured(["go", "list", "-find", "-f", template, "unsafe"], cwd=self.directory, env=env)
        if result.returncode:
            if experiments and re.search(r"^go: (?:unknown GOEXPERIMENT |GOEXPERIMENT .* requires )",
                                         result.stderr, re.MULTILINE):
                return None
            raise ValueError(redacted(result.stderr).strip())
        return result.stdout.split()

    def tag(self, name, config, standalone):
        if name in self.oses:
            return name in (config.goos, OS_ALIAS.get(config.goos))
        if name in self.arches:
            return name == config.arch
        if name == "unix":
            return config.goos in UNIX
        if name == "cgo":
            return config.cgo
        if name in ("race", "asan", "msan"):
            return config.instrument == name
        if name == "ignore":
            return standalone
        if name in FEATURE_TAGS and name.partition(".")[0] != config.arch:
            return False
        if (name in ("gc", "gccgo", "boringcrypto") or re.fullmatch(r"go1\.\d+", name)
                or name.startswith("goexperiment.") or name in FEATURE_TAGS):
            if name == "boringcrypto":
                name = "goexperiment.boringcrypto"
            return name in self.tool_tags(config.goos, config.arch, config.feature, config.experiments)
        return None

    @cache
    def supports_instrument(self, config):
        env = self.environment(config)
        result = captured(
            ["go", "list", "-find", "-f", '{{join context.ToolTags " "}}', "unsafe"],
            cwd=self.directory, env=env,
        )
        if result.returncode and f"-{config.instrument} is not supported on" in result.stderr:
            return False
        if result.returncode:
            raise ValueError(redacted(result.stderr).strip())
        return config.instrument in result.stdout.split()

    def configurations(self, feature_arches, instruments):
        native_port = (self.host.goos, self.host.arch)
        order = [(port, cgo) for port in self.ports if (port["GOOS"], port["GOARCH"]) == native_port
                 for cgo in (self.host.cgo, not self.host.cgo)]
        order += [(port, cgo) for cgo in (False, True) for port in self.ports
                  if (port["GOOS"], port["GOARCH"]) != native_port]
        for port, cgo in order:
            goos, arch = port["GOOS"], port["GOARCH"]
            native = (goos, arch) == native_port
            if cgo and not port["CgoSupported"]:
                continue
            if not cgo and (goos, arch) in EXTERNAL_LINK_WITHOUT_CGO:
                continue
            levels = FEATURES[arch][1] if arch in feature_arches else ()
            for feature in (None, *levels):
                yield Config(goos, arch, cgo, feature=feature)
                if cgo and native:
                    for instrument in instruments:
                        yield Config(goos, arch, True, instrument, feature=feature)

    @cache
    def known_experiment(self, name):
        result = captured(["go", "env", "GOEXPERIMENT"], cwd=self.directory, env=self.env | {"GOEXPERIMENT": name})
        if result.returncode and "unknown GOEXPERIMENT " in result.stderr:
            return False
        if result.returncode and not re.search(r"GOEXPERIMENT .* requires ", result.stderr):
            raise ValueError(redacted(result.stderr).strip())
        return True

    def experiment_configs(self, base, names):
        yield base
        if not names:
            return
        defaults = self.tool_tags(base.goos, base.arch, base.feature)
        for count in range(1, len(names) + 1):
            for chosen in combinations(names, count):
                self.charge()
                flags = tuple("no" + name if "goexperiment." + name in defaults else name for name in chosen)
                effective = self.tool_tags(base.goos, base.arch, base.feature, flags)
                if effective is not None and all(("goexperiment." + n in effective) !=
                                                 ("goexperiment." + n in defaults) for n in chosen):
                    yield replace(base, experiments=flags)

    def platforms(self, builtins, instruments):
        if not builtins.difference({"ignore"}):
            yield self.host
            return
        feature_arches = {name.partition(".")[0] for name in builtins if name in FEATURE_TAGS}
        names = {name.removeprefix("goexperiment.") for name in builtins
                 if name == "boringcrypto" or re.fullmatch(r"goexperiment\.[a-z][a-z0-9]*", name)}
        experiments = [name for name in sorted(names) if self.known_experiment(name)]
        seen = set()
        for base in self.configurations(feature_arches, instruments):
            for config in self.experiment_configs(base, experiments):
                self.charge()
                signature = tuple(self.tag(name, config, False) for name in builtins)
                if signature in seen:
                    continue
                if config.instrument and not self.supports_instrument(config):
                    continue
                seen.add(signature)
                yield config

    @cache
    def first_config(self, expr, names, main, instruments):
        custom = sorted(name for name in names if self.tag(name, self.host, False) is None)
        platforms = []

        def accepts(config):
            self.charge()
            return self.matches(config, expr, False) or (main and self.matches(config, expr, True))

        for config in self.platforms(names.difference(custom), instruments):
            if accepts(config):
                return config
            platforms.append(config)
        for count in range(1, len(custom) + 1):
            for tags in combinations(custom, count):
                for base in platforms:
                    config = replace(base, tags=tags)
                    if accepts(config):
                        return config
        return None

    def matches(self, config, expr, standalone):
        return expr is None or evaluate(expr, lambda name: name in config.tags or bool(self.tag(name, config, standalone)))


class PlanningLimit(ValueError):
    pass


def go_list(module, env, tags, targets, flags, fields):
    data = go_output(
        ["go", "list", "-e", *flags, "-buildvcs=false", "-tags=" + tags,
         "-json=" + ",".join(fields), *targets],
        cwd=module.resolve(), env=env,
    ).decode()
    decoder = json.JSONDecoder()
    nonspace = re.compile(r"\S")
    offset = 0
    while start := nonspace.search(data, offset):
        package, offset = decoder.raw_decode(data, start.start())
        yield package


def list_packages(module, env, tags, active, tool):
    targets = ["./" + str(path) for path in tool] if tool is not None else ["./..."]
    fields = ("Dir", *CGO_INPUTS)
    found = list(go_list(module, env, tags, targets, ["-find"], fields))
    if tool is None:
        module_dir = module.resolve()
        vendor = module_dir / "vendor"
        if (vendor / "modules.txt").is_file():
            kept = [pkg for pkg in found if not (pkg.get("Dir") and Path(pkg["Dir"]).resolve().is_relative_to(vendor)
                                                 and Path(pkg["Dir"]).resolve() != vendor)]
            if len(kept) != len(found):
                found = kept
                targets = sorted({"./" + str(Path(pkg["Dir"]).resolve().relative_to(module_dir)) for pkg in kept if pkg.get("Dir")})
        directories = {Path(pkg["Dir"]).resolve() for pkg in found if pkg.get("Dir")}
        skipped = sorted({path.parent for path in active if module_dir / path.parent not in directories})
        if skipped:
            extra = ["./" + str(directory) for directory in skipped]
            found.extend(go_list(module, env, tags, extra, ["-find"], fields))
            targets.extend(extra)
    prohibited = set()
    for package in found:
        for field in CGO_INPUTS:
            for name in package.get(field, []):
                prohibited.add(Path(package["Dir"]) / name)
    if prohibited:
        raise ValueError("\n".join(f"{path}: cgo is banned with unsafe" for path in sorted(prohibited)))
    fields = ("Dir", "ImportPath", "Module", *COMPILED, *CGO_INPUTS[1:], "CompiledGoFiles", "IgnoredGoFiles", "Error")
    return targets, list(go_list(module, env, tags, targets, ["-test", "-compiled"], fields))


def run_linter(binary, module, env, tags, targets, packages):
    args = [
        binary, "run", "-c", Path.cwd() / ".golangci.yml", "--verbose", "--color=never",
        "--tests=true", "--build-tags=" + tags, "--modules-download-mode=readonly",
        "--issues-exit-code=1", "--allow-parallel-runners", *targets,
    ]
    env = env | {"GL_DEBUG": "loader,goanalysis/issues/cache", "LOG_LEVEL": "debug", "LOG_TIMESTAMP": "0",
                 "CLICOLOR_FORCE": "0", "CLICOLOR": "0"}
    result = captured(args, cwd=module.resolve(), env=env)
    sys.stdout.write(redacted(result.stdout))
    sys.stdout.flush()
    actual = set()
    matched = {}
    skipped = set()
    analyzed = set()
    other = []
    for line in result.stderr.split("\n"):
        if line.startswith('level=debug msg="[loader] Loaded pkg #'):
            message = ast.literal_eval(line.partition(" msg=")[2])
            record = re.fullmatch(r"\[loader\] Loaded pkg #\d+: (ID=.+) Syntax=\[.*\]", message, re.DOTALL)
            if record and record[1] in packages:
                package_id, files = packages[record[1]]
                matched[package_id] = files
        elif line.startswith('level=debug msg="[loader] skip pkg ID='):
            message = ast.literal_eval(line.partition(" msg=")[2])
            skipped.add(message.removeprefix("[loader] skip pkg ID=").removesuffix(" because we load it with test package"))
        elif line.startswith('level=debug msg="[goanalysis/issues/cache] '):
            message = ast.literal_eval(line.partition(" msg=")[2])
            record = re.fullmatch(r"\[goanalysis/issues/cache\] (?:Loaded|Saved) package (.+) issues \(\d+\) (?:from|to) cache", message)
            if record:
                analyzed.add(record[1])
        elif match := re.fullmatch(r'level=info msg="\[loader\] Using build tags: \[([^\]]*)\]"', line):
            actual.update(match[1].split())
        elif line and not line.startswith(("level=info ", "level=debug ")):
            other.append(line + "\n")
    sys.stderr.write(redacted("".join(other)))
    retained = matched.keys() - skipped
    loaded = {path for pkg in retained & analyzed for path in matched[pkg]}
    skipped_files = {path for pkg in matched.keys() & skipped for path in matched[pkg]}
    skipped_files.difference_update(path for pkg in retained for path in matched[pkg])
    if result.returncode:
        print(f"[lint] {module}: loader confirmed {len(loaded)} tracked files before lint failed", file=sys.stderr)
        if result.returncode == 1:
            return None
        result.check_returncode()
    if actual != set(filter(None, tags.split(","))):
        raise ValueError(f"{module}: golangci-lint loaded build tags {sorted(actual)}, expected {tags!r}")
    return loaded, skipped_files


def lint_group(module, binary, config, env, planned, tracked, reasons, failed, tool):
    tags = config.build_tags()
    module_dir = module.resolve()
    patterns, listed = list_packages(module, env, tags, planned, tool)
    manifest = (module_dir / "go.mod").resolve()
    compiled = set()
    cgo = set()
    packages = {}
    errors = set()
    for package in listed:
        if error := package.get("Error"):
            error = {"Err": redacted(error["Err"])}
            errors.add(error["Err"])
        elif tool is None:
            go_mod = (package.get("Module") or {}).get("GoMod")
            if not go_mod or Path(go_mod).resolve() != manifest:
                raise ValueError(f"{package.get('ImportPath')}: go list reported Module.GoMod={go_mod!r}, expected {manifest}")
        if not package.get("Dir"):
            continue
        physical = Path(package["Dir"]).resolve()
        if not physical.is_relative_to(module_dir):
            continue
        directory = physical.relative_to(module_dir)
        cgo.update(directory / name for field in CGO_INPUTS for name in package.get(field, []))
        if error:
            for path in planned:
                if path.parent == directory and path in reasons:
                    reasons[path] = error["Err"]
            continue
        for name in package.get("IgnoredGoFiles", []):
            path = directory / name
            if path in planned and path in reasons:
                reasons[path] = "excluded by Go"
        for field in COMPILED:
            files = {directory / name for name in package.get(field, [])}
            compiled.update(files)
        originals = [directory / name for name in package.get("GoFiles", [])]
        inputs = [directory / name for name in package.get("CompiledGoFiles", []) if Path(name).suffix in ("", ".go")]
        relevant = tracked.intersection(originals, inputs)
        if relevant and inputs:
            paths = " ".join(str(module_dir / path) for path in originals)
            compiled_paths = " ".join(str(module_dir / path) for path in inputs)
            record = f"ID={package['ImportPath']} GoFiles=[{paths}] CompiledGoFiles=[{compiled_paths}]"
            packages[record] = (package["ImportPath"], relevant)

    if cgo:
        raise ValueError("\n".join(f"{module / path}: cgo is banned with unsafe" for path in sorted(cgo)))
    for error in sorted(errors):
        print(f"[lint] {module}: go list: {error}", file=sys.stderr)
    if not listed:
        return not planned
    covered = compiled & tracked
    print(f"[lint] {module} {config.describe()} {' '.join(patterns)} ({len(covered)} files listed)", flush=True)
    result = run_linter(binary, module, env, tags, patterns, packages)
    if result is None:
        reason = f"lint exited 1 in {config.describe()} {' '.join(patterns)}"
        for path in covered | planned:
            failed.setdefault(path, reason)
        return False
    loaded, skipped = result
    for path in loaded & planned:
        reasons.pop(path, None)
    missing = (covered | planned) - loaded
    for path in sorted(missing):
        reason = ("package skipped by golangci-lint after loading" if path in skipped
                  else "not confirmed in golangci-lint's final analyzed packages")
        if path not in compiled:
            reason = reasons.get(path, "absent from listed packages")
        failed.setdefault(path, reason)
        print(f"[lint] uncovered in this configuration: {module / path} ({reason})", file=sys.stderr)
    if missing:
        print(f"[lint] {module}: loader confirmed {len(loaded)}/{len(covered)} listed files", file=sys.stderr)
    return not missing and not errors


def check_run_limit(label, configurations, limit):
    expected = sum(len(origins) for origins in configurations.values())
    if expected > limit:
        rows = []
        for module, origins in configurations.items():
            for config, paths in origins.items():
                files = ", ".join(str(module / path) for path in paths)
                rows.append(f"  {module}: {config.describe()} ({files})")
        raise PlanningLimit(f"{label}: expected {expected} full-module runs (limit {limit}); "
                            "configurations and responsible files:\n" + "\n".join(rows))


@dataclass
class ModulePlan:
    matrix: Matrix
    groups: dict
    reasons: dict
    origins: dict


def plan_module(module, sources, env, lint_version, violations, workspace_key):
    matrix = Matrix(module, env, lint_version, workspace_key)
    expressions = {}
    origins = {matrix.host: [Path("go.mod")]}
    reasons = {}
    for path, source in sorted(sources.items()):
        if source.generated and path.name.startswith(("_", ".")):
            continue
        matrix.responsible = (path,)
        matrix.provisional_runs = len(origins)
        constraint = source.constraint
        try:
            expr = parse(constraint, matrix) if constraint else None
        except PlanningLimit:
            raise
        except (ValueError, RecursionError) as exc:
            raise ValueError(f"{module / path}: {exc}") from exc
        suffixes = tuple(matrix.filename_tags(path))
        for tag in suffixes:
            expr = ("&&", tag, expr) if expr is not None else tag
        expressions[path] = expr, source.main
        if source.generated:
            names = frozenset(IDENTIFIER.findall(constraint)).union(suffixes)
            if names & {"cgo", "asan", "msan"}:
                permitted = ("&&", expr, without_cgo(expr)) if "cgo" in names else expr
                buildable = (matrix.matches(matrix.host, permitted, False)
                             or (source.main and matrix.matches(matrix.host, permitted, True))
                             or matrix.first_config(permitted, names, source.main, ("race",) if "race" in names else ()))
                modes = tuple(mode for mode in ("race", "asan", "msan") if mode in names)
                dependent = None if buildable else matrix.first_config(expr, names, source.main, modes)
                if dependent:
                    mode = f" -{dependent.instrument}" if dependent.instrument else ""
                    violations[module / path] = f"constraint ({constraint}) requires CGO_ENABLED=1{mode}"
            continue
        reasons[path] = "absent from listed packages"
        names = frozenset(IDENTIFIER.findall(constraint)).union(suffixes)
        permitted = ("&&", expr, without_cgo(expr)) if "cgo" in names else expr
        if matrix.matches(matrix.host, permitted, False) or (source.main and matrix.matches(matrix.host, permitted, True)):
            continue
        config = matrix.first_config(permitted, names, source.main, ("race",) if "race" in names else ())
        if config is not None:
            origins.setdefault(config, []).append(path)
            continue
        if names & {"cgo", "asan", "msan"}:
            modes = tuple(mode for mode in ("race", "asan", "msan") if mode in names)
            dependent = matrix.first_config(expr, names, source.main, modes)
            if dependent:
                mode = f" -{dependent.instrument}" if dependent.instrument else ""
                violations[module / path] = f"constraint ({constraint}) requires CGO_ENABLED=1{mode}"
                continue
        terms = ([f"({constraint})"] if constraint else []) + list(suffixes)
        reasons[path] = "unsatisfiable build constraint: " + " && ".join(terms)

    check_run_limit(module, {module: origins}, MODULE_RUN_LIMIT)
    groups = {config: (set(), []) for config in origins}
    for path, (expr, main) in expressions.items():
        matrix.responsible = (path,)
        for config, (ordinary, tools) in groups.items():
            if matrix.matches(config, expr, False):
                ordinary.add(path)
            if main and matrix.matches(config, expr, True):
                tools.append((path,))
    return ModulePlan(matrix, groups, reasons, origins)


def lint_module(module, binary, plan):
    reasons = plan.reasons
    handwritten = set(reasons)
    failed = {}
    total = len(reasons)
    if not total:
        print(f"[lint] {module}: no tracked hand-written Go files (0/0)", file=sys.stderr)
        return False
    ordinary = {path for planned, _ in plan.groups.values() for path in planned}
    ok = True
    for config, (planned, tools) in plan.groups.items():
        env = plan.matrix.environment(config)
        if not lint_group(module, binary, config, env, planned, ordinary, reasons, failed, None):
            ok = False
        for tool in tools:
            if not lint_group(module, binary, config, env, set(tool), set(tool), reasons, failed, tool):
                ok = False

    reasons.update((path, failed[path]) for path in handwritten.intersection(failed))
    print(f"{module}: {total - len(reasons)}/{total} tracked hand-written Go files covered", flush=True)
    for path, reason in sorted(reasons.items()):
        print(f"[lint] uncovered: {module / path} ({reason})", file=sys.stderr)
    return ok and not reasons


def regular_file(path):
    try:
        return stat.S_ISREG(os.lstat(path).st_mode)
    except FileNotFoundError:
        return False


def workspace_identity(workfile, directives):
    def physical(path):
        return str((workfile.parent / path).resolve())

    for entry in directives.get("Use") or []:
        entry["DiskPath"] = physical(entry["DiskPath"])
        entry.pop("ModPath", None)
    for replacement in directives.get("Replace") or []:
        target = replacement["New"]
        if not target.get("Version"):
            target["Path"] = physical(target["Path"])
    for field in ("Use", "Replace"):
        if directives.get(field):
            directives[field].sort(key=lambda entry: json.dumps(entry, sort_keys=True))
    if directives.get("Godebug"):
        directives["Godebug"].sort(key=lambda entry: entry["Key"])
    return hashlib.sha256(json.dumps(directives, sort_keys=True).encode()).hexdigest()


def main(argv):
    parser = argparse.ArgumentParser(
        allow_abbrev=False,
        usage="%(prog)s --list-modules | --golangci-lint PATH --golangci-lint-version vX.Y.Z "
              "[--gowork PATH] [--module DIR ...]",
    )
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--list-modules", action="store_true")
    mode.add_argument("--golangci-lint", metavar="PATH", type=Path)
    parser.add_argument("--golangci-lint-version", metavar="vX.Y.Z")
    parser.add_argument("--gowork", metavar="PATH", type=Path)
    parser.add_argument("--module", action="extend", nargs="+", type=Path, default=[])
    args = parser.parse_args(argv)
    if args.list_modules:
        if args.module or args.golangci_lint_version is not None or args.gowork is not None:
            parser.error("--list-modules takes no lint options")
    elif not re.fullmatch(r"v(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*)){2}", args.golangci_lint_version or ""):
        parser.error("--golangci-lint-version requires an exact vX.Y.Z release")
    env = {key: value for key, value in os.environ.items() if not key.startswith(("GOLANGCI_", "GL_", "GIT_"))}
    env.update(GOWORK="off", GOFLAGS="-mod=readonly", GO111MODULE="on", GOPACKAGESDRIVER="off",
               GOEXPERIMENT=",", PWD=str(Path.cwd()))
    if subprocess.check_output(["git", "rev-parse", "--show-prefix"], env=env).strip():
        parser.error("run from the repository root")
    entries = subprocess.check_output(["git", "ls-files", "-s", "-z"], env=env)
    staged = {name: meta for meta, name in (entry.split(b"\t", 1) for entry in entries.split(b"\0") if entry)}
    vendored = [Path(os.fsdecode(name)).parent for name in staged
                if (name == b"vendor/modules.txt" or name.endswith(b"/vendor/modules.txt"))
                and vendor_owner(Path(os.fsdecode(name)), staged)]
    paths = [path for path in map(Path, map(os.fsdecode, staged))
             if (path.name == "go.mod" or path.name.endswith((".go", ".swig", ".swigcxx")))
             and not any(path.is_relative_to(vendor) and path.parent != vendor for vendor in vendored)]
    problems = {}
    for path in paths:
        if path.name == "go.mod" and (not staged[os.fsencode(path)].startswith((b"100644 ", b"100755 "))
                                      or not regular_file(path)):
            problems[path] = "go.mod must be a tracked regular file, not a symlink"
    modules = {path.parent: {} for path in paths if path.name == "go.mod" and path not in problems}
    for path in paths:
        if path.name.endswith((".swig", ".swigcxx")):
            problems[path] = "cgo is banned with unsafe (SWIG input)"
            continue
        if not path.name.endswith(".go"):
            continue
        owner = next((parent for parent in path.parents if parent in modules), None)
        reasons = ["no owning tracked go.mod"] if owner is None else []
        try:
            source = source_header(path)
        except (OSError, ValueError, RuntimeError) as exc:
            problems[path] = ", ".join([*reasons, str(exc)])
            continue
        relative = path.relative_to(owner) if owner is not None else None
        if owner is not None:
            modules[owner][relative] = source
        if b"C" in source.forbidden:
            reasons.append('cgo is banned with unsafe (import "C")')
        if b"unsafe" in source.forbidden:
            reasons.append('import "unsafe" is banned')
        if reasons:
            problems[path] = ", ".join(reasons)
    for module in modules:
        if str(module).splitlines() != [str(module)]:
            problems[module] = f"module path {str(module)!r} contains a line separator"
    if not modules:
        print("[lint] repository has no tracked go.mod", file=sys.stderr)
    if problems or not modules:
        for path, reason in sorted(problems.items()):
            print(f"[lint] uncovered: {path} ({reason})", file=sys.stderr)
        return 1
    if args.list_modules:
        empty = [module for module, files in sorted(modules.items()) if all(src.generated for src in files.values())]
        if empty:
            for module in empty:
                print(f"[lint] {module}: no tracked hand-written Go files (0/0)", file=sys.stderr)
            return 1
        for module in sorted(modules):
            print(module)
        return 0
    args.module = [Path(os.path.relpath(path.resolve())) for path in args.module]
    if unknown := set(args.module) - modules.keys():
        parser.error("no tracked go.mod in " + ", ".join(map(str, sorted(unknown))))
    binary = args.golangci_lint.resolve(strict=True)
    version = subprocess.check_output([binary, "version"], env=env, text=True).strip()
    match = re.match(r"^golangci-lint has version (\S+)(?:\s|$)", version)
    if not match:
        raise ValueError(f"{binary}: version output did not identify golangci-lint")
    if match[1].removeprefix("v") != args.golangci_lint_version[1:]:
        raise ValueError(f"{binary}: expected {args.golangci_lint_version}, got {match[1]}")
    if not Path(".golangci.yml").is_file():
        raise ValueError("missing repository .golangci.yml")
    selected = dict.fromkeys(args.module or sorted(modules))
    workspace_key = "off"
    if args.gowork is not None:
        workfile = args.gowork.resolve(strict=True)
        if not workfile.is_file() or not workfile.is_relative_to(Path.cwd()):
            raise ValueError(f"{workfile}: workspace must be a file inside the repository")
        workspace = json.loads(go_output(["go", "work", "edit", "-json", workfile], env=env))
        members = {(workfile.parent / entry["DiskPath"]).resolve() for entry in workspace.get("Use") or []}
        missing = [str(module) for module in selected if module.resolve() not in members]
        if missing:
            raise ValueError(f"{workfile}: workspace does not use selected modules: {', '.join(missing)}")
        workspace_key = workspace_identity(workfile, workspace)
        env["GOWORK"] = str(workfile)
    plans = {}
    violations = {}
    if len(selected) > TOTAL_RUN_LIMIT:
        hosts = "\n".join(f"  {module}: host ({module / 'go.mod'})" for module in selected)
        raise PlanningLimit(f"repository minimum: expected at least {len(selected)} full-module runs "
                            f"(limit {TOTAL_RUN_LIMIT}); mandatory host configurations:\n{hosts}")
    for module in sorted(modules):
        if not (module / "go.mod").is_file():
            raise ValueError(f"{module / 'go.mod'}: missing tracked manifest")
        if module in selected:
            plans[module] = plan_module(module, modules[module], env, args.golangci_lint_version, violations, workspace_key)
        else:
            plan_module(module, modules[module], env | {"GOWORK": "off"}, args.golangci_lint_version, violations, "off")
    if violations:
        for path, detail in sorted(violations.items()):
            print(f"[lint] {path}: cgo is banned with unsafe ({detail})", file=sys.stderr)
        return 1
    check_run_limit("repository", {module: plan.origins for module, plan in plans.items()}, TOTAL_RUN_LIMIT)
    ok = True
    for module, plan in plans.items():
        if not lint_module(module, binary, plan):
            ok = False
    return int(not ok)


def vendor_owner(manifest, staged):
    owner = manifest.parent.parent
    if os.fsencode(owner / "go.mod") in staged:
        return True
    if os.fsencode(owner / "go.work") not in staged or not regular_file(manifest):
        return False
    return "## workspace" in manifest.read_text(encoding="utf-8", errors="replace").splitlines()


def captured(command, **options):
    result = subprocess.run(command, capture_output=True, **options)
    return subprocess.CompletedProcess(
        command, result.returncode, result.stdout.decode("utf-8", "replace"), result.stderr.decode("utf-8", "replace"),
    )


def load_resolver():
    spec = spec_from_file_location("resolve_go_tool", Path(__file__).with_name("resolve-go-tool.py"))
    module = module_from_spec(spec)
    sys.dont_write_bytecode = True
    spec.loader.exec_module(module)
    return module


RESOLVER = load_resolver()
redacted, go_output = RESOLVER.redacted, RESOLVER.go_output


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except subprocess.CalledProcessError as error:
        sys.exit(error.returncode if error.returncode > 0 else 128 - error.returncode)
    except (OSError, ValueError) as error:
        sys.exit(str(error))
