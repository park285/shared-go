#!/usr/bin/env python3
import argparse
import hashlib
import itertools
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path

MODULES = {
    "golangci-lint": "github.com/golangci/golangci-lint/v2",
    "govulncheck": "golang.org/x/vuln",
}
BASELINES = {
    "386": ("GO386", "softfloat"),
    "amd64": ("GOAMD64", "v1"),
    "arm": ("GOARM", "5"),
    "arm64": ("GOARM64", "v8.0"),
    "mips": ("GOMIPS", "softfloat"),
    "mips64": ("GOMIPS64", "softfloat"),
    "ppc64": ("GOPPC64", "power8"),
    "riscv64": ("GORISCV64", "rva20u64"),
}
for family in ("mips", "mips64", "ppc64"):
    BASELINES[family + "le"] = BASELINES[family]


class UnsafeCache(RuntimeError):
    pass


def go_version_key(version):
    name = version.removeprefix("go").partition("-")[0]
    match = re.fullmatch(r"(\d+)\.(\d+)(?:\.(\d+)|(beta|rc)(\d+))?", name)
    if not match:
        raise ValueError(f"invalid Go version: {version}")
    major, minor, patch, prerelease, sequence = match.groups()
    if patch is None and (int(major), int(minor)) < (1, 21) and not prerelease:
        patch = "0"
    return int(major), int(minor), int(patch or "-1"), {None: 0, "beta": 1, "rc": 2}[prerelease], int(sequence or 0)


def digest(path):
    checksum = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1 << 20):
            checksum.update(chunk)
    return checksum.hexdigest()


def remove_stale_build(path):
    for directory, _, _ in os.walk(path):
        os.chmod(directory, 0o700)
    shutil.rmtree(path)


def check_permissions(path, info, ancestor=False):
    owners = (0, os.geteuid()) if ancestor else (os.geteuid(),)
    if info.st_uid not in owners:
        raise UnsafeCache(f"{path}: untrusted owner (uid {info.st_uid})")
    if info.st_mode & 0o022 and not (ancestor and info.st_mode & stat.S_ISVTX):
        raise UnsafeCache(f"{path}: group- or other-writable cache path")


def cache_directory(path, ancestor=False):
    try:
        info = path.lstat()
    except FileNotFoundError:
        try:
            path.mkdir(mode=0o700)
        except FileExistsError:
            pass
        info = path.lstat()
    if not stat.S_ISDIR(info.st_mode):
        raise UnsafeCache(f"{path}: expected a non-symlink cache directory")
    check_permissions(path, info, ancestor)


def key_directory(root, parts):
    key = root
    for part in parts:
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", part):
            raise ValueError(f"invalid cache key component: {part}")
        key /= part
        cache_directory(key)
    return key


def require_writable(key):
    if not os.access(key, os.W_OK | os.X_OK):
        raise PermissionError(f"{key}: cache directory is not writable")
    return key


URL = re.compile(r"([A-Za-z][A-Za-z0-9+.-]*://)(?:[^/?#\s@]*@)?((?:\[[^\]\s]*\]|[^/?#\s@:\[]*)(?::[0-9]+|:(?=[/?#]))?)(?:[/?#](?:\S*[^\s:])?)?")


STATUS = re.compile(r"(://[^/\s]*/\.\.\.: [0-9]{3})(?![0-9])[^\n]*")


def redacted(text):
    lines = []
    in_response = False
    for line in re.findall(r"[^\n]*\n|[^\n]+", text):
        if in_response and line.startswith("\t"):
            continue
        head, marker, _ = line.partition("server response:")
        in_response = bool(marker)
        head = STATUS.sub(r"\1 [status text omitted]", URL.sub(r"\1\2/...", head))
        lines.append(head + (marker + " [omitted]\n" if marker else ""))
    return "".join(lines)


def go_output(command, **options):
    result = subprocess.run(command, capture_output=True, **options)
    sys.stderr.write(redacted(result.stderr.decode("utf-8", "replace")))
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return result.stdout


def cache_roots(go_cache):
    configured = os.environ.get("IRIS_STACK_TOOL_CACHE")
    if configured:
        return [Path(configured)]
    if not go_cache or go_cache == "off":
        raise UnsafeCache("GOCACHE is off; set IRIS_STACK_TOOL_CACHE to a private directory")
    user_cache = os.environ.get("XDG_CACHE_HOME", "")
    fallback = Path(user_cache if os.path.isabs(user_cache) else Path.home() / ".cache") / "iris-stack-tools"
    return [Path(go_cache) / "iris-stack-tools", fallback]


def checked_root(configured):
    root = Path(configured).absolute()
    if "\n" in str(root) or "\r" in str(root):
        raise UnsafeCache("cache path must fit on one output line")
    if ".." in root.parts:
        raise UnsafeCache(f"{root}: cache path contains '..'")
    for parent in reversed(root.parents):
        cache_directory(parent, ancestor=True)
    cache_directory(root)
    return root


