#!/bin/bash
# Этап 2: локальный деплой артефактов проекта на SD-карту (или тест-образ).
# Реализация плана deploy/local-deploy.md. Запускать с ноутбука от имени,
# имеющего sudo. Цель: /dev/sdX (весь диск) или образ-файл (*.img).
#
#   deploy/local/stage2.sh /dev/sdX
#   MAL_EXTRA_GB=5 deploy/local/stage2.sh /path/to/image.img   # тест-образ
#
set -euo pipefail

SRC="$(cd "$(dirname "$0")/../.." && pwd)"
PROV="$SRC/system/provision"
TARGET="${1:?usage: stage2.sh <device|image>}"
WORK="${WORK:-/tmp/malina-ld}"
ROOTM="$WORK/root"
MALM="$WORK/malina"
MAL_EXTRA_GB="${MAL_EXTRA_GB:-5}"
SUDO="${SUDO:-sudo}"
log() { printf '\n== %s\n' "$*"; }
die() { echo "ОШИБКА: $*" >&2; exit 1; }

mkdir -p "$WORK" "$ROOTM" "$MALM"
LOOP=""
cleanup() {
	set +e
	mountpoint -q "$MALM" && $SUDO umount "$MALM"
	mountpoint -q "$ROOTM" && $SUDO umount "$ROOTM"
	[ -n "$LOOP" ] && $SUDO losetup -d "$LOOP"
}
trap cleanup EXIT

# --- 2.0 resolve target -> BASE (для sfdisk), PART() ---
if [ -b "$TARGET" ]; then
	BASE="$TARGET"
	case "$BASE" in
		/dev/mmcblk[0-9]*|/dev/nvme[0-9]*|/dev/loop[0-9]*) PART() { echo "${BASE}p$1"; } ;;
		/dev/sd[a-z]) PART() { echo "${BASE}$1"; } ;;
		*) die "не понимаю устройство: $BASE" ;;
	esac
	[ -b "$(PART 1)" ] && [ -b "$(PART 2)" ] || die "нет разделов p1/p2 на $BASE"
else
	[ -f "$TARGET" ] || die "нет файла $TARGET"
	LOOP="$($SUDO losetup -Pf --show "$TARGET")"
	if [ ! -b "${LOOP}p3" ]; then
		$SUDO losetup -d "$LOOP"; LOOP=""
		log "увеличиваю образ на +${MAL_EXTRA_GB}G под p3"
		truncate -s "+${MAL_EXTRA_GB}G" "$TARGET"
		LOOP="$($SUDO losetup -Pf --show "$TARGET")"
	fi
	BASE="$LOOP"
	PART() { echo "${LOOP}p$1"; }
fi

# --- 2.1 раздел p3 ---
P3="$(PART 3)"
if [ ! -b "$P3" ]; then
	log "создаю раздел p3 в свободном месте"
	P2="$(PART 2)"
	p2_start=$(cat "/sys/class/block/$(basename "$P2")/start")
	p2_size=$(cat "/sys/class/block/$(basename "$P2")/size")
	start3=$(( p2_start + p2_size ))
	# выравниваем начало на 2048 секторов (1 МиБ)
	start3=$(( ((start3 + 2047) / 2048) * 2048 ))
	echo "start=${start3}, size=+, type=83" | $SUDO sfdisk --force --no-reread -N 3 "$BASE"
	$SUDO partprobe "$BASE" 2>/dev/null || true
	[ -n "$LOOP" ] && $SUDO losetup -c "$LOOP"
	$SUDO udevadm settle 2>/dev/null || sleep 1
fi
[ -b "$P3" ] || die "p3 не появился"
if ! $SUDO blkid "$P3" | grep -q 'TYPE="ext4"'; then
	log "mkfs.ext4 -L malina на $P3"
	$SUDO mkfs.ext4 -F -q -L malina "$P3"
fi

