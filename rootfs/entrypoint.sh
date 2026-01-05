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

# mount --make-rshared /mnt/data

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
