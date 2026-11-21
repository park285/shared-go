#!/usr/bin/env bash
set -euo pipefail

readonly AST_GREP_VERSION=0.51.3
readonly TLA2TOOLS_VERSION=1.7.4
readonly TLA2TOOLS_SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
readonly ELAN_VERSION=4.3.0
readonly TLC_TIMEOUT_SECONDS="${TLC_TIMEOUT_SECONDS:-600}"

CACHE_ROOT="${IRIS_STACK_TOOL_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/iris-stack-tools}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd -P)"

usage() {
  cat >&2 <<'EOF'
Usage: verify-tools.sh <command> [args...]
  ast-grep [args...]        pinned ast-grep CLI
  scan                      stack ast-grep rules plus the SQL, TLA+, Lean and embedded Python comment ban
  tla2tools-path            print the pinned tla2tools.jar path
  tlc SPEC.tla [CONFIG.cfg] run TLC on one model (default config: SPEC with .cfg)
  lake [args...]            run Lake under the pinned elan
  formal                    run verification/tla/<Model>/<Model>.{tla,cfg}; lake build --wfail and the axiom audit in verification/lean/<Project>
EOF
  exit 2
}

fail() {
  printf 'verify-tools: %s\n' "$*" >&2
  exit 1
}

machine() {
  case "$(uname -m)" in
    x86_64 | amd64) printf 'x86_64\n' ;;
    aarch64 | arm64) printf 'aarch64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

ast_grep_sha256() {
  case "$1" in
    x86_64) printf '%s\n' 22803eb0c5bf83164fb7fd76d4721d355ecf0c8466ef47342c1ec7d80897ca47 ;;
    aarch64) printf '%s\n' 5b44fd68e78744934b359b07badce347d4197ed50b341d50f1f99d056ecedb08 ;;
    *) return 1 ;;
  esac
}

elan_sha256() {
  case "$1" in
    x86_64) printf '%s\n' 0c71472fa3ac34871dc1e2539993cca222d7656b42a1469f8f8c97c22f9adab2 ;;
    aarch64) printf '%s\n' a1a3359c09e2a9f016bd241393ca79ad5f88eb1e4d45bb0215d799f3e55f08a3 ;;
    *) return 1 ;;
  esac
}

fetch_verified() {
  local url="$1" dest="$2" sha="$3"
  curl -fsSL -o "$dest" "$url" || { printf 'verify-tools: download failed: %s\n' "$url" >&2; return 1; }
  printf '%s  %s\n' "$sha" "$dest" | sha256sum --check --status ||
    { printf 'verify-tools: sha256 mismatch: %s\n' "$url" >&2; return 1; }
}

new_stage() {
  (umask 077 && mkdir -p -- "$1") && mktemp -d -- "$1/.stage.XXXXXX"
}

ast_grep_bin() {
  local arch sha dir stage
  arch="$(machine)"
  dir="$CACHE_ROOT/ast-grep/$AST_GREP_VERSION-$arch"
  if [[ ! -x "$dir/ast-grep" ]]; then
    sha="$(ast_grep_sha256 "$arch")" || return 1
    stage="$(new_stage "$CACHE_ROOT/ast-grep")" || return 1
    if ! fetch_verified "https://github.com/ast-grep/ast-grep/releases/download/$AST_GREP_VERSION/app-$arch-unknown-linux-gnu.zip" "$stage/app.zip" "$sha" ||
      ! unzip -q "$stage/app.zip" ast-grep -d "$stage" ||
      ! chmod 0755 "$stage/ast-grep" ||
      ! mkdir -p -- "$dir" ||
      ! mv -f -- "$stage/ast-grep" "$dir/ast-grep"; then
      rm -r -f -- "$stage"
      return 1
    fi
    rm -r -f -- "$stage"
  fi
  printf '%s\n' "$dir/ast-grep"
}

tla2tools_jar() {
  local dir="$CACHE_ROOT/tla2tools/$TLA2TOOLS_VERSION" stage
  if [[ ! -f "$dir/tla2tools.jar" ]]; then
    stage="$(new_stage "$CACHE_ROOT/tla2tools")" || return 1
    if ! fetch_verified "https://github.com/tlaplus/tlaplus/releases/download/v$TLA2TOOLS_VERSION/tla2tools.jar" "$stage/tla2tools.jar" "$TLA2TOOLS_SHA256" ||
      ! mkdir -p -- "$dir" ||
      ! mv -f -- "$stage/tla2tools.jar" "$dir/tla2tools.jar"; then
      rm -r -f -- "$stage"
      return 1
    fi
    rm -r -f -- "$stage"
  fi
  printf '%s\n' "$dir/tla2tools.jar"
}

