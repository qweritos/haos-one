#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_DIR="${ROOT_DIR}/supervisor"
PATCH_DIR="${ROOT_DIR}/rootfs/patches/supervisor"
SUPERVISOR_IMAGE="ghcr.io/home-assistant/amd64-hassio-supervisor:latest"

apply_patch() {
  if [[ ! -d "${PATCH_DIR}" ]]; then
    echo "patch directory not found at ${PATCH_DIR}; skipping" >&2
    return 0
  fi

  local patches=()
  if [[ "$#" -gt 0 ]]; then
    for name in "$@"; do
      local patch="${PATCH_DIR}/${name}"
      if [[ "${patch}" != *.patch ]]; then
        patch="${patch}.patch"
      fi
      patches+=("${patch}")
    done
  else
    shopt -s nullglob
    patches=("${PATCH_DIR}"/*.patch)
    shopt -u nullglob
  fi

  if [[ "${#patches[@]}" -eq 0 ]]; then
    echo "no patch files found in ${PATCH_DIR}; skipping" >&2
    return 0
  fi

  for patch in "${patches[@]}"; do
    if [[ ! -s "${patch}" ]]; then
      echo "patch file is empty; skipping ${patch}" >&2
      continue
    fi

    if patch -d "${SUPERVISOR_DIR}" -p1 --dry-run < "${patch}"; then
      patch -d "${SUPERVISOR_DIR}" -p1 < "${patch}"
      echo "Applied patch from ${patch}"
      continue
    fi

    echo "Patch does not apply cleanly; skipping ${patch}" >&2
  done
}

mkdir -p "${SUPERVISOR_DIR}"

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 1
fi
if ! command -v patch >/dev/null 2>&1; then
  echo "error: patch is required" >&2
  exit 1
fi

echo "Pulling supervisor image: ${SUPERVISOR_IMAGE}"
docker pull "${SUPERVISOR_IMAGE}" >/dev/null

CONTAINER_ID="$(docker create "${SUPERVISOR_IMAGE}")"
trap 'docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true' EXIT

rm -rf "${SUPERVISOR_DIR}"/*
echo "Extracting supervisor filesystem to ${SUPERVISOR_DIR}"
docker export "${CONTAINER_ID}" | tar -C "${SUPERVISOR_DIR}" -xf -

apply_patch "$@"

echo "Supervisor filesystem unpacked into ${SUPERVISOR_DIR}"
