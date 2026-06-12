#!/usr/bin/env bash
set -euo pipefail

python3 - "$@" <<'PY'
import argparse
import copy
import os
import re
import shutil
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


TOP_FIELDS = {"schemaVersion", "repo", "defaults", "benchmarks", "settings"}
CLASSES = {"critical", "hotpath", "build_path", "non_critical"}
BUDGET_FIELDS = {
    "max_ns_regression_percent",
    "max_bytes_regression_percent",
    "allow_alloc_increase",
}
BENCH_FIELDS = BUDGET_FIELDS | {"package", "class", "gate"}
SETTINGS_FIELDS = {"mode", "benchstat_alpha", "min_count", "noise_floor_ns"}
FAIL_CLASSES = {"critical", "hotpath"}
GATES = {"pr", "nightly", "release"}
DEFAULT_BENCHTIME = "100ms"
PERF_ARTIFACT_ROOT = Path("artifacts/perf")
BenchKey = tuple[str, str]


class PolicyError(Exception):
    pass


@dataclass
class Sample:
    ns: float
    bytes_per_op: float | None
    allocs_per_op: float | None
    file: Path


@dataclass
class Results:
    samples: dict[BenchKey, list[Sample]]
    race: bool
    files: list[Path]
    count: int | None
    benchtime: str | None
    count_file: Path | None
    benchtime_file: Path | None


@dataclass
class Issue:
    level: str
    bench: str
    bench_class: str
    metric: str
    baseline: float
    candidate: float
    detail: str


def strip_comment(line: str) -> str:
    in_single = False
    in_double = False
    for idx, char in enumerate(line):
        if char == "'" and not in_double:
            in_single = not in_single
        elif char == '"' and not in_single:
            in_double = not in_double
        elif char == "#" and not in_single and not in_double:
            return line[:idx]
    return line


def parse_scalar(value: str):
    value = value.strip()
    if value in {"true", "True"}:
        return True
    if value in {"false", "False"}:
        return False
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        return value[1:-1]
    if re.fullmatch(r"-?\d+", value):
        return int(value)
    if re.fullmatch(r"-?\d+\.\d+", value):
        return float(value)
    return value


def split_inline_items(inner: str) -> list[str]:
    items = []
    current = []
    in_single = False
    in_double = False
    for char in inner:
        if char == "'" and not in_double:
            in_single = not in_single
        elif char == '"' and not in_single:
            in_double = not in_double
        if char == "," and not in_single and not in_double:
            item = "".join(current).strip()
            if item:
                items.append(item)
            current = []
            continue
        current.append(char)
    item = "".join(current).strip()
    if item:
        items.append(item)
    return items


def parse_inline_map(value: str) -> dict:
    inner = value.strip()[1:-1].strip()
    result = {}
    if not inner:
        return result
    for item in split_inline_items(inner):
        if ":" not in item:
            raise PolicyError(f"invalid inline map item: {item}")
        key, raw_value = item.split(":", 1)
        result[key.strip()] = parse_scalar(raw_value)
    return result


def parse_value(value: str, anchors: dict[str, object]):
    value = value.strip()
    if value.startswith("*"):
        name = value[1:].strip()
        if name not in anchors:
            raise PolicyError(f"unknown yaml alias: {name}")
        return copy.deepcopy(anchors[name])
    if value.startswith("{") and value.endswith("}"):
        return parse_inline_map(value)
    return parse_scalar(value)


def parse_yaml(path: Path) -> dict:
    root: dict = {}
    stack: list[tuple[int, dict]] = [(-1, root)]
    anchors: dict[str, object] = {}
    for lineno, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        if "\t" in line[: len(line) - len(line.lstrip(" "))]:
            raise PolicyError(f"{path}:{lineno}: tab indentation is not supported")
        indent = len(line) - len(line.lstrip(" "))
        text = line.strip()
        if ":" not in text:
            raise PolicyError(f"{path}:{lineno}: expected key: value")
        key, raw_value = text.split(":", 1)
        key = key.strip()
        raw_value = raw_value.strip()
        while indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if not isinstance(parent, dict):
            raise PolicyError(f"{path}:{lineno}: parent is not a mapping")
        if not raw_value or raw_value.startswith("&"):
            value: dict = {}
            parent[key] = value
            if raw_value.startswith("&"):
                anchor = raw_value[1:].split()[0]
                anchors[anchor] = value
            stack.append((indent, value))
            continue
        parent[key] = parse_value(raw_value, anchors)
    return root


