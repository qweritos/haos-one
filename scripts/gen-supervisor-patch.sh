#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_DIR="${ROOT_DIR}/supervisor"
PATCH_DIR="${ROOT_DIR}/rootfs/patches/supervisor"
TARGET_DIR="/usr/src/supervisor"
PATCH_NAME="${1:-hassio-supervisor.patch}"
PATCH_FILE="${PATCH_DIR}/${PATCH_NAME}"

if [[ "${PATCH_NAME}" != *.patch ]]; then
  PATCH_FILE="${PATCH_FILE}.patch"
fi

if [[ ! -d "${SUPERVISOR_DIR}" ]]; then
  echo "error: supervisor directory not found at ${SUPERVISOR_DIR}; update git submodules." >&2
  exit 1
fi

if ! git -C "${SUPERVISOR_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: ${SUPERVISOR_DIR} is not a git work tree" >&2
  exit 1
fi

mkdir -p "${PATCH_DIR}"

if ! git -C "${SUPERVISOR_DIR}" diff --quiet HEAD --; then
  git -C "${SUPERVISOR_DIR}" diff --binary \
    --src-prefix="a${TARGET_DIR}/" \
    --dst-prefix="b${TARGET_DIR}/" \
    HEAD -- > "${PATCH_FILE}"
  echo "Wrote patch to ${PATCH_FILE}"
else
  : > "${PATCH_FILE}"
  echo "No supervisor changes found; wrote empty patch to ${PATCH_FILE}"
fi
