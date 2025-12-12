FROM debian:13-slim

LABEL org.opencontainers.image.title="Home Assistant Supervised in docker"
LABEL org.opencontainers.image.authors="Andrey Artamonychev<me@andrey.wtf>"
LABEL org.opencontainers.image.vendor="Andrey Artamonychev"
LABEL org.opencontainers.image.source="https://github.com/qweritos/hass-supervised-docker"
LABEL org.opencontainers.image.documentation="https://github.com/qweritos/hass-supervised-docker"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.description="Home Assistant Supervised container image based on Debian 12 with systemd and docker"

ARG OS_AGENT_VERSION=1.6.0
ARG SUPERVISED_INSTALLER_GIT_REF=main
ARG DATA_SHARE=/usr/share/hassio

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y ca-certificates curl
RUN install -m 0755 -d /etc/apt/keyrings && \
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc && \
  chmod a+r /etc/apt/keyrings/docker.asc && \
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

RUN apt-get update && apt-get install -y --no-install-recommends \
  systemd \
  apparmor \
  jq \
  wget \
  udisks2 \
  libglib2.0-bin \
  network-manager \
  dbus \
  lsb-release \
  systemd-journal-remote \
  systemd-resolved \
  nano \
  inetutils-ping \
  bluez \
  iproute2 \
  httping \
  git \
  avahi-daemon avahi-utils libnss-mdns \
  bash-completion \
  fuse-overlayfs \
  docker-ce docker-ce-cli containerd.io

RUN rm -f \
  /lib/systemd/system/sockets.target.wants/*udev* \
  /lib/systemd/system/sockets.target.wants/*initctl* \
  /lib/systemd/system/local-fs.target.wants/* \
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
      systemd-logind && \
      systemctl set-default multi-user.target || true

RUN systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target ModemManager.service

STOPSIGNAL SIGRTMIN+3

ADD ./rootfs /

RUN systemctl enable systemd-journal-remote.service systemd-resolved systemd-journal-gatewayd hass-install avahi-daemon systemd-journal-gatewayd.socket

# HACK: ignore post-installation scripts
RUN cd /tmp && wget -O os-agent.deb https://github.com/home-assistant/os-agent/releases/download/${OS_AGENT_VERSION}/os-agent_${OS_AGENT_VERSION}_linux_x86_64.deb && \
      dpkg --unpack os-agent.deb && \
      rm /var/lib/dpkg/info/os-agent.postinst -f && \
      dpkg --configure os-agent && \
      apt-get install -yf os-agent && \
      systemctl enable haos-agent

# HACK: skip `systemctl` commands that relies on operational systemd PID 1 instance
ENV DATA_SHARE=${DATA_SHARE}
ADD ./supervised-installer.patch /tmp
RUN cd /tmp && git clone https://github.com/home-assistant/supervised-installer.git --depth 1 --branch ${SUPERVISED_INSTALLER_GIT_REF} supervised-installer && \
      cd supervised-installer && \
      git apply ../supervised-installer.patch && \
      dpkg-deb --build --root-owner-group homeassistant-supervised && \
      apt install -y ./homeassistant-supervised.deb

RUN rm -rf /tmp/*

# overwrite docker config file that has been altered by homeassistant-supervised.deb
ADD ./rootfs/etc/docker/daemon.json /etc/docker/daemon.json

ENTRYPOINT [ "/entrypoint.sh" ]
CMD ["/sbin/init", "--log-level=debug", "--log-target=console"]