def verified(binary, checksum, tool, version, required, go, env):
    entry = binary.parent
    receipt = entry / "SHA256"
    try:
        directory = entry.lstat()
        if not stat.S_ISDIR(directory.st_mode):
            return False
        check_permissions(entry, directory)
        for path in (receipt, binary):
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode):
                return False
            check_permissions(path, info)
            if info.st_mode & 0o200:
                return False
            if path == receipt and info.st_size != 65:
                return False
        if not os.access(binary, os.X_OK):
            return False
        if receipt.read_bytes() != f"{checksum}\n".encode("ascii") or digest(binary) != checksum:
            return False
        info = json.loads(subprocess.check_output(
            [go, "version", "-m", "-json", binary], env=env, stderr=subprocess.DEVNULL,
        ))
        target = {item["Key"]: item.get("Value") for item in info["Settings"]}
        return (
            info["Path"] == f"{MODULES[tool]}/cmd/{tool}"
            and info["Main"]["Path"] == MODULES[tool]
            and info["Main"]["Version"] == version and not info["Main"].get("Replace")
            and info["GoVersion"] == env["GOTOOLCHAIN"]
            and target.get("GOEXPERIMENT", "") == ""
            and target.get("GOFIPS140", "off") == "off"
            and all(target.get(key) == value for key, value in required.items())
        )
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError):
        return False


def offline_sumdb(configured, download):
    if configured == "off":
        return configured
    fields = configured.split()
    if not 1 <= len(fields) <= 2:
        raise ValueError("invalid GOSUMDB configuration")
    key = "sum.golang.org" if configured == "sum.golang.google.cn" else fields[0]
    name = key.partition("+")[0]
    if not all(part and part not in (".", "..") for part in name.split("/")):
        raise ValueError("invalid checksum database name")
    return f"{key} {(download / 'sumdb' / name).as_uri()}"