def require_mapping(value, path: str) -> dict:
    if not isinstance(value, dict):
        raise PolicyError(f"{path} must be a mapping")
    return value


def unknown_fields(mapping: dict, allowed: set[str], path: str) -> None:
    for key in mapping:
        if key not in allowed:
            raise PolicyError(f"unknown field: {path}.{key}")


def require_number(value, path: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise PolicyError(f"{path} must be a number")
    return float(value)


def validate_policy(policy: dict, repo_name: str) -> None:
    unknown_fields(policy, TOP_FIELDS, "policy")
    if policy.get("schemaVersion") != 1:
        raise PolicyError("schemaVersion must be 1")
    if policy.get("repo") != repo_name:
        raise PolicyError(
            f"repo mismatch: policy repo={policy.get('repo')} running repo={repo_name}"
        )

    defaults = require_mapping(policy.get("defaults"), "defaults")
    unknown_fields(defaults, CLASSES, "defaults")
    for bench_class in CLASSES:
        if bench_class not in defaults:
            raise PolicyError(f"missing defaults.{bench_class}")
        config = require_mapping(defaults[bench_class], f"defaults.{bench_class}")
        unknown_fields(config, BUDGET_FIELDS, f"defaults.{bench_class}")
        for field in ("max_ns_regression_percent", "max_bytes_regression_percent"):
            require_number(config.get(field), f"defaults.{bench_class}.{field}")
        validate_alloc_policy(
            config.get("allow_alloc_increase"),
            f"defaults.{bench_class}.allow_alloc_increase",
        )

    benchmarks = require_mapping(policy.get("benchmarks"), "benchmarks")
    if not benchmarks:
        raise PolicyError("benchmarks must not be empty")
    for name, config in benchmarks.items():
        bench_path = f"benchmarks.{name}"
        config = require_mapping(config, bench_path)
        unknown_fields(config, BENCH_FIELDS, bench_path)
        for field in ("package", "class", "gate"):
            if field not in config:
                raise PolicyError(f"missing {bench_path}.{field}")
        if not isinstance(config["package"], str):
            raise PolicyError(f"{bench_path}.package must be a string")
        if config["class"] not in CLASSES:
            raise PolicyError(f"{bench_path}.class must be one of {sorted(CLASSES)}")
        if config["gate"] not in GATES:
            raise PolicyError(f"{bench_path}.gate must be one of {sorted(GATES)}")
        for field in ("max_ns_regression_percent", "max_bytes_regression_percent"):
            if field in config:
                require_number(config[field], f"{bench_path}.{field}")
        if "allow_alloc_increase" in config:
            validate_alloc_policy(config["allow_alloc_increase"], f"{bench_path}.allow_alloc_increase")

    settings = require_mapping(policy.get("settings"), "settings")
    unknown_fields(settings, SETTINGS_FIELDS, "settings")
    for field in SETTINGS_FIELDS:
        if field not in settings:
            raise PolicyError(f"missing settings.{field}")
    if settings["mode"] not in {"warn", "fail"}:
        raise PolicyError("settings.mode must be warn or fail")
    require_number(settings["benchstat_alpha"], "settings.benchstat_alpha")
    min_count = require_number(settings["min_count"], "settings.min_count")
    if min_count < 1:
        raise PolicyError("settings.min_count must be at least 1")
    noise_floor = require_number(settings["noise_floor_ns"], "settings.noise_floor_ns")
    if noise_floor < 0:
        raise PolicyError("settings.noise_floor_ns must be non-negative")


def validate_alloc_policy(value, path: str) -> None:
    if value is not False and value != "warn":
        raise PolicyError(f"{path} must be false or warn")


def selected_benchmarks(policy: dict, gate: str | None) -> dict:
    benchmarks = policy["benchmarks"]
    if gate is None:
        return benchmarks
    selected = {name: cfg for name, cfg in benchmarks.items() if cfg["gate"] == gate}
    if not selected:
        raise PolicyError(f"no benchmarks selected for gate={gate}")
    return selected


def result_files(path: Path) -> list[Path]:
    if path.is_file():
        return [path]
    if not path.exists():
        return []
    return sorted(p for p in path.rglob("*") if p.is_file())


def ensure_safe_perf_candidate_path(path: Path) -> None:
    if path.is_absolute():
        raise PolicyError(
            f"candidate path must be a relative path under artifacts/perf/: {path}"
        )
    normalized = Path(os.path.normpath(str(path)))
    if normalized == Path(".") or ".." in normalized.parts:
        raise PolicyError(
            f"candidate path must be a relative path under artifacts/perf/: {path}"
        )
    try:
        normalized.relative_to(PERF_ARTIFACT_ROOT)
    except ValueError as exc:
        raise PolicyError(
            f"candidate path must be a relative path under artifacts/perf/: {path}"
        ) from exc


def has_race_marker(line: str) -> bool:
    lower = line.lower()
    return bool(re.search(r"(^|[\s=])-race($|\s)", line)) or "race detector" in lower


def parse_results(path: Path) -> Results:
    samples: dict[BenchKey, list[Sample]] = {}
    race = False
    files = result_files(path)
    count: int | None = None
    benchtime: str | None = None
    count_file: Path | None = None
    benchtime_file: Path | None = None
    pattern = re.compile(
        r"^(Benchmark\S*)\s+\d+\s+([0-9.]+)\s+ns/op"
        r"(?:\s+([0-9.]+)\s+B/op)?"
        r"(?:\s+([0-9.]+)\s+allocs/op)?"
    )
    count_pattern = re.compile(r"(?:^|\s)-count=(\d+)(?:\s|$)")
    benchtime_pattern = re.compile(r"(?:^|\s)-benchtime=([^\s)]+)")
    for file_path in files:
        current_package: str | None = None
        current_package_from_comment = False
        try:
            text = file_path.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            raise PolicyError(f"cannot read result file {file_path}: {exc}") from exc
        for line in text.splitlines():
            stripped = line.strip()
            if has_race_marker(line):
                race = True
            if stripped.startswith("# package:"):
                current_package = stripped.split(":", 1)[1].strip()
                current_package_from_comment = True
                continue
            if stripped.startswith("pkg:") and not current_package_from_comment:
                current_package = stripped.split(":", 1)[1].strip()
                continue
            if stripped.startswith("# count:"):
                raw_count = stripped.split(":", 1)[1].strip()
                if raw_count.isdigit():
                    count = int(raw_count)
                    count_file = file_path
                continue
            if stripped.startswith("# benchtime:"):
                benchtime = stripped.split(":", 1)[1].strip()
                benchtime_file = file_path
                continue
            if stripped.startswith("# command:"):
                command = stripped.split(":", 1)[1]
                count_match = count_pattern.search(command)
                if count_match:
                    count = int(count_match.group(1))
                    count_file = file_path
                benchtime_match = benchtime_pattern.search(command)
                if benchtime_match:
                    benchtime = benchtime_match.group(1)
                    benchtime_file = file_path
            match = pattern.match(stripped)
            if not match:
                continue
            raw_name, ns, bytes_per_op, allocs_per_op = match.groups()
            name = re.sub(r"-\d+$", "", raw_name)
            package = current_package or ""
            samples.setdefault((package, name), []).append(
                Sample(
                    ns=float(ns),
                    bytes_per_op=float(bytes_per_op) if bytes_per_op is not None else None,
                    allocs_per_op=float(allocs_per_op) if allocs_per_op is not None else None,
                    file=file_path,
                )
            )
    return Results(
        samples=samples,
        race=race,
        files=files,
        count=count,
        benchtime=benchtime,
        count_file=count_file,
        benchtime_file=benchtime_file,
    )


def copy_candidate_to_baseline(candidate: Path, baseline: Path) -> None:
    if not candidate.exists():
        raise PolicyError(f"candidate path does not exist: {candidate}")
    if candidate.is_file():
        baseline.mkdir(parents=True, exist_ok=True)
        shutil.copy2(candidate, baseline / candidate.name)
        return
    baseline.mkdir(parents=True, exist_ok=True)
    for source in result_files(candidate):
        relative = source.relative_to(candidate)
        target = baseline / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)


