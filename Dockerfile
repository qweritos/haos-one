FROM debian:stable-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  libguestfs-tools \
  xz-utils linux-image-generic \
  && rm -rf /var/lib/apt/lists/*

ARG IMAGE_VERSION="17.0.rc1"
ENV IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${IMAGE_VERSION}/haos_ova-${IMAGE_VERSION}.qcow2.xz"

RUN mkdir -p /input /rootfs

RUN curl -fL "$IMAGE_URL" -o /input/disk.qcow2.xz && \
  unxz /input/disk.qcow2.xz

RUN guestfish --ro -a /input/disk.qcow2 -m /dev/sda3 copy-out / /rootfs

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
  systemd-ask-password-wall.path
#      systemctl set-default multi-user.target || true

RUN systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target ModemManager.service

# RUN mkdir -p /var/log/audit
# RUN ln -s /run /var/run

# ENV DOCKER_HOST=unix:///run/docker.sock

RUN curl -L https://github.com/containers/fuse-overlayfs/releases/download/v1.16/fuse-overlayfs-x86_64 -o /usr/bin/fuse-overlayfs && chmod +x /usr/bin/fuse-overlayfs
ADD ./rootfs /
STOPSIGNAL SIGRTMIN+3


ENTRYPOINT ["/entrypoint.sh"]
CMD [ "/sbin/init" ]
