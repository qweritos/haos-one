#!/bin/sh
set -eu

mkdir -p /mnt/data
if [ ! -e /data/data.img ]; then
  truncate -s 10G /data/data.img
  mkfs.ext4 -F /data/data.img
fi
mount /data/data.img /mnt/data

mount --make-rshared /
mount --make-rshared /mnt/data

if [ -x /usr/bin/grub-editenv ]; then
  mkdir -p /mnt/boot/EFI/BOOT
  if [ ! -f /mnt/boot/EFI/BOOT/grubenv ]; then
    grub-editenv /mnt/boot/EFI/BOOT/grubenv create
  fi
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set A_OK=1
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set A_TRY=0
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set ORDER="A B"
  grub-editenv /mnt/boot/EFI/BOOT/grubenv set B_OK=1
fi

mkdir -p /etc/systemd/system/rauc.service.d
cat > /etc/systemd/system/rauc.service.d/override.conf <<'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/rauc --mount=/run/rauc/mnt --override-boot-slot=A service
EOF


exec "$@"
