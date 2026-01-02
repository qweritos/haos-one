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

ENV LIBGUESTFS_DEBUG=1 
ENV LIBGUESTFS_TRACE=1

RUN guestfish --ro -a /input/disk.qcow2 -m /dev/sda3 copy-out / /rootfs

ADD ./rootfs /


ENTRYPOINT ["/entrypoint.sh"]
# CMD [ "/sbin/init" ]