# --- 2.2 монтирование ---
log "монтирую p2 -> $ROOTM, p3 -> $MALM"
$SUDO mount "$(PART 2)" "$ROOTM"
$SUDO mount "$P3" "$MALM"
[ -d "$ROOTM/etc" ] || die "p2 не похож на rootfs"

# --- 2.3a точка монтирования /malina в rootfs (иначе malina.mount падает) ---
$SUDO mkdir -p "$ROOTM/malina" && $SUDO chmod 0755 "$ROOTM/malina"

# --- 2.3 fstab ---
if ! grep -q 'LABEL=malina' "$ROOTM/etc/fstab"; then
	log "fstab: + LABEL=malina"
	echo 'LABEL=malina  /malina  ext4  defaults,noatime  0  2' | $SUDO tee -a "$ROOTM/etc/fstab" >/dev/null
fi

# --- 2.4 дерево /malina и владельцы ---
log "дерево /malina"
$SUDO mkdir -p "$MALM/etc/malina/network" "$MALM/etc/wpa_supplicant" "$MALM/home" \
	"$MALM/root" "$MALM/var/spool/cron/crontabs" "$MALM/usr/local/share/malina/web" \
	"$MALM/usr/local/bin" "$MALM/usr/local/sbin" "$MALM/usr/local/libexec"
$SUDO install -m0600 "$SRC/system/network/mode1.json" "$MALM/etc/malina/network/mode1.json"
$SUDO install -m0600 "$SRC/system/network/mode2.json" "$MALM/etc/malina/network/mode2.json"
$SUDO install -m0644 "$SRC/system/network/active"   "$MALM/etc/malina/network/active"

# --- 2.5 перенос /root,/home,cron,wpa и симлинки ---
log "перенос и симлинки в rootfs"
if [ -d "$ROOTM/root" ] && [ ! -L "$ROOTM/root" ]; then $SUDO cp -a "$ROOTM/root/." "$MALM/root/"; $SUDO rm -rf "$ROOTM/root"; fi
if [ -d "$ROOTM/home" ] && [ ! -L "$ROOTM/home" ]; then $SUDO cp -a "$ROOTM/home/." "$MALM/home/"; $SUDO rm -rf "$ROOTM/home"; fi
$SUDO ln -sfn /malina/root "$ROOTM/root"
$SUDO ln -sfn /malina/home "$ROOTM/home"
if [ -d "$ROOTM/var/spool/cron" ] && [ ! -L "$ROOTM/var/spool/cron" ]; then
	$SUDO cp -a "$ROOTM/var/spool/cron/." "$MALM/var/spool/cron/" 2>/dev/null || true
	$SUDO rm -rf "$ROOTM/var/spool/cron"
fi
$SUDO ln -sfn /malina/var/spool/cron "$ROOTM/var/spool/cron"
if [ -d "$ROOTM/etc/wpa_supplicant" ] && [ ! -L "$ROOTM/etc/wpa_supplicant" ]; then
	$SUDO cp -a "$ROOTM/etc/wpa_supplicant/." "$MALM/etc/wpa_supplicant/"
	$SUDO rm -rf "$ROOTM/etc/wpa_supplicant"
fi
$SUDO ln -sfn /malina/etc/wpa_supplicant "$ROOTM/etc/wpa_supplicant"
$SUDO mkdir -p "$ROOTM/var/lib/dbus" "$ROOTM/run"
$SUDO ln -sfn /run/malina-net/resolv.conf "$ROOTM/etc/resolv.conf"
$SUDO ln -sfn /etc/machine-id "$ROOTM/var/lib/dbus/machine-id"

# --- 2.6 machine-id, hostname ---
log "machine-id / hostname"
# Пересоздаём (rm + install) — свежий inode, чтобы не унаследовать потенциально
# повреждённый inode из образа (см. «Повреждённый inode» в deploy/local-deploy.md).
[ -s "$PROV/machine-id" ] && { $SUDO rm -f "$ROOTM/etc/machine-id"; $SUDO install -m0444 "$PROV/machine-id" "$ROOTM/etc/machine-id"; }
[ -s "$PROV/hostname" ]   && { $SUDO rm -f "$ROOTM/etc/hostname";   $SUDO install -m0644 "$PROV/hostname"   "$ROOTM/etc/hostname"; }