def require_auto_baseline_quality(policy: dict, candidate: Results, allow_smoke: bool) -> None:
    if allow_smoke:
        return
    min_count = int(policy["settings"]["min_count"])
    if candidate.count is None:
        raise PolicyError(
            "refusing to create baseline from benchmark results without count metadata"
        )
    if candidate.count < min_count:
        raise PolicyError(
            "refusing to create baseline from smoke benchmark results: "
            f"count={candidate.count} < min_count={min_count}"
        )
    if candidate.benchtime is None:
        raise PolicyError(
            "refusing to create baseline from benchmark results without benchtime metadata"
        )
    if candidate.benchtime != DEFAULT_BENCHTIME:
        raise PolicyError(
            "refusing to create baseline from smoke benchmark results: "
            f"benchtime={candidate.benchtime} differs from required {DEFAULT_BENCHTIME}"
        )


def check_existing_baseline_quality(policy: dict, baseline: Results) -> int | None:
    min_count = int(policy["settings"]["min_count"])
    if baseline.count is None or baseline.count >= min_count:
        return None
    source = f" file={baseline.count_file}" if baseline.count_file else ""
    message = (
        "existing baseline is smoke/junk baseline: "
        f"count={baseline.count} < min_count={min_count}{source}"
    )
    if policy["settings"]["mode"] == "fail":
        print(f"error: {message}", file=sys.stderr)
        return 2
    print(f"warning: {message}")
    return None


