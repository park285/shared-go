#!/usr/bin/env bash
set -euo pipefail

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.12.2}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.4.0}"

go_bin_tool() {
  local tool="$1" bin
  local gobin
  gobin="$(go env GOBIN)"
  if [[ -n "${gobin}" && -x "${gobin}/${tool}" ]]; then
    printf '%s/%s\n' "${gobin}" "${tool}"
    return
  fi

  local gopath
  gopath="$(go env GOPATH)"
  if [[ -n "${gopath}" && -x "${gopath}/bin/${tool}" ]]; then
    printf '%s/bin/%s\n' "${gopath}" "${tool}"
    return
  fi

  bin="$(command -v "${tool}" || true)"
  if [[ -n "${bin}" ]]; then
    printf '%s\n' "${bin}"
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
  local bin version_output marker
  marker="version ${GOLANGCI_LINT_VERSION#v}"
  bin="$(go_bin_tool golangci-lint)"
  if [[ -z "${bin}" ]] || [[ "$("${bin}" version 2>/dev/null || true)" != *"${marker}"* ]]; then
    echo "[GO TOOLING] Installing golangci-lint@${GOLANGCI_LINT_VERSION}" >&2
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
    bin="$(go_tool_install_path golangci-lint)"
  fi
  version_output="$("${bin}" version 2>/dev/null || true)"
  [[ "${version_output}" == *"${marker}"* ]] || { echo "expected golangci-lint ${GOLANGCI_LINT_VERSION}, got: ${version_output}" >&2; exit 1; }
  printf '%s\n' "${bin}"
}

ensure_govulncheck() {
  local bin version_output marker
  marker="govulncheck@${GOVULNCHECK_VERSION}"
  bin="$(go_bin_tool govulncheck)"
  if [[ -z "${bin}" ]] || [[ "$("${bin}" -version 2>/dev/null || true)" != *"${marker}"* ]]; then
    echo "[GO TOOLING] Installing govulncheck@${GOVULNCHECK_VERSION}" >&2
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
    bin="$(go_tool_install_path govulncheck)"
  fi
  version_output="$("${bin}" -version 2>/dev/null || true)"
  [[ "${version_output}" == *"${marker}"* ]] || { echo "expected govulncheck ${GOVULNCHECK_VERSION}, got: ${version_output}" >&2; exit 1; }
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