def resolve(tool, version, repo):
    if os.name != "posix":
        raise ValueError("POSIX cache ownership, permissions and locking are required")
    import fcntl

    if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", version):
        raise ValueError(f"invalid {tool} release: {version}")
    if not (repo / "go.mod").is_file():
        raise ValueError(f"{repo}: expected a Go module directory containing go.mod")
    env = os.environ | {"GO111MODULE": "on", "GOWORK": "off", "GOFLAGS": " ", "CGO_ENABLED": "0",
                        "GOEXPERIMENT": ",", "GOFIPS140": "off", "GOPRIVATE": " "}
    for name in {"GOOS", "GOARCH", "GOWASM"} | {name for name, _ in BASELINES.values()}:
        env.pop(name, None)
    settings = json.loads(go_output(
        ["go", "env", "-json", "GOPROXY", "GOMODCACHE", "GOSUMDB", "GONOSUMDB", "GOINSECURE", "GONOPROXY", "GOCACHE",
         "GOAUTH", "GOVCS"],
        cwd="/", env=env | {"GOTOOLCHAIN": "local"},
    ))
    if settings["GOPROXY"].strip() == "off":
        download = Path(settings["GOMODCACHE"]) / "cache/download"
        env |= {"GOPROXY": download.as_uri(), "GONOPROXY": "none",
                "GOSUMDB": offline_sumdb(settings["GOSUMDB"], download)}
    settings |= json.loads(go_output(
        ["go", "env", "-json", "GOVERSION", "GOROOT", "GOHOSTOS", "GOHOSTARCH"], cwd=repo, env=env,
    ))
    toolchain = settings["GOVERSION"]
    host_os, host_arch = settings["GOHOSTOS"], settings["GOHOSTARCH"]
    go = Path(settings["GOROOT"]) / "bin/go"
    required = {"GOOS": host_os, "GOARCH": host_arch, "CGO_ENABLED": "0"}
    cpu_key = "default"
    if baseline := BASELINES.get(host_arch):
        name, value = baseline
        required[name] = value
        cpu_key = f"{name}-{value}"
    env = {name: value for name, value in os.environ.items()
           if not re.fullmatch(r"GO[A-Z0-9]+", name) and not name.startswith("CGO_")}
    env.update(required, GOROOT=settings["GOROOT"], GOTOOLCHAIN=toolchain, GOENV="off", GOWORK="off",
               GO111MODULE="on", GOFLAGS="", GOEXPERIMENT="", GOFIPS140="off")
    for name in ("GOSUMDB", "GOINSECURE", "GOPROXY", "GOAUTH", "GOVCS"):
        env[name] = settings[name]
    for name in ("GONOSUMDB", "GONOPROXY"):
        env[name] = "" if settings[name] == " " else settings[name]
    directive = json.loads(go_output(
        [go, "mod", "edit", "-json", "go.mod"], cwd=repo, env=env,
    )).get("Go") or "1.16"
    if go_version_key(toolchain) < go_version_key(directive):
        raise ValueError(f"{repo}/go.mod requires Go {directive}, resolved {toolchain}")
    parts = ("go-install-v2", tool, version, toolchain, f"{host_os}-{host_arch}", cpu_key, "cgo-0",
             "goexperiment-default", "gofips140-off")
    roots = cache_roots(settings["GOCACHE"])
    base_env = env
    for position, cache_root in enumerate(roots):
        last = position == len(roots) - 1
        try:
            key = key_directory(checked_root(cache_root), parts)
        except (UnsafeCache, OSError) as error:
            if last:
                raise
            print(f"go-tooling: {error}; trying the next cache root", file=sys.stderr)
            continue
        root = key.parents[len(parts) - 1]
        env = dict(base_env)
        env["GOCACHE"] = settings["GOCACHE"] if settings["GOCACHE"] not in {"", "off"} else str(root / "go-build")

        try:
            lock = os.open(key, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        except OSError as error:
            if last:
                raise
            print(f"go-tooling: {error}; trying the next cache root", file=sys.stderr)
            continue
        try:
            fcntl.flock(lock, fcntl.LOCK_EX)
            for entry in sorted(key.glob("build-*")):
                match = re.fullmatch(r"build-([a-f0-9]{64})-[0-9]+", entry.name)
                if match and verified(entry / tool, match[1], tool, version, required, go, env):
                    return entry / tool
                print(f"go-tooling: invalid cache entry: {entry}", file=sys.stderr)
            if not last and not os.access(key, os.W_OK | os.X_OK):
                print(f"go-tooling: {key}: cache directory is not writable; trying the next cache root", file=sys.stderr)
                continue
            require_writable(key)
            try:
                os.makedirs(env["GOCACHE"], mode=0o700, exist_ok=True)
                require_writable(env["GOCACHE"])
            except OSError as error:
                env["GOCACHE"] = str(root / "go-build")
                print(f"go-tooling: {error}; building with GOCACHE={env['GOCACHE']}", file=sys.stderr)
            for stale in key.glob(".build-*"):
                if stale.is_dir() and not stale.is_symlink():
                    remove_stale_build(stale)

            proxy = env["GOPROXY"]
            download = Path(settings["GOMODCACHE"]) / "cache/download"
            local_proxy = download.as_uri()
            if proxy.strip() == "off":
                env.update(GOPROXY=local_proxy, GONOPROXY="none", GOSUMDB=offline_sumdb(env["GOSUMDB"], download))
            else:
                env["GOPROXY"] = f"{local_proxy},{proxy}"

            with tempfile.TemporaryDirectory(dir=key, prefix=".build-") as temporary:
                stage = Path(temporary)
                cache_directory(stage)
                package = f"{MODULES[tool]}/cmd/{tool}@{version}"
                print(f"go-tooling: installing {package} with {toolchain}", file=sys.stderr)
                modcache = stage / "modcache"
                modcache.mkdir(mode=0o700)
                install_env = env | {"GOBIN": temporary, "GOMODCACHE": str(modcache),
                                     "GOPATH": str(stage / "gopath")}
                sys.stderr.write(redacted(go_output([go, "install", package], cwd=stage, env=install_env)
                                          .decode("utf-8", "replace")))
                binary = stage / tool
                if not stat.S_ISREG(binary.lstat().st_mode):
                    raise ValueError(f"{package}: expected a regular executable")
                binary.chmod(0o500)
                check_permissions(binary, binary.lstat())
                checksum = digest(binary)
                receipt = stage / "SHA256"
                with receipt.open("x", encoding="ascii") as stream:
                    stream.write(checksum + "\n")
                receipt.chmod(0o400)
                if not verified(binary, checksum, tool, version, required, go, env):
                    raise ValueError(f"{package}: installed executable failed verification")
                for generation in itertools.count():
                    entry = key / f"build-{checksum}-{generation}"
                    try:
                        entry.mkdir(mode=0o700)
                    except FileExistsError:
                        continue
                    cache_directory(entry)
                    os.link(receipt, entry / "SHA256")
                    os.link(binary, entry / tool)
                    return entry / tool
        finally:
            os.close(lock)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(allow_abbrev=False)
    parser.add_argument("--tool", choices=MODULES, required=True)
    parser.add_argument("--version", metavar="vX.Y.Z", required=True)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    args = parser.parse_args()
    try:
        print(resolve(args.tool, args.version, args.repo))
    except (OSError, ValueError, UnsafeCache, subprocess.CalledProcessError) as exc:
        sys.exit(f"go-tooling: {exc}")
