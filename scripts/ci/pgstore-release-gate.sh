#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}/postgres"

for tool in podman jq go; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "pgstore-release-gate: ${tool} is required" >&2
    exit 1
  fi
done

POSTGRES_IMAGE='docker.io/library/postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
container_id=''
result_file="$(mktemp)"
cleanup() {
  local exit_status=$?
  trap - EXIT INT TERM

  if [[ -n "${container_id}" ]]; then
    if ! podman rm -f -v "${container_id}" >/dev/null; then
      echo 'pgstore-release-gate: disposable PostgreSQL cleanup failed' >&2
      exit_status=1
    fi
  fi
  if ! rm -f "${result_file}"; then
    exit_status=1
  fi
  exit "${exit_status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

platform="$(podman info --format '{{.Host.OS}}/{{.Host.Arch}}')"
container_id="$(podman run --rm -d --platform "${platform}" -p 127.0.0.1::5432 \
  -e POSTGRES_HOST_AUTH_METHOD=trust "${POSTGRES_IMAGE}")"
port_binding="$(podman port "${container_id}" 5432/tcp)"
if [[ ! "${port_binding}" =~ ^127\.0\.0\.1:([0-9]+)$ ]]; then
  echo 'pgstore-release-gate: unexpected PostgreSQL port binding' >&2
  exit 1
fi
port="${BASH_REMATCH[1]}"

ready=false
for _ in {1..30}; do
  if podman exec "${container_id}" pg_isready -q -U postgres; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "${ready}" != true ]]; then
  echo 'pgstore-release-gate: disposable PostgreSQL did not become ready' >&2
  exit 1
fi

export TEST_DATABASE_URL="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
export ALLOW_EXTERNAL_TEST_DB=true

if ! go test -race -count=1 -timeout=5m -json ./... >"${result_file}"; then
  echo 'pgstore-release-gate: PostgreSQL suite failed' >&2
  jq -r 'select(.Action == "fail" or .Action == "skip") | [.Action, (.Test // .Package)] | @tsv' "${result_file}" >&2 || true
  exit 1
fi

if ! jq -e -s '
  . as $events |
  ([$events[] | select(.Action == "skip")] | length) == 0 and
  all(["TestRunAgainstPostgres", "TestReferenceSchemaIsRepeatable",
       "TestIntegration_OpenPool", "TestIntegration_OpenPoolDSN",
       "TestIntegration_AcquirePolicyControlContention",
       "TestIntegrationServerCancellationPreservesSQLStateAndConnection"][];
    . as $name | ([$events[] | select(.Action == "pass" and .Test == $name)] | length) == 1)
' "${result_file}" >/dev/null; then
  echo 'pgstore-release-gate: required PostgreSQL case did not pass or a test skipped' >&2
  exit 1
fi

echo 'pgstore-release-gate: required cases passed, skip=0'