# --- 2.7 артефакты system/* по manifest ---
log "manifest -> rootfs"
while IFS=$'\t' read -r src dst mode pol; do
	[ -z "${src:-}" ] && continue
	case "$src" in \#*) continue ;; esac
	# назначения под /malina кладём на p3 (rw), остальное — в rootfs (ro):
	case "$dst" in
		/malina/*) tgt="$MALM${dst#/malina}" ;;
		*)         tgt="$ROOTM$dst" ;;
	esac
	if [ "$pol" = "if_absent" ] && [ -e "$tgt" ]; then continue; fi
	$SUDO install -D -o root -g root -m "$mode" "$SRC/$src" "$tgt"
done < "$SRC/deploy/manifest.tsv"

# --- 2.8 бинарники (на /malina — так их ждут юниты) ---
log "бинарники"
$SUDO install -D -o root -g root -m0755 "$SRC/web/dist/malina-web-armv7" "$MALM/usr/local/bin/malina-web"
$SUDO install -D -o root -g root -m0755 "$SRC/web/dist/malina-netd-armv7" "$MALM/usr/local/sbin/malina-netd"
$SUDO install -D -o root -g root -m0755 "$SRC/daemons/map-reader/dist/map-reader-armv7" "$MALM/usr/local/sbin/map-reader"
$SUDO install -D -o root -g root -m0755 "$SRC/daemons/kes-reader/dist/kes-reader-armv7" "$MALM/usr/local/sbin/kes-reader"

# --- 2.9 веб-ассеты (на /malina) ---
log "веб-ассеты"
$SUDO rm -rf "$MALM/usr/local/share/malina/web"
$SUDO mkdir -p "$MALM/usr/local/share/malina/web"
$SUDO cp -a "$SRC/web/assets/templates" "$SRC/web/assets/static" "$SRC/web/assets/i18n" "$MALM/usr/local/share/malina/web/"
$SUDO find "$MALM/usr/local/share/malina/web" -type d -exec chmod 0755 {} +
$SUDO find "$MALM/usr/local/share/malina/web" -type f -exec chmod 0644 {} +

# --- 2.10 pamcheck (chroot+qemu) ---
log "сборка pamcheck (armhf, chroot+qemu)"
$SUDO cp "$SRC/web/pamcheck/pamcheck.c" "$ROOTM/tmp/pamcheck.c"
[ -e "$ROOTM/usr/bin/qemu-arm-static" ] || $SUDO cp /usr/bin/qemu-arm-static "$ROOTM/usr/bin/"
$SUDO mount --bind /proc "$ROOTM/proc" 2>/dev/null || true
$SUDO chroot "$ROOTM" /usr/bin/gcc -O2 -o /tmp/malina-pamcheck /tmp/pamcheck.c -ldl
$SUDO install -D -o root -g root -m0755 "$ROOTM/tmp/malina-pamcheck" "$MALM/usr/local/libexec/malina-pamcheck"
$SUDO rm -f "$ROOTM/tmp/malina-pamcheck" "$ROOTM/tmp/pamcheck.c" "$ROOTM/usr/bin/qemu-arm-static"
$SUDO umount "$ROOTM/proc" 2>/dev/null || true

# --- 2.11 владельцы ---
log "владельцы/режимы"
# ВАЖНО: не chown-ить весь rootfs /etc — иначе ломаются системные группы
# (/etc/shadow → shadow, sudoers, gshadow и т.п.). Артефакты ставятся install -o root -g root.
$SUDO chown -R root:root "$MALM/etc" "$MALM/usr" "$MALM/var" "$MALM/home"
$SUDO chown root:root "$MALM/root" && $SUDO chmod 0700 "$MALM/root"
# числовые uid/gid из образа (на ПК пользователя malina/crontab нет)
CGID=$(awk -F: '$1=="crontab"{print $3; exit}' "$ROOTM/etc/group")
MIDS=$(awk -F: '$1=="malina"{print $3":"$4; exit}' "$ROOTM/etc/passwd")
[ -n "$CGID" ] && $SUDO chown "root:$CGID" "$MALM/var/spool/cron/crontabs" && $SUDO chmod 1730 "$MALM/var/spool/cron/crontabs"
if [ -d "$MALM/home/malina" ] && [ -n "$MIDS" ]; then
	$SUDO chown -R "$MIDS" "$MALM/home/malina" && $SUDO chmod 0700 "$MALM/home/malina"
fi

# --- 2.12 юниты offline ---
log "enable/mask/disable юнитов"
for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader redis-server; do
	$SUDO systemctl --root="$ROOTM" enable "$u" >/dev/null 2>&1 || true
done
[ -f "$PROV/masked.txt" ]   && while read -r u; do [ -n "$u" ] && $SUDO systemctl --root="$ROOTM" mask "$u"    >/dev/null 2>&1 || true; done < "$PROV/masked.txt"
[ -f "$PROV/disabled.txt" ] && while read -r u; do [ -n "$u" ] && $SUDO systemctl --root="$ROOTM" disable "$u" >/dev/null 2>&1 || true; done < "$PROV/disabled.txt"

# --- 2.13 NetworkManager (purge в chroot) ---
log "удаление NetworkManager (chroot apt purge)"
$SUDO mount --bind /proc "$ROOTM/proc" 2>/dev/null || true
$SUDO mount --bind /dev  "$ROOTM/dev"  2>/dev/null || true
$SUDO cp /usr/bin/qemu-arm-static "$ROOTM/usr/bin/" 2>/dev/null || true
$SUDO chroot "$ROOTM" /bin/sh -c 'DEBIAN_FRONTEND=noninteractive apt-get -y purge network-manager network-manager-* >/dev/null 2>&1 || true'
$SUDO rm -f "$ROOTM/usr/bin/qemu-arm-static"
$SUDO umount "$ROOTM/dev" "$ROOTM/proc" 2>/dev/null || true

# Образ держит fstab-tmpfs '/var/lib/NetworkManager'; purge удаляет каталог, и mount
# падает → local-fs.target → emergency. Убираем строку и подстраховываем остальные
# tmpfs-точки (если точки нет — создаём), чтобы загрузка не блокировалась.
log "fstab: убираю tmpfs /var/lib/NetworkManager; обеспечиваю /var/lib/dhcp; проверяю tmpfs-точки"
$SUDO sed -i '\#/var/lib/NetworkManager#d' "$ROOTM/etc/fstab"
# /var/lib/dhcp — аренды dhclient (иначе запись на ro-корень падает)
grep -q '/var/lib/dhcp' "$ROOTM/etc/fstab" || echo 'tmpfs   /var/lib/dhcp           tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=4m    0 0' | $SUDO tee -a "$ROOTM/etc/fstab" >/dev/null
awk '$3=="tmpfs"{print $2}' "$ROOTM/etc/fstab" | while read -r mp; do
	[ -n "$mp" ] && $SUDO mkdir -p "$ROOTM$mp"
done
# остатки конфигов NetworkManager (создавались образом) — убрать после purge
$SUDO rm -rf "$ROOTM/etc/NetworkManager"

# --- 2.14 tmpfiles (best-effort) ---
$SUDO systemd-tmpfiles --root="$ROOTM" --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf >/dev/null 2>&1 || true

log "sync и размонтирование"
$SUDO sync
$SUDO umount "$MALM"
$SUDO umount "$ROOTM"
[ -n "$LOOP" ] && { $SUDO losetup -d "$LOOP"; LOOP=""; }

log "готово"