def files_with_missing_metric(samples: list[Sample], metric: str) -> list[str]:
    return sorted({str(sample.file) for sample in samples if getattr(sample, metric) is None})


def required_metric_diagnostics(
    policy: dict, selected: dict, label: str, results: Results
) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []
    for name, config in selected.items():
        samples = results.samples.get(benchmark_key(name, config), [])
        if not samples:
            continue
        budget = merged_budget(policy, name, config)
        bytes_files = files_with_missing_metric(samples, "bytes_per_op")
        if "max_bytes_regression_percent" in budget and bytes_files:
            errors.append(
                f"missing B/op for {name} in {label} file(s): "
                f"{', '.join(bytes_files)}; run benchmarks with -benchmem"
            )
        alloc_files = files_with_missing_metric(samples, "allocs_per_op")
        if alloc_files:
            message = (
                f"missing allocs/op for {name} in {label} file(s): "
                f"{', '.join(alloc_files)}; run benchmarks with -benchmem"
            )
            if budget["allow_alloc_increase"] is False:
                errors.append(message)
            elif budget["allow_alloc_increase"] == "warn":
                warnings.append(message)
    return errors, warnings


def print_required_metric_diagnostics(
    policy: dict, selected: dict, baseline: Results | None, candidate: Results
) -> bool:
    all_errors: list[str] = []
    all_warnings: list[str] = []
    if baseline is not None:
        errors, warnings = required_metric_diagnostics(policy, selected, "baseline", baseline)
        all_errors.extend(errors)
        all_warnings.extend(warnings)
    errors, warnings = required_metric_diagnostics(policy, selected, "candidate", candidate)
    all_errors.extend(errors)
    all_warnings.extend(warnings)
    for warning in all_warnings:
        print(f"warning: {warning}")
    for error in all_errors:
        print(f"error: {error}", file=sys.stderr)
    return not all_errors


