#!/bin/sh
set -eu

qemu-nbd --connect=/dev/nbd0 /input/disk.qcow2
partprobe /dev/nbd0 2>/dev/null || true

fdisk -l


mkdir -p /lower
mount -o ro /dev/nbd0p3 /lower

# Upper/work live on tmpfs (writable)
mkdir -p /ovl
mount -t tmpfs tmpfs /ovl
mkdir -p /ovl/upper /ovl/work

# Overlay mount becomes the new root
mkdir -p /rootfs
mount -t overlay overlay \
  -o lowerdir=/lower,upperdir=/ovl/upper,workdir=/ovl/work \
  /rootfs

mount --make-rprivate /

mkdir -p /rootfs/oldroot

mount -o bind /etc/resolv.conf /rootfs/etc/resolv.conf

pivot_root /rootfs /rootfs/oldroot
cd /

mount --move /oldroot/dev  /dev
mount --move /oldroot/proc /proc
mount --move /oldroot/sys  /sys

# mount -t tmpfs tmpfs /run
# mkdir -p /run/lock

# mkdir -p /sys/fs/cgroup
# mount -t cgroup2 none /sys/fs/cgroup 2>/dev/null || true


mount /dev/nbd0p1 /mnt/boot
mount /dev/nbd0p7 /mnt/overlay
mount /dev/nbd0p8 /mnt/data
fallocate -l 10G /oldroot/data
mkfs.ext4 /oldroot/data
mount /oldroot/data /mnt/data
mount --make-rshared /
mount --make-rshared /mnt/data

mkdir -p /etc/systemd/system/rauc.service.d
cat > /etc/systemd/system/rauc.service.d/override.conf <<'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/rauc --mount=/run/rauc/mnt --override-boot-slot=A service
EOF


exec /sbin/init
