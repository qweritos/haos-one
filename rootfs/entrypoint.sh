#!/bin/sh
set -eu

# todo: make it configurable
echo UTC > /etc/timezone

# mkdir -p /mnt/data
# if [ ! -e /data/data.img ]; then
#   size="${DATA_IMG_SIZE:-3G}"
#   case "$size" in
#     *G) count=$(( ${size%G} * 1024 )) ;;
#     *M) count=$(( ${size%M} )) ;;
#     *) echo "Unsupported DATA_IMG_SIZE=$size (use M or G suffix)" >&2; exit 1 ;;
#   esac
#   dd if=/dev/zero of=/data/data.img bs=1M count="$count"
#   mkfs.xfs -f -n ftype=1 /data/data.img
#   sync
# fi
# loopdev="$(losetup -f)"
# losetup "$loopdev" /data/data.img
# mount -t xfs "$loopdev" /mnt/data

mount --make-rshared /mnt/data

use_dummy_networkmanager=0
case "${USE_DUMMY_NETWORKMANAGER:-1}" in
  1|true|TRUE|yes|YES|on|ON)
    use_dummy_networkmanager=1
    ln -sf /dev/null /etc/systemd/system/NetworkManager.service
    ;;
esac

mkdir -p /etc/systemd/system/multi-user.target.wants
ln -sf /etc/systemd/system/haos-one-compat.service /etc/systemd/system/multi-user.target.wants/haos-one-compat.service
mkdir -p /etc/systemd/system/hassos-supervisor.service.d
cat > /etc/systemd/system/hassos-supervisor.service.d/override.conf <<'EOF'
[Unit]
After=haos-one-compat.service
Requires=haos-one-compat.service
EOF
mkdir -p /etc/systemd/system/haos-one-compat.service.d
cat > /etc/systemd/system/haos-one-compat.service.d/override.conf <<EOF
[Service]
ExecStart=
ExecStart=/usr/bin/docker run --name haos_one_compat -e USE_DUMMY_NETWORKMANAGER=$use_dummy_networkmanager -v /run/dbus:/run/dbus -v /run:/host-run haos_one_compat
EOF

# Optionally disable udev via systemd masking.
case "${DISABLE_UDEV:-1}" in
  1|true|TRUE|yes|YES|on|ON)
    ln -sf /dev/null /etc/systemd/system/systemd-udevd.service
    ln -sf /dev/null /etc/systemd/system/systemd-udevd-control.socket
    ln -sf /dev/null /etc/systemd/system/systemd-udevd-kernel.socket
    ln -sf /dev/null /etc/systemd/system/systemd-udev-trigger.service
    ;;
esac

# Disable the HA CLI login service for wrong-geometry console, for example for docker-compose.
# https://github.com/qweritos/haos-one/issues/31
disable_ha_cli=0
if [ -c /dev/console ]; then
  console_size="$(stty -F /dev/console size 2>/dev/null || true)"
  case "$console_size" in
    ''|0\ *|*\ 0)
      disable_ha_cli=1
      ;;
  esac
fi

if [ "$disable_ha_cli" -eq 1 ]; then
  ln -sf /dev/null /etc/systemd/system/ha-cli@console.service
  ln -sf /dev/null /etc/systemd/system/ha-cli@tty1.service
fi

case "${DEV:-0}" in
  1|true|TRUE|yes|YES|on|ON)
    cat > /etc/systemd/system/haos-one-compat.service.d/override.conf <<EOF
[Service]
ExecStart=
ExecStart=/usr/bin/docker run --name haos_one_compat -e USE_DUMMY_NETWORKMANAGER=$use_dummy_networkmanager -v /run/dbus:/run/dbus -v /run:/host-run -v /opt/haos-one-compat:/opt/haos-one-compat haos_one_compat
EOF
    ;;
esac

# make rauc to start
if [ -x /usr/bin/grub-editenv ]; then
  mkdir -p /mnt/boot/EFI/BOOT
  if [ ! -f /mnt/boot/EFI/BOOT/grubenv ]; then
    grub-editenv /mnt/boot/EFI/BOOT/grubenv create
  fi
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set A_OK=1
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set A_TRY=0
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set ORDER="A B"
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set B_OK=1
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set B_TRY=0
fi

exec "$@"
