#!/bin/sh
set -eu

# qemu-nbd --connect=/dev/nbd0 /input/disk.qcow2
# partprobe /dev/nbd0 2>/dev/null || true

# fdisk -l


# mkdir -p /lower
# mount -o ro /dev/nbd0p3 /lower

# mkdir -p /ovl
# mount /dev/nbd0p7 /ovl
# mkdir -p /ovl/upper /ovl/work

# mkdir -p /rootfs
# mount -t overlay overlay \
#   -o lowerdir=/lower,upperdir=/ovl/upper,workdir=/ovl/work \
#   /rootfs

# mount --make-rprivate /

# mkdir -p /rootfs/oldroot

# mount -o bind /etc/resolv.conf /rootfs/etc/resolv.conf

# cp -r /rootfs-add/* /rootfs/

# pivot_root /rootfs /rootfs/oldroot
# cd /

# mount --move /oldroot/dev  /dev
# mount --move /oldroot/proc /proc
# mount --move /oldroot/sys  /sys


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
