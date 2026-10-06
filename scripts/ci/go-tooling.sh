#!/usr/bin/env bash
set -euo pipefail

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.14.0}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.8.0}"

go_bin_tool() {
  local tool="$1" minimum="${2:-}" probe="${3:-}" pattern=""
  local gobin gopath bin output invalid_bin=""
  local required_major required_minor required_patch
  local -a candidates=()

  case "${tool}" in
    golangci-lint)
      minimum="${minimum:-${GOLANGCI_LINT_VERSION}}"
      probe="${probe:-version}"
      pattern='version[[:space:]]+([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
      ;;
    govulncheck)
      minimum="${minimum:-${GOVULNCHECK_VERSION}}"
      probe="${probe:--version}"
      pattern='govulncheck@v([0-9]+)\.([0-9]+)\.([0-9]+)($|[[:space:]])'
      ;;
  esac
  if [[ -n "${pattern}" ]]; then
    [[ "${minimum}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
      echo "invalid minimum version for ${tool}: ${minimum}" >&2
      return 1
    }
    IFS=. read -r required_major required_minor required_patch <<<"${minimum#v}"
  fi

  gobin="$(go env GOBIN)" || return
  gopath="$(go env GOPATH)" || return
  [[ -z "${gobin}" ]] || candidates+=("${gobin}/${tool}")
  [[ -z "${gopath}" ]] || candidates+=("${gopath}/bin/${tool}")
  bin="$(command -v "${tool}" || true)"
  [[ -z "${bin}" ]] || candidates+=("${bin}")

  for bin in "${candidates[@]}"; do
    [[ -x "${bin}" ]] || continue
    if [[ -z "${pattern}" ]]; then
      printf '%s\n' "${bin}"
      return
    fi
    if ! output="$("${bin}" "${probe}" 2>/dev/null)" || [[ ! "${output}" =~ ${pattern} ]]; then
      invalid_bin="${bin}"
      continue
    fi
    if (( 10#${BASH_REMATCH[1]} > 10#${required_major}
      || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} > 10#${required_minor})
      || (10#${BASH_REMATCH[1]} == 10#${required_major} && 10#${BASH_REMATCH[2]} == 10#${required_minor}
        && 10#${BASH_REMATCH[3]} >= 10#${required_patch}) )); then
      printf '%s\n' "${bin}"
      return
    fi
  done
  if [[ -n "${invalid_bin}" ]]; then
    echo "cannot verify ${tool} version: ${invalid_bin}" >&2
    return 1
  fi
}

go_tool_install_path() {
  local tool="$1" gobin gopath
  gobin="$(go env GOBIN)"
  if [[ -n "${gobin}" ]]; then
    printf '%s/%s\n' "${gobin}" "${tool}"
    return
  fi
  gopath="$(go env GOPATH)"
  [[ -n "${gopath}" ]] || { echo "GOPATH is empty; cannot locate ${tool}" >&2; exit 1; }
  printf '%s/bin/%s\n' "${gopath}" "${tool}"
}

ensure_golangci_lint() {
  local bin
  bin="$(go_bin_tool golangci-lint)" || return
  if [[ -z "${bin}" ]]; then
    echo "[GO TOOLING] Installing golangci-lint@${GOLANGCI_LINT_VERSION}" >&2
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" || return
    bin="$(go_bin_tool golangci-lint)" || return
  fi
  [[ -n "${bin}" ]] || { echo "golangci-lint >= ${GOLANGCI_LINT_VERSION} is required" >&2; return 1; }
  printf '%s\n' "${bin}"
}

ensure_govulncheck() {
  local bin
  bin="$(go_bin_tool govulncheck)" || return
  if [[ -z "${bin}" ]]; then
    echo "[GO TOOLING] Installing govulncheck@${GOVULNCHECK_VERSION}" >&2
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" || return
    bin="$(go_bin_tool govulncheck)" || return
  fi
  [[ -n "${bin}" ]] || { echo "govulncheck >= ${GOVULNCHECK_VERSION} is required" >&2; return 1; }
  printf '%s\n' "${bin}"
}

tool="${1:-}"
[[ -n "${tool}" ]] || { echo "usage: $0 <golangci-lint|govulncheck> [args...]" >&2; exit 2; }
shift
case "${tool}" in
  golangci-lint) bin="$(ensure_golangci_lint)" ;;
  govulncheck) bin="$(ensure_govulncheck)" ;;
  *) echo "unsupported Go tool: ${tool}" >&2; exit 2 ;;
esac
exec "${bin}" "$@"