def mean_metric(samples: list[Sample], metric: str) -> float | None:
    values = []
    for sample in samples:
        value = getattr(sample, metric)
        if value is not None:
            values.append(value)
    if not values:
        return None
    return sum(values) / len(values)


def merged_budget(policy: dict, name: str, config: dict) -> dict:
    budget = dict(policy["defaults"][config["class"]])
    for field in BUDGET_FIELDS:
        if field in config:
            budget[field] = config[field]
    return budget


def benchmark_key(name: str, config: dict) -> BenchKey:
    return (config["package"], name)


def percent_delta(baseline: float, candidate: float) -> float:
    if baseline == 0:
        return float("inf") if candidate > 0 else 0.0
    return ((candidate - baseline) / baseline) * 100.0


def compare_results(policy: dict, selected: dict, baseline: Results, candidate: Results) -> list[Issue]:
    issues: list[Issue] = []
    noise_floor = float(policy["settings"]["noise_floor_ns"])
    for name, config in selected.items():
        key = benchmark_key(name, config)
        base_samples = baseline.samples[key]
        cand_samples = candidate.samples[key]
        budget = merged_budget(policy, name, config)
        bench_class = config["class"]

        base_ns = mean_metric(base_samples, "ns")
        cand_ns = mean_metric(cand_samples, "ns")
        if base_ns is not None and cand_ns is not None and cand_ns > base_ns:
            if base_ns < noise_floor:
                delta_ns = cand_ns - base_ns
                if delta_ns > noise_floor:
                    issues.append(
                        Issue(
                            "violation",
                            name,
                            bench_class,
                            "ns/op",
                            base_ns,
                            cand_ns,
                            f"+{delta_ns:.3f} ns exceeds absolute budget {noise_floor:.3f} ns",
                        )
                    )
            else:
                delta_percent = percent_delta(base_ns, cand_ns)
                limit = float(budget["max_ns_regression_percent"])
                if delta_percent > limit:
                    issues.append(
                        Issue(
                            "violation",
                            name,
                            bench_class,
                            "ns/op",
                            base_ns,
                            cand_ns,
                            f"+{delta_percent:.2f}% exceeds budget {limit:.2f}%",
                        )
                    )

        base_bytes = mean_metric(base_samples, "bytes_per_op")
        cand_bytes = mean_metric(cand_samples, "bytes_per_op")
        if base_bytes is not None and cand_bytes is not None and cand_bytes > base_bytes:
            delta_percent = percent_delta(base_bytes, cand_bytes)
            limit = float(budget["max_bytes_regression_percent"])
            if delta_percent > limit:
                issues.append(
                    Issue(
                        "violation",
                        name,
                        bench_class,
                        "B/op",
                        base_bytes,
                        cand_bytes,
                        f"+{delta_percent:.2f}% exceeds budget {limit:.2f}%",
                    )
                )

        base_allocs = mean_metric(base_samples, "allocs_per_op")
        cand_allocs = mean_metric(cand_samples, "allocs_per_op")
        if base_allocs is not None and cand_allocs is not None and cand_allocs > base_allocs:
            allow_alloc = budget["allow_alloc_increase"]
            if allow_alloc is False:
                issues.append(
                    Issue(
                        "violation",
                        name,
                        bench_class,
                        "allocs/op",
                        base_allocs,
                        cand_allocs,
                        "allocation count increased",
                    )
                )
            elif allow_alloc == "warn":
                issues.append(
                    Issue(
                        "warning",
                        name,
                        bench_class,
                        "allocs/op",
                        base_allocs,
                        cand_allocs,
                        "allocation count increased",
                    )
                )
    return issues


def check_missing(label: str, results: Results, selected: dict) -> list[str]:
    return [
        f"{name} package={config['package']}"
        for name, config in selected.items()
        if benchmark_key(name, config) not in results.samples
    ]


def print_issues(issues: list[Issue]) -> None:
    for issue in issues:
        print(
            f"{issue.level}: {issue.bench} class={issue.bench_class} "
            f"metric={issue.metric} baseline={issue.baseline:.3f} "
            f"candidate={issue.candidate:.3f} {issue.detail}"
        )


