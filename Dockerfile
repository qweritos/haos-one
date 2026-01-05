FROM debian:stable-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  libguestfs-tools \
  xz-utils linux-image-generic \
  && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG IMAGE_VERSION="17.0.rc1"
ARG DATA_IMG_SIZE="3G"
ENV DATA_IMG_SIZE="${DATA_IMG_SIZE}"

RUN mkdir -p /input /rootfs

RUN case "${TARGETARCH}" in \
    arm64) IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${IMAGE_VERSION}/haos_generic-aarch64-${IMAGE_VERSION}.qcow2.xz" ;; \
    amd64|x86_64|"") IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${IMAGE_VERSION}/haos_ova-${IMAGE_VERSION}.qcow2.xz" ;; \
    *) echo "Unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
  esac && \
  curl -fL "$IMAGE_URL" -o /input/disk.qcow2.xz && \
  unxz /input/disk.qcow2.xz

ENV LIBGUESTFS_DEBUG=1 LIBGUESTFS_TRACE=1
RUN case "${TARGETARCH}" in \
    arm64) export LIBGUESTFS_BACKEND=direct LIBGUESTFS_BACKEND_SETTINGS=force_tcg ;; \
  esac && \
  guestfish --ro -a /input/disk.qcow2 -m /dev/sda3 copy-out / /rootfs

# -------------------------------------------------------------------------------

FROM scratch

COPY --from=builder /rootfs/ /

ADD ./rootfs /

VOLUME [ "/mnt/data" ]

STOPSIGNAL SIGRTMIN+3

ENTRYPOINT ["/entrypoint.sh"]
CMD [ "/sbin/init" ]
