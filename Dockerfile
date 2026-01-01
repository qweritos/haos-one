FROM debian:stable-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  e2fsprogs \
  erofs-utils \
  fdisk \
  patch \
  rsync \
  util-linux \
  xz-utils \
  && rm -rf /var/lib/apt/lists/*

ARG IMAGE_VERSION="17.0.rc1"
ENV IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${IMAGE_VERSION}/haos_generic-x86-64-${IMAGE_VERSION}.img.xz"

RUN mkdir -p /input /rootfs

RUN curl -fL "$IMAGE_URL" -o /input/disk.img.xz && \
  unxz /input/disk.img.xz

RUN tmp=$(mktemp -d) && \
  parts="$(sfdisk -d /input/disk.img | awk -F'[ =,"]+' '$11=="name" && $12=="hassos-system0" {print $4 " " $6; exit}')" && \
  start=${parts%% *} && size=${parts#* } && \
  dd if=/input/disk.img of="$tmp/root.erofs" bs=512 skip="$start" count="$size" status=none && \
  fsck.erofs --extract=/rootfs --overwrite --preserve "$tmp/root.erofs" && \
  rm -rf "$tmp"

FROM scratch
COPY --from=builder /rootfs/ /

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
  systemd-logind 
#      systemctl set-default multi-user.target || true

RUN systemctl mask sleep.target ps axspend.target hibernate.target hybrid-sleep.target ModemManager.service

RUN mkdir -p /var/log/audit

ENV DOCKER_HOST=unix:///run/docker.sock

RUN curl -L https://github.com/containers/fuse-overlayfs/releases/download/v1.16/fuse-overlayfs-x86_64 -o /usr/bin/fuse-overlayfs && chmod +x /usr/bin/fuse-overlayfs
ADD ./rootfs /
STOPSIGNAL SIGRTMIN+3