def resolve_package(repo_root: Path, package: str) -> tuple[Path, str]:
    if not package.startswith("./"):
        return repo_root, package
    package_dir = (repo_root / package[2:]).resolve()
    if not package_dir.exists():
        return repo_root, package
    current = package_dir
    module_dir = repo_root if (repo_root / "go.mod").exists() else None
    while True:
        if (current / "go.mod").exists():
            module_dir = current
            break
        if current == repo_root:
            break
        current = current.parent
    if module_dir is None or module_dir == repo_root:
        return repo_root, package
    rel = package_dir.relative_to(module_dir)
    package_arg = "." if str(rel) == "." else f"./{rel.as_posix()}"
    return module_dir, package_arg


def bench_regex(names: list[str]) -> str:
    return "^(" + "|".join(re.escape(name) for name in names) + ")$"


def collect_results(policy: dict, selected: dict, args, repo_root: Path) -> int:
    candidate = Path(args.candidate)
    ensure_safe_perf_candidate_path(candidate)
    if candidate.exists():
        if not candidate.is_dir():
            print(f"error: candidate path exists and is not a directory: {candidate}", file=sys.stderr)
            return 2
        shutil.rmtree(candidate)
    output_dir = candidate / "go-bench"
    output_dir.mkdir(parents=True, exist_ok=True)
    output_file = output_dir / f"{repo_root.name}.txt"
    count = args.count if args.count is not None else int(policy["settings"]["min_count"])
    benchtime = args.benchtime
    min_count = int(policy["settings"]["min_count"])
    smoke_run = count < min_count

    by_package: dict[str, list[str]] = {}
    for name, config in selected.items():
        by_package.setdefault(config["package"], []).append(name)

    with output_file.open("w", encoding="utf-8") as sink:
        print("# generated by scripts/perf/check-bench-regression.sh collect", file=sink)
        print(f"# repo: {repo_root.name}", file=sink)
        print(f"# count: {count}", file=sink)
        if benchtime:
            print(f"# benchtime: {benchtime}", file=sink)
        if smoke_run:
            print(f"# smoke run: count<min_count count={count} min_count={min_count}", file=sink)
        if args.gate:
            print(f"# gate: {args.gate}", file=sink)
        if os.environ.get("GOFLAGS"):
            print(f"# goflags: {os.environ['GOFLAGS']}", file=sink)
        for package, names in sorted(by_package.items()):
            cwd, package_arg = resolve_package(repo_root, package)
            regex = bench_regex(sorted(names))
            cmd = [
                "go",
                "test",
                "-run",
                "^$",
                "-bench",
                regex,
                "-benchmem",
                f"-count={count}",
            ]
            if benchtime:
                cmd.append(f"-benchtime={benchtime}")
            cmd.append(package_arg)
            display = " ".join(shlex.quote(part) for part in cmd)
            if cwd != repo_root:
                display = f"(cd {shlex.quote(str(cwd.relative_to(repo_root)))} && {display})"
            print(f"# package: {package}", file=sink)
            print(f"# command: {display}", file=sink)
            sink.flush()
            completed = subprocess.run(
                cmd,
                cwd=cwd,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                check=False,
            )
            sink.write(completed.stdout)
            if completed.stdout and not completed.stdout.endswith("\n"):
                sink.write("\n")
            sink.flush()
            if completed.returncode != 0:
                print(completed.stdout, end="", file=sys.stderr)
                print(
                    f"error: benchmark collection failed for package {package}",
                    file=sys.stderr,
                )
                return completed.returncode

    results = parse_results(candidate)
    missing = check_missing("candidate", results, selected)
    if missing:
        for name in missing:
            print(f"error: missing candidate benchmark: {name}", file=sys.stderr)
        return 2
    print(
        f"collected benchmark results: {output_file} "
        f"(benchmarks={len(selected)}, packages={len(by_package)}, count={count}"
        f"{', benchtime=' + benchtime if benchtime else ''})"
    )
    if smoke_run:
        print(f"smoke run: count<min_count (count={count}, min_count={min_count})")
    return 0


