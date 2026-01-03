#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_DIR="${ROOT_DIR}/supervisor"
PATCH_FILE="${ROOT_DIR}/rootfs/patches/hassio-supervisor.patch"
TARGET_DIR="/usr/src/supervisor"

if [[ ! -d "${SUPERVISOR_DIR}" ]]; then
  echo "error: supervisor directory not found at ${SUPERVISOR_DIR}; update git submodules." >&2
  exit 1
fi

if ! git -C "${SUPERVISOR_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: ${SUPERVISOR_DIR} is not a git work tree" >&2
  exit 1
fi

mkdir -p "$(dirname "${PATCH_FILE}")"

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
