#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_DIR="${ROOT_DIR}/supervisor"
STABLE_URL="https://raw.githubusercontent.com/home-assistant/version/refs/heads/master/stable.json"

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required" >&2
  exit 1
fi

if [[ ! -d "${SUPERVISOR_DIR}" ]]; then
  echo "error: supervisor directory not found at ${SUPERVISOR_DIR}" >&2
  exit 1
fi

if ! git -C "${SUPERVISOR_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: ${SUPERVISOR_DIR} is not a git work tree" >&2
  exit 1
fi

supervisor_tag=$(curl -fsSL "${STABLE_URL}" | jq -r '.supervisor')
if [[ -z "${supervisor_tag}" || "${supervisor_tag}" == "null" ]]; then
  echo "error: supervisor tag missing in stable.json" >&2
  exit 1
fi

echo "Using supervisor tag: ${supervisor_tag}"

git -C "${SUPERVISOR_DIR}" fetch --tags origin

git -C "${SUPERVISOR_DIR}" checkout "refs/tags/${supervisor_tag}"

echo "Supervisor submodule now at: $(git -C "${SUPERVISOR_DIR}" rev-parse --short HEAD)"