def check_results(policy: dict, selected: dict, args) -> int:
    baseline_path = Path(args.baseline)
    candidate_path = Path(args.candidate)
    candidate = parse_results(candidate_path)
    if not candidate.files:
        print(f"error: candidate path has no result files: {candidate_path}", file=sys.stderr)
        return 2

    missing_candidate = check_missing("candidate", candidate, selected)
    if missing_candidate:
        for name in missing_candidate:
            print(f"error: missing candidate benchmark: {name}", file=sys.stderr)
        return 2

    if not result_files(baseline_path):
        if candidate.race:
            print(
                "error: refusing to create baseline from race benchmark results",
                file=sys.stderr,
            )
            return 2
        if not print_required_metric_diagnostics(policy, selected, None, candidate):
            return 2
        require_auto_baseline_quality(policy, candidate, args.allow_smoke_baseline)
        copy_candidate_to_baseline(candidate_path, baseline_path)
        print(f"baseline created: {baseline_path}")
        return 0

    baseline = parse_results(baseline_path)
    if candidate.race or baseline.race:
        print("skip: race build result detected; benchmark budget comparison skipped")
        return 0

    missing_baseline = check_missing("baseline", baseline, selected)
    if missing_baseline:
        for name in missing_baseline:
            print(f"error: missing baseline benchmark: {name}", file=sys.stderr)
        return 2

    baseline_quality_status = check_existing_baseline_quality(policy, baseline)
    if baseline_quality_status is not None:
        return baseline_quality_status

    if not print_required_metric_diagnostics(policy, selected, baseline, candidate):
        return 2

    issues = compare_results(policy, selected, baseline, candidate)
    if not issues:
        gate = f", gate={args.gate}" if args.gate else ""
        print(
            f"ok: benchmark budget comparison passed "
            f"(benchmarks={len(selected)}, mode={policy['settings']['mode']}{gate})"
        )
        return 0

    print_issues(issues)
    mode = policy["settings"]["mode"]
    if mode == "warn":
        print("warn mode: violations reported; exiting 0")
        return 0
    fail_eligible = [
        issue
        for issue in issues
        if issue.level == "violation" and issue.bench_class in FAIL_CLASSES
    ]
    if fail_eligible:
        print("fail mode: critical/hotpath violations found")
        return 1
    print("fail mode: no critical/hotpath violations; exiting 0")
    return 0


def parse_args(argv: list[str]):
    action = "check"
    if argv and argv[0] in {"check", "collect"}:
        action = argv[0]
        argv = argv[1:]
    parser = argparse.ArgumentParser(
        prog="check-bench-regression.sh",
        description=(
            "Check Go benchmark regression budgets. benchstat_alpha is accepted "
            "by policy schema; this checker uses mean comparison across repetitions."
        ),
    )
    parser.add_argument("--baseline", default="artifacts/perf/baseline/main")
    parser.add_argument("--candidate", default="artifacts/perf/pr")
    parser.add_argument("--policy", default="perf-budget.yaml")
    parser.add_argument("--gate", choices=sorted(GATES))
    parser.add_argument("--count", type=int)
    parser.add_argument("--benchtime", default=DEFAULT_BENCHTIME)
    parser.add_argument(
        "--allow-smoke-baseline",
        action="store_true",
        help="Allow creating a baseline from smoke benchmark results.",
    )
    args = parser.parse_args(argv)
    if args.count is not None and args.count < 1:
        parser.error("--count must be at least 1")
    return action, args


def main(argv: list[str]) -> int:
    action, args = parse_args(argv)
    repo_root = Path.cwd().resolve()
    try:
        policy = parse_yaml(Path(args.policy))
        validate_policy(policy, repo_root.name)
        selected = selected_benchmarks(policy, args.gate)
        if action == "collect":
            return collect_results(policy, selected, args, repo_root)
        return check_results(policy, selected, args)
    except PolicyError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
PY
