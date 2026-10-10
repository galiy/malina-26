#!/bin/bash -e

cat > "${ROOTFS_DIR}/etc/fstab" <<'EOF'
proc            /proc           proc    defaults          0       0
BOOTDEV  /boot/firmware  vfat    ro,noatime                          0       2
ROOTDEV  /               ext4    ro,noatime,nodelalloc,commit=60     0       1
tmpfs   /tmp                    tmpfs   defaults,noatime,nosuid,nodev,mode=1777,size=64m   0 0
tmpfs   /var/log                tmpfs   defaults,noatime,nosuid,mode=0755,size=64m         0 0
tmpfs   /var/tmp                tmpfs   defaults,noatime,nosuid,nodev,mode=1777,size=32m   0 0
tmpfs   /var/spool              tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=32m   0 0
tmpfs   /var/lib/dhcp           tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=4m    0 0
tmpfs   /var/lib/bluetooth      tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=16m   0 0
tmpfs   /var/lib/systemd        tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=32m   0 0
EOF

# /etc/mtab — заранее создать симлинк (на ro-корне systemd-tmpfiles не может).
ln -sfn ../proc/self/mounts "${ROOTFS_DIR}/etc/mtab"

CMDLINE="${ROOTFS_DIR}/boot/firmware/cmdline.txt"
if ! grep -qw ro "$CMDLINE"; then
	sed -i 's/[[:space:]]*$/ ro/' "$CMDLINE"
fi

install -d -m 0755 "${ROOTFS_DIR}/etc/systemd/journald.conf.d"
cat > "${ROOTFS_DIR}/etc/systemd/journald.conf.d/90-malina.conf" <<'EOF'
[Journal]
Storage=volatile
RuntimeMaxUse=32M
EOF

on_chroot <<'EOF'
systemctl disable resize2fs_once 2>/dev/null || true
systemctl disable regenerate_ssh_host_keys.service 2>/dev/null || true
systemctl disable apt-daily.timer apt-daily-upgrade.timer 2>/dev/null || true
systemctl mask systemd-random-seed.service
systemctl mask systemd-machine-id-commit.service
EOF
