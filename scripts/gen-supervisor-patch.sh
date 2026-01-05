#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SUPERVISOR_ROOTFS="${ROOT_DIR}/supervisor"
PATCH_DIR="${ROOT_DIR}/rootfs/patches/supervisor"
TARGET_DIR="/usr/src/supervisor"
PATCH_NAME="${1:-hassio-supervisor.patch}"
PATCH_FILE="${PATCH_DIR}/${PATCH_NAME}"
SUPERVISOR_IMAGE="ghcr.io/home-assistant/amd64-hassio-supervisor:latest"

if [[ "${PATCH_NAME}" != *.patch ]]; then
  PATCH_FILE="${PATCH_FILE}.patch"
fi

if [[ ! -d "${SUPERVISOR_ROOTFS}" ]]; then
  echo "error: supervisor rootfs not found at ${SUPERVISOR_ROOTFS}" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 1
fi
if ! command -v diff >/dev/null 2>&1; then
  echo "error: diff is required" >&2
  exit 1
fi

mkdir -p "${PATCH_DIR}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
IMAGE_ROOTFS="${TMP_DIR}/image"
IMAGE_SRC="${IMAGE_ROOTFS}${TARGET_DIR}"
LOCAL_SRC="${SUPERVISOR_ROOTFS}${TARGET_DIR}"

echo "Pulling supervisor image: ${SUPERVISOR_IMAGE}"
docker pull "${SUPERVISOR_IMAGE}" >/dev/null

CONTAINER_ID="$(docker create "${SUPERVISOR_IMAGE}")"
trap 'docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true' EXIT

mkdir -p "${IMAGE_ROOTFS}"
docker export "${CONTAINER_ID}" | tar -C "${IMAGE_ROOTFS}" -xf -

if [[ ! -d "${IMAGE_SRC}" ]]; then
  echo "error: ${TARGET_DIR} not found in image rootfs" >&2
  exit 1
fi

if [[ ! -d "${LOCAL_SRC}" ]]; then
  echo "error: ${TARGET_DIR} not found in supervisor rootfs" >&2
  exit 1
fi

WORK_DIR="${TMP_DIR}/work"
mkdir -p "${WORK_DIR}/a${TARGET_DIR}" "${WORK_DIR}/b${TARGET_DIR}"
cp -a "${IMAGE_SRC}/." "${WORK_DIR}/a${TARGET_DIR}/"
cp -a "${LOCAL_SRC}/." "${WORK_DIR}/b${TARGET_DIR}/"

if (cd "${WORK_DIR}" && diff -ruN a b > "${PATCH_FILE}"); then
  : > "${PATCH_FILE}"
  echo "No supervisor changes found; wrote empty patch to ${PATCH_FILE}"
else
  echo "Wrote patch to ${PATCH_FILE}"
fi
