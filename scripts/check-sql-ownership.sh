#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
cd "${ROOT_DIR}"

"${CI_PYTHON_BIN}" scripts/check-sql-ownership.py