elan_home() {
  local arch sha home stage
  arch="$(machine)"
  home="$CACHE_ROOT/elan/$ELAN_VERSION"
  if [[ ! -x "$home/bin/lake" ]]; then
    sha="$(elan_sha256 "$arch")" || return 1
    stage="$(new_stage "$CACHE_ROOT/elan")" || return 1
    if ! fetch_verified "https://github.com/leanprover/elan/releases/download/v$ELAN_VERSION/elan-$arch-unknown-linux-gnu.tar.gz" "$stage/elan.tar.gz" "$sha" ||
      ! tar -xzf "$stage/elan.tar.gz" -C "$stage" elan-init ||
      ! ELAN_HOME="$stage/home" "$stage/elan-init" -y --no-modify-path --default-toolchain none >/dev/null ||
      [[ ! -x "$stage/home/bin/lake" ]] ||
      ! { mv -T -- "$stage/home" "$home" 2>/dev/null || [[ -x "$home/bin/lake" ]]; }; then
      rm -r -f -- "$stage"
      return 1
    fi
    rm -r -f -- "$stage"
  fi
  printf '%s\n' "$home"
}

tlc_abort() {
  local pid
  for pid in $(jobs -p); do
    kill -TERM "$pid" 2>/dev/null || true
  done
  wait || true
  [[ -z "${tlc_meta:-}" ]] || rm -r -f -- "$tlc_meta"
  exit "$1"
}

