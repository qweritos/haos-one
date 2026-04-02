FROM debian:stable-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  jq \
  libguestfs-tools \
  xz-utils linux-image-generic \
  && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG HAOS_VERSION=""
ARG DATA_IMG_SIZE="3G"
ENV DATA_IMG_SIZE="${DATA_IMG_SIZE}"
ENV USE_DUMMY_NETWORKMANAGER=1
ENV DISABLE_UDEV=1

RUN mkdir -p /input /rootfs

RUN if [ -z "${HAOS_VERSION}" ]; then \
    HAOS_VERSION="$(curl -fsSL https://api.github.com/repos/home-assistant/operating-system/releases/latest \
      | jq -r '.tag_name')"; \
  fi && \
  if [ -z "${HAOS_VERSION}" ]; then \
    echo "Failed to resolve HAOS_VERSION, specify arg manually." >&2; exit 1; \
  fi && \
  case "${TARGETARCH}" in \
    arm64) IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${HAOS_VERSION}/haos_generic-aarch64-${HAOS_VERSION}.qcow2.xz" ;; \
    amd64|x86_64|"") IMAGE_URL="https://github.com/home-assistant/operating-system/releases/download/${HAOS_VERSION}/haos_ova-${HAOS_VERSION}.qcow2.xz" ;; \
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

LABEL org.opencontainers.image.title="haos-one"
LABEL org.opencontainers.image.authors="Andrey Artamonychev<me@andrey.wtf>"
LABEL org.opencontainers.image.vendor="Andrey Artamonychev"
LABEL org.opencontainers.image.source="https://github.com/qweritos/haos-one"
LABEL org.opencontainers.image.documentation="https://github.com/qweritos/haos-one/tree/master/docs"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.description="Home Assistant Operating System: Single-Container Docker Image"
LABEL io.artifacthub.package.readme-url="https://raw.githubusercontent.com/qweritos/haos-one/master/README.md"

COPY --from=builder /rootfs/ /

ADD ./rootfs /

RUN rm /etc/resolv.conf; touch /etc/resolv.conf

VOLUME [ "/mnt/data" ]
EXPOSE 8123
STOPSIGNAL SIGRTMIN+3

ENTRYPOINT ["/entrypoint.sh"]
CMD ["/sbin/init"]
