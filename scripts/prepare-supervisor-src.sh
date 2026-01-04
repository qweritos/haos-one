#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_DIR="${ROOT_DIR}/supervisor"
STABLE_URL="https://raw.githubusercontent.com/home-assistant/version/refs/heads/master/stable.json"
PATCH_FILE="${ROOT_DIR}/rootfs/patches/hassio-supervisor.patch"

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

apply_patch() {
  if [[ ! -f "${PATCH_FILE}" ]]; then
    echo "patch file not found at ${PATCH_FILE}; skipping" >&2
    return 0
  fi

  if [[ ! -s "${PATCH_FILE}" ]]; then
    echo "patch file is empty; skipping" >&2
    return 0
  fi

  if git -C "${SUPERVISOR_DIR}" apply -p4 --check "${PATCH_FILE}" >/dev/null 2>&1; then
    git -C "${SUPERVISOR_DIR}" apply -p4 "${PATCH_FILE}"
    echo "Applied patch from ${PATCH_FILE}"
    return 0
  fi

  echo "Patch does not apply cleanly; skipping" >&2
  return 0
}

supervisor_tag=$(curl -fsSL "${STABLE_URL}" | jq -r '.supervisor')
if [[ -z "${supervisor_tag}" || "${supervisor_tag}" == "null" ]]; then
  echo "error: supervisor tag missing in stable.json" >&2
  exit 1
fi

echo "Using supervisor tag: ${supervisor_tag}"

git -C "${SUPERVISOR_DIR}" fetch --tags origin

git -C "${SUPERVISOR_DIR}" checkout "refs/tags/${supervisor_tag}"

apply_patch

echo "Supervisor submodule now at: $(git -C "${SUPERVISOR_DIR}" rev-parse --short HEAD)"