run_tlc() {
  local spec config jar status=0
  spec="$(realpath -e -- "$1")" || fail "missing spec: $1"
  if [[ $# -ge 2 ]]; then
    config="$(realpath -e -- "$2")" || fail "missing config: $2"
  else
    config="$(realpath -e -- "${spec%.tla}.cfg")" || fail "missing config: ${spec%.tla}.cfg"
  fi
  jar="$(tla2tools_jar)" || return 1
  tlc_meta=""
  trap 'tlc_abort 130' INT
  trap 'tlc_abort 143' TERM
  tlc_meta="$(mktemp -d)" || return 1
  (cd "$(dirname -- "$spec")" && exec timeout --foreground "$TLC_TIMEOUT_SECONDS" java -XX:+UseParallelGC -cp "$jar" tlc2.TLC \
    -workers "${TLC_WORKERS:-auto}" -metadir "$tlc_meta" -config "$config" "$(basename -- "$spec")") &
  wait "$!" || status=$?
  rm -r -f -- "$tlc_meta"
  tlc_meta=""
  trap - INT TERM
  return "$status"
}

run_lake() {
  local home
  home="$(elan_home)" || return 1
  ELAN_HOME="$home" PATH="$home/bin:$PATH" "$home/bin/lake" "$@"
}

python_bin() {
  if [[ -n "${CI_PYTHON_BIN:-}" ]]; then
    printf '%s\n' "$CI_PYTHON_BIN"
  elif [[ -f "$REPO_ROOT/scripts/ci/python-runtime.sh" ]]; then
    (
      # shellcheck source=/dev/null
      source "$REPO_ROOT/scripts/ci/python-runtime.sh" && repo_python_init >&2 && printf '%s\n' "$CI_PYTHON_BIN"
    )
  elif [[ -f "$REPO_ROOT/tools/ci/python-runtime.sh" ]]; then
    (
      # shellcheck source=/dev/null
      source "$REPO_ROOT/tools/ci/python-runtime.sh" && stack_python_init >&2 && printf '%s\n' "$CI_PYTHON_BIN"
    )
  elif [[ -f "$REPO_ROOT/.python-version" ]]; then
    printf 'verify-tools: %s pins Python in .python-version but has no python-runtime.sh helper\n' "$REPO_ROOT" >&2
    return 1
  else
    command -v python3 || { printf 'verify-tools: python3 not found\n' >&2; return 1; }
  fi
}

source_files() {
  local mode="$1"
  if [[ "$mode" == tracked ]]; then
    git -C "$REPO_ROOT" ls-files -z -s
  else
    git -C "$REPO_ROOT" ls-files -z -s --cached
    git -C "$REPO_ROOT" ls-files -z --others --exclude-standard | awk -v RS='\0' -v ORS='\0' '{ print "100644 0 0\t" $0 }'
  fi | awk -v RS='\0' -v ORS='\0' '$1 != "160000" && $1 != "120000" { sub(/^[^\t]*\t/, ""); print }' | sort -z -u
}

regular_source_files() {
  local path parent
  while IFS= read -r -d '' path; do
    [[ -f "$path" && ! -L "$path" ]] || continue
    parent=$path
    while [[ "$parent" == */* ]]; do
      parent=${parent%/*}
      [[ ! -L "$parent" ]] || continue 2
    done
    printf '%s\0' "$path"
  done
}

run_scan() {
  local bin config mode list python lexical_args=() status=0
  bin="$(ast_grep_bin)" || return 1
  python="$(python_bin)" || return 1
  if [[ -f "$REPO_ROOT/sgconfig.yml" ]]; then
    config="$REPO_ROOT/sgconfig.yml"
    mode=all
  else
    config="$SCRIPT_DIR/ast-grep/sgconfig.yml"
    mode=tracked
    lexical_args=(--tracked-only)
  fi
  list="$(mktemp)"
  (cd "$REPO_ROOT" && source_files "$mode" | regular_source_files) > "$list" || { rm -f -- "$list"; return 1; }
  (cd "$REPO_ROOT" && xargs -0 -r "$bin" scan --config "$config" --report-style short -- < "$list") || status=1
  "$python" "$SCRIPT_DIR/lexical-no-comments.py" --root "$REPO_ROOT" --heredocs-from "$list" --ast-grep "$bin" --config "$config" "${lexical_args[@]}" || status=1
  rm -f -- "$list"
  return "$status"
}

lean_modules() {
  local project="${1%/}" lib="${1%/}/.lake/build/lib/lean" module root roots=() declared
  [[ -d "$lib" ]] || return 0
  declared="$(grep -c -E '(^|[^[:alnum:]_])srcDir([^[:alnum:]_]|$)' "$project/lakefile.toml" || true)"
  mapfile -t roots < <(sed -n -E "s/^[[:space:]]*srcDir[[:space:]]*=[[:space:]]*([\"'])([^\"']*)\\1[[:space:]]*\$/\\2/p" "$project/lakefile.toml")
  if ((declared != ${#roots[@]})); then
    printf 'verify-tools: %s/lakefile.toml declares srcDir in a form other than srcDir = "dir"\n' "${project#"$REPO_ROOT"/}" >&2
    return 1
  fi
  ((${#roots[@]})) || roots=(.)
  while IFS= read -r module; do
    for root in "${roots[@]}"; do
      if [[ -f "$project/$root/${module//.//}.lean" ]]; then
        printf '%s\n' "$module"
        break
      fi
    done
  done < <(cd "$lib" && find . -type f -name '*.olean' -print | sed -e 's|^\./||' -e 's|\.olean$||' -e 's|/|.|g' | sort)
}

run_formal() {
  local model name project modules=() ran=0 status=0
  for model in "$REPO_ROOT"/verification/tla/*/; do
    [[ -d "$model" ]] || continue
    name="$(basename -- "$model")"
    if [[ ! -f "$model$name.tla" || ! -f "$model$name.cfg" ]]; then
      printf 'verify-tools: %s needs %s.tla and %s.cfg\n' "${model#"$REPO_ROOT"/}" "$name" "$name" >&2
      status=1
      continue
    fi
    ran=1
    printf 'verify-tools: TLC %s%s.tla\n' "${model#"$REPO_ROOT"/}" "$name"
    run_tlc "$model$name.tla" "$model$name.cfg" || status=1
  done
  for project in "$REPO_ROOT"/verification/lean/*/; do
    [[ -d "$project" ]] || continue
    if [[ ! -f "$project/lakefile.toml" ]]; then
      printf 'verify-tools: %s has no lakefile.toml\n' "${project#"$REPO_ROOT"/}" >&2
      status=1
      continue
    fi
    ran=1
    printf 'verify-tools: lake build --wfail %s\n' "${project#"$REPO_ROOT"/}"
    if (cd "$project" && run_lake build --wfail); then
      mapfile -t modules < <(lean_modules "$project")
      if ((${#modules[@]} == 0)); then
        printf 'verify-tools: %s has no built project modules to audit\n' "${project#"$REPO_ROOT"/}" >&2
        status=1
      else
        (cd "$project" && run_lake env lean --run "$SCRIPT_DIR/lean-axiom-audit.lean" "${modules[@]}") || status=1
      fi
    else
      status=1
    fi
  done
  ((ran == 1 || status != 0)) || printf 'verify-tools: no formal verification under %s/verification\n' "$REPO_ROOT"
  return "$status"
}

[[ $# -ge 1 ]] || usage
command="$1"
shift
case "$command" in
  ast-grep) bin="$(ast_grep_bin)" || exit 1; exec "$bin" "$@" ;;
  scan) [[ $# -eq 0 ]] || usage; run_scan ;;
  tla2tools-path) [[ $# -eq 0 ]] || usage; tla2tools_jar ;;
  tlc) [[ $# -ge 1 && $# -le 2 ]] || usage; run_tlc "$@" ;;
  lake) run_lake "$@" ;;
  formal) [[ $# -eq 0 ]] || usage; run_formal ;;
  *) usage ;;
esac
