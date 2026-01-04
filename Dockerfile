FROM debian:stable-slim AS builder

LABEL org.opencontainers.image.title="haos-one"
LABEL org.opencontainers.image.authors="Andrey Artamonychev<me@andrey.wtf>"
LABEL org.opencontainers.image.vendor="Andrey Artamonychev"
LABEL org.opencontainers.image.source="https://github.com/qweritos/haos-one"
LABEL org.opencontainers.image.documentation="https://github.com/qweritos/haos-one/tree/master/docs"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.description="Home Assistant Operating System: Single‑Container Docker Image"

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

ADD ./rootfs/entrypoint.sh /
ADD ./rootfs /rootfs-add

RUN rm -f \
  /lib/systemd/system/sockets.target.wants/*udev* \
  /lib/systemd/system/sockets.target.wants/*initctl* \
  /lib/systemd/system/local-fs.target.wants/* \
  /etc/systemd/system/local-fs.target.wants/* \
  /lib/systemd/system/sysinit.target.wants/systemd-tmpfiles-setup* \
  /etc/systemd/system/etc-resolv.conf.mount \
  /etc/systemd/system/etc-hostname.mount \
  /etc/systemd/system/etc-hosts.mount


RUN systemctl mask -- \
  tmp.mount \
  etc-hostname.mount \
  etc-hosts.mount \
  etc-resolv.conf.mount \
  swap.target \
  getty.target \
  getty-static.service \
  dev-mqueue.mount \
  cgproxy.service \
  systemd-tmpfiles-setup-dev.service \
  systemd-remount-fs.service \
  systemd-ask-password-wall.path \
  sleep.target suspend.target hibernate.target hybrid-sleep.target ModemManager.service

ADD ./rootfs /
STOPSIGNAL SIGRTMIN+3


ENTRYPOINT ["/entrypoint.sh"]
CMD [ "/sbin/init" ]
