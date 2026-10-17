#!/bin/bash
# Единый локальный деплой ПАК «Малина»: перезаливка голого образа + раскладка
# артефактов + offline-проверка. Один вариант — на смену stage2.sh/stage4.sh.
#
#   deploy/local/deploy-local.sh <device|image>          выполнить (перезаливка+раскладка)
#   deploy/local/deploy-local.sh --plan <device|image>   напечатать пошаговый план (dry-run)
#
# Цель:
#   - блочное устройство (напр. /dev/sdX) — карта в картридере ПК;
#     перезаливает образ (p1+p2), затем создаёт p3 /malina и раскладывает артефакты.
#   - файл (*.img) — тест-образ: увеличивает файл на MAL_EXTRA_GB под p3 и
#     раскладывает через loop (без перезаливки, образ уже есть).
#
#   IMG=… — образ для перезаливки (по умолчанию — последний *malina-lite.img в pi-gen).
#   MAL_EXTRA_GB=5 — добавочный размер для p3 у тест-образа (default 5).
#   WORK=…, SUDO=… — переопределения.
#
# ВНИМАНИЕ: для устройства скрипт ПЕРЕЗАЛИВАЕТ карту (`dd` образа) — стирает её
# содержимое целиком. Это намеренное поведение единого локального деплоя.
set -euo pipefail

SRC="$(cd "$(dirname "$0")/../.." && pwd)"
PROV="$SRC/system/provision"
WORK="${WORK:-/tmp/malina-ld}"
ROOTM="$WORK/root"
MALM="$WORK/malina"
MAL_EXTRA_GB="${MAL_EXTRA_GB:-5}"
SUDO="${SUDO:-sudo}"
PIGEN_DEPLOY="/opt/raspberry_tmp/pi-gen/deploy"
PLAN=0

log()  { printf '\n== %s\n' "$*"; }
plan() { if [ "$PLAN" = 1 ]; then printf '[ПЛАН] %s\n' "$*"; fi; }
die()  { echo "ОШИБКА: $*" >&2; exit 1; }

# --- 0.0 разбор аргументов ---
if [ "${1:-}" = "--plan" ]; then PLAN=1; shift; fi
TARGET="${1:?usage: deploy-local.sh [--plan] <device|image>}"
if [ -z "${IMG:-}" ]; then
	# последний собранный образ
	IMG="$(ls -1t "$PIGEN_DEPLOY"/*malina-lite.img 2>/dev/null | head -1 || true)"
fi
: "${IMG:?не найден образ (задайте IMG=… или соберите image/build.sh)}"
[ "$PLAN" = 1 ] && { log "План (dry-run) для: $TARGET ; образ: $IMG"; }

mkdir -p "$WORK" "$ROOTM" "$MALM"
LOOP=""
cleanup() {
	set +e
	mountpoint -q "$MALM" && $SUDO umount "$MALM"
	mountpoint -q "$ROOTM" && $SUDO umount "$ROOTM"
	[ -n "$LOOP" ] && $SUDO losetup -d "$LOOP"
}
[ "$PLAN" = 1 ] || trap cleanup EXIT

# --- 0.1 resolve target -> BASE (для sfdisk), PART() ---
BASE="" ; PART() { :; }
if [ -b "$TARGET" ]; then
	# блочное устройство: перезаливка образа + раскладка
	case "$TARGET" in
		/dev/mmcblk[0-9]*|/dev/nvme[0-9]*|/dev/loop[0-9]*) PART() { echo "${TARGET}p$1"; } ;;
		/dev/sd[a-z]) PART() { echo "${TARGET}$1"; } ;;
		*) die "не понимаю устройство: $TARGET" ;;
	esac
	BASE="$TARGET"
	plan "ПЕРЕЗАЛИВКА: dd $IMG -> $TARGET (p1+p2)"
	if [ "$PLAN" = 0 ]; then
		log "запись образа (dd)…"
		$SUDO dd if="$IMG" of="$TARGET" bs=4M conv=fsync status=progress 2>&1 | tail -2
		sync
		$SUDO partprobe "$TARGET" 2>/dev/null || true
		$SUDO udevadm settle 2>/dev/null || sleep 1
	fi
else
	# файл-образ (тест) или — в plan-режиме — несуществующий плейсхолдер устройства
	if [ -f "$TARGET" ]; then
		ISFILE=1
	else
		[ "$PLAN" = 1 ] || die "нет файла $TARGET"
		ISFILE=0
	fi
	if [ "$ISFILE" = 1 ]; then
		[ "$PLAN" = 1 ] && { plan "ТЕСТ-ОБРАЗ: $TARGET (loop)"; plan "[при необходимости] truncate -s +${MAL_EXTRA_GB}G для p3"; }
		if [ "$PLAN" = 0 ]; then
			LOOP="$($SUDO losetup -Pf --show "$TARGET")"
			if [ ! -b "${LOOP}p3" ]; then
				log "увеличиваю образ на +${MAL_EXTRA_GB}G под p3"
				truncate -s "+${MAL_EXTRA_GB}G" "$TARGET"
				$SUDO losetup -d "$LOOP"; LOOP=""
				LOOP="$($SUDO losetup -Pf --show "$TARGET")"
			fi
			BASE="$LOOP"
			PART() { echo "${LOOP}p$1"; }
		fi
	else
		# plan-режим на плейсхолдере устройства
		BASE="$TARGET"
		plan "ПЕРЕЗАЛИВКА: dd $IMG -> $TARGET (p1+p2)"
	fi
fi

# --- 1. раздел p3 (/malina) и его ФС ---
if [ "$PLAN" = 1 ]; then
	plan "Разметка p3 (/malina): начало от конца p2 (…/start|size), size=+ (весь остаток)"
	plan "  sfdisk -N 3; затем mkfs.ext4 -L malina на p3"
else
	P3="$(PART 3)"
	if [ ! -b "$P3" ]; then
		log "создаю раздел p3 в свободном месте"
		P2="$(PART 2)"
		p2_start=$(cat "/sys/class/block/$(basename "$P2")/start")
		p2_size=$(cat "/sys/class/block/$(basename "$P2")/size")
		start3=$(( p2_start + p2_size ))
		start3=$(( ((start3 + 2047) / 2048) * 2048 ))   # выравнивание 1 МиБ
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
fi

# --- 2. монтирование ---
if [ "$PLAN" = 1 ]; then
	plan "Монтирование: p2 -> \$WORK/root (r), p3 -> \$WORK/malina (r)"
else
	log "монтирую p2 -> $ROOTM, p3 -> $MALM"
	$SUDO mount "$(PART 2)" "$ROOTM"
	$SUDO mount "$(PART 3)" "$MALM"
	[ -d "$ROOTM/etc" ] || die "p2 не похож на rootfs"
fi

# --- 2.3a точка монтирования /malina в rootfs ---
if [ "$PLAN" = 1 ]; then
	plan "mkdir -p \$ROOT/malina (точка монтирования LABEL=malina)"
else
	$SUDO mkdir -p "$ROOTM/malina" && $SUDO chmod 0755 "$ROOTM/malina"
fi

# --- 2.3 fstab ---
if [ "$PLAN" = 1 ]; then
	plan "fstab: + 'LABEL=malina  /malina  ext4  defaults,noatime  0  2' (если нет)"
else
	if ! grep -q 'LABEL=malina' "$ROOTM/etc/fstab"; then
		log "fstab: + LABEL=malina"
		echo 'LABEL=malina  /malina  ext4  defaults,noatime  0  2' | $SUDO tee -a "$ROOTM/etc/fstab" >/dev/null
	fi
fi

# --- 2.4 дерево /malina и seed-конфиги сети ---
if [ "$PLAN" = 1 ]; then
	plan "mkdir -p /malina/{etc/malina/network,etc/wpa_supplicant,home,root,var/spool/cron/crontabs,usr/local/{bin,sbin,libexec,share/malina/web}}"
	plan "install mode1.json/mode2.json/active (seed сети) в /malina/etc/malina/network/"
else
	log "дерево /malina"
	$SUDO mkdir -p "$MALM/etc/malina/network" "$MALM/etc/wpa_supplicant" "$MALM/home" \
		"$MALM/root" "$MALM/var/spool/cron/crontabs" "$MALM/usr/local/share/malina/web" \
		"$MALM/usr/local/bin" "$MALM/usr/local/sbin" "$MALM/usr/local/libexec"
	$SUDO install -m0600 "$SRC/system/network/mode1.json" "$MALM/etc/malina/network/mode1.json"
	$SUDO install -m0600 "$SRC/system/network/mode2.json" "$MALM/etc/malina/network/mode2.json"
	$SUDO install -m0644 "$SRC/system/network/active"   "$MALM/etc/malina/network/active"
fi

# --- 2.5 перенос /root,/home,cron,wpa и симлинки ---
if [ "$PLAN" = 1 ]; then
	plan "Перенос /root, /home, cron, wpa на /malina; симлинки: /root,/home,/etc/wpa_supplicant -> /malina/*"
	plan "  ln -sfn /run/malina-net/resolv.conf -> /etc/resolv.conf; dbus machine-id -> /etc/machine-id"
else
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
fi

# --- 2.6 machine-id, hostname ---
if [ "$PLAN" = 1 ]; then
	plan "machine-id -> /etc/machine-id (0444); hostname -> /etc/hostname (0644); свежие inode (rm + install)"
else
	log "machine-id / hostname"
	[ -s "$PROV/machine-id" ] && { $SUDO rm -f "$ROOTM/etc/machine-id"; $SUDO install -m0444 "$PROV/machine-id" "$ROOTM/etc/machine-id"; }
	[ -s "$PROV/hostname" ]   && { $SUDO rm -f "$ROOTM/etc/hostname";   $SUDO install -m0644 "$PROV/hostname"   "$ROOTM/etc/hostname"; }
fi

# --- 2.7 артефакты system/* по manifest ---
if [ "$PLAN" = 1 ]; then
	plan "Раскладка артефактов по deploy/manifest.tsv (src -> dst, mode, policy always|if_absent); /malina/* на p3, прочее в rootfs"
else
	log "manifest -> rootfs"
	while IFS=$'\t' read -r src dst mode pol; do
		[ -z "${src:-}" ] && continue
		case "$src" in \#*) continue ;; esac
		case "$dst" in
			/malina/*) tgt="$MALM${dst#/malina}" ;;
			*)         tgt="$ROOTM$dst" ;;
		esac
		if [ "$pol" = "if_absent" ] && [ -e "$tgt" ]; then continue; fi
		$SUDO install -D -o root -g root -m "$mode" "$SRC/$src" "$tgt"
	done < "$SRC/deploy/manifest.tsv"
fi

# --- 2.8 бинарники (на /malina) ---
if [ "$PLAN" = 1 ]; then
	plan "Бинарники (0755) в /malina/usr/local/{bin,sbin}: malina-web, malina-netd, map-reader, kes-reader, bms-reader, bms-proxy, malina-tsdb"
else
	log "бинарники"
	$SUDO install -D -o root -g root -m0755 "$SRC/web/dist/malina-web-armv7" "$MALM/usr/local/bin/malina-web"
	$SUDO install -D -o root -g root -m0755 "$SRC/web/dist/malina-netd-armv7" "$MALM/usr/local/sbin/malina-netd"
	$SUDO install -D -o root -g root -m0755 "$SRC/daemons/map-reader/dist/map-reader-armv7" "$MALM/usr/local/sbin/map-reader"
	$SUDO install -D -o root -g root -m0755 "$SRC/daemons/kes-reader/dist/kes-reader-armv7" "$MALM/usr/local/sbin/kes-reader"
	$SUDO install -D -o root -g root -m0755 "$SRC/daemons/tsdb/dist/malina-tsdb-armv7" "$MALM/usr/local/sbin/malina-tsdb"
	$SUDO install -D -o root -g root -m0755 "$SRC/daemons/bms-reader/dist/bms-reader-armv7" "$MALM/usr/local/sbin/bms-reader"
	$SUDO install -D -o root -g root -m0755 "$SRC/daemons/bms-proxy/dist/bms-proxy-armv7" "$MALM/usr/local/sbin/bms-proxy"
fi

# --- 2.9 веб-ассеты (на /malina) ---
if [ "$PLAN" = 1 ]; then
	plan "Веб-ассеты: cp -a web/assets/{templates,static,i18n} -> /malina/usr/local/share/malina/web/; режимы"
else
	log "веб-ассеты"
	$SUDO rm -rf "$MALM/usr/local/share/malina/web"
	$SUDO mkdir -p "$MALM/usr/local/share/malina/web"
	$SUDO cp -a "$SRC/web/assets/templates" "$SRC/web/assets/static" "$SRC/web/assets/i18n" "$MALM/usr/local/share/malina/web/"
	$SUDO find "$MALM/usr/local/share/malina/web" -type d -exec chmod 0755 {} +
	$SUDO find "$MALM/usr/local/share/malina/web" -type f -exec chmod 0644 {} +
fi

# --- 2.10 pamcheck (chroot+qemu, gcc из образа) ---
if [ "$PLAN" = 1 ]; then
	plan "pamcheck: положить pamcheck.c в \$ROOT/tmp, скопировать qemu-arm-static, chroot gcc -O2 -> /tmp/malina-pamcheck, install в /malina/usr/local/libexec/; зачистить"
else
	log "сборка pamcheck (armhf, chroot+qemu)"
	$SUDO cp "$SRC/web/pamcheck/pamcheck.c" "$ROOTM/tmp/pamcheck.c"
	[ -e "$ROOTM/usr/bin/qemu-arm-static" ] || $SUDO cp /usr/bin/qemu-arm-static "$ROOTM/usr/bin/"
	$SUDO mount --bind /proc "$ROOTM/proc" 2>/dev/null || true
	$SUDO chroot "$ROOTM" /usr/bin/gcc -O2 -o /tmp/malina-pamcheck /tmp/pamcheck.c -ldl
	$SUDO install -D -o root -g root -m0755 "$ROOTM/tmp/malina-pamcheck" "$MALM/usr/local/libexec/malina-pamcheck"
	$SUDO rm -f "$ROOTM/tmp/malina-pamcheck" "$ROOTM/tmp/pamcheck.c" "$ROOTM/usr/bin/qemu-arm-static"
	$SUDO umount "$ROOTM/proc" 2>/dev/null || true
fi

# --- 2.11 владельцы ---
if [ "$PLAN" = 1 ]; then
	plan "Владельцы/режимы: chown root:root /malina/{etc,usr,var,home}; /malina/root root:root 0700; crontabs root:<gid>crontab 1730; /home/malina <uid>:<gid> 0700 (числовые, из образа)"
else
	log "владельцы/режимы"
	$SUDO chown -R root:root "$MALM/etc" "$MALM/usr" "$MALM/var" "$MALM/home"
	$SUDO chown root:root "$MALM/root" && $SUDO chmod 0700 "$MALM/root"
	CGID=$(awk -F: '$1=="crontab"{print $3; exit}' "$ROOTM/etc/group")
	MIDS=$(awk -F: '$1=="malina"{print $3":"$4; exit}' "$ROOTM/etc/passwd")
	[ -n "$CGID" ] && $SUDO chown "root:$CGID" "$MALM/var/spool/cron/crontabs" && $SUDO chmod 1730 "$MALM/var/spool/cron/crontabs"
	if [ -d "$MALM/home/malina" ] && [ -n "$MIDS" ]; then
		$SUDO chown -R "$MIDS" "$MALM/home/malina" && $SUDO chmod 0700 "$MALM/home/malina"
	fi
fi

# --- 2.12 юниты ---
if [ "$PLAN" = 1 ]; then
	plan "systemctl --root=\$ROOT enable: dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader malina-bms-reader malina-bms-proxy malina-tsdb malina-tsdb-raw"
	plan "mask/disable по system/provision/{masked,disabled}.txt"
else
	log "enable/mask/disable юнитов"
	for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader malina-bms-reader malina-bms-proxy malina-tsdb malina-tsdb-raw; do
		$SUDO systemctl --root="$ROOTM" enable "$u" >/dev/null 2>&1 || true
	done
	[ -f "$PROV/masked.txt" ]   && while read -r u; do [ -n "$u" ] && $SUDO systemctl --root="$ROOTM" mask "$u"    >/dev/null 2>&1 || true; done < "$PROV/masked.txt"
	[ -f "$PROV/disabled.txt" ] && while read -r u; do [ -n "$u" ] && $SUDO systemctl --root="$ROOTM" disable "$u" >/dev/null 2>&1 || true; done < "$PROV/disabled.txt"
fi

# --- 2.13 NetworkManager: purge + фиксы fstab/emergency ---
if [ "$PLAN" = 1 ]; then
	plan "purge network-manager (chroot); fstab: удалить tmpfs /var/lib/NetworkManager, +tmpfs /var/lib/dhcp, +tmpfs /run/malina-ts-raw (200M); создать tmpfs-точки; rm -rf /etc/NetworkManager"
else
	log "удаление NetworkManager (chroot apt purge)"
	$SUDO mount --bind /proc "$ROOTM/proc" 2>/dev/null || true
	$SUDO mount --bind /dev  "$ROOTM/dev"  2>/dev/null || true
	$SUDO cp /usr/bin/qemu-arm-static "$ROOTM/usr/bin/" 2>/dev/null || true
	$SUDO chroot "$ROOTM" /bin/sh -c 'DEBIAN_FRONTEND=noninteractive apt-get -y purge network-manager network-manager-* >/dev/null 2>&1 || true'
	$SUDO rm -f "$ROOTM/usr/bin/qemu-arm-static"
	$SUDO umount "$ROOTM/dev" "$ROOTM/proc" 2>/dev/null || true

	log "fstab: убираю tmpfs /var/lib/NetworkManager; обеспечиваю /var/lib/dhcp; проверяю tmpfs-точки"
	$SUDO sed -i '\#/var/lib/NetworkManager#d' "$ROOTM/etc/fstab"
	grep -q '/var/lib/dhcp' "$ROOTM/etc/fstab" || echo 'tmpfs   /var/lib/dhcp           tmpfs   defaults,noatime,nosuid,nodev,mode=0755,size=4m    0 0' | $SUDO tee -a "$ROOTM/etc/fstab" >/dev/null
	grep -q '/run/malina-ts-raw' "$ROOTM/etc/fstab" || echo 'tmpfs   /run/malina-ts-raw      tmpfs   size=200M,mode=0755,nosuid,nodev    0 0' | $SUDO tee -a "$ROOTM/etc/fstab" >/dev/null
	awk '$3=="tmpfs"{print $2}' "$ROOTM/etc/fstab" | while read -r mp; do
		[ -n "$mp" ] && $SUDO mkdir -p "$ROOTM$mp"
	done
	$SUDO rm -rf "$ROOTM/etc/NetworkManager"
fi

# --- 2.14 tmpfiles ---
if [ "$PLAN" = 1 ]; then
	plan "systemd-tmpfiles --root=\$ROOT --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf (best-effort)"
else
	$SUDO systemd-tmpfiles --root="$ROOTM" --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf >/dev/null 2>&1 || true
fi

# --- 2.15 sync, размонтирование, offline-проверка ---
if [ "$PLAN" = 1 ]; then
	plan ""
	plan "sync; umount p3/p2; (для loop — losetup -d)."
	plan "Проверка: /malina в rootfs; fstab LABEL=malina+/var/lib/dhcp, без NetworkManager; бинарники 7/7; rootfs /usr/local пусто; ассеты >0"
	exit 0
fi

log "sync и размонтирование"
$SUDO sync
$SUDO umount "$MALM"
$SUDO umount "$ROOTM"

log "offline-проверка (mount ro)"
$SUDO mount -o ro "$(PART 2)" "$ROOTM"
$SUDO mount -o ro "$(PART 3)" "$MALM"
echo "[verify] /malina dir: $( [ -d "$ROOTM/malina" ] && echo ok || echo MISSING )"
echo "[verify] fstab: malina=$(grep -c 'LABEL=malina' "$ROOTM/etc/fstab") dhcp=$(grep -c '/var/lib/dhcp' "$ROOTM/etc/fstab") nm=$(grep -c NetworkManager "$ROOTM/etc/fstab")"
echo "[verify] /malina binaries: $(ls "$MALM/usr/local/bin" "$MALM/usr/local/sbin" 2>/dev/null | grep -cE 'malina-web|malina-netd|map-reader|kes-reader|bms-reader|bms-proxy|malina-tsdb')/7"
echo "[verify] rootfs /usr/local пусто: $(ls -A "$ROOTM/usr/local/bin" "$ROOTM/usr/local/sbin" 2>/dev/null | wc -l)"
echo "[verify] assets: $(find "$MALM/usr/local/share/malina/web" -type f 2>/dev/null | wc -l)"
$SUDO umount "$MALM" "$ROOTM"
[ -n "$LOOP" ] && { $SUDO losetup -d "$LOOP"; LOOP=""; }

log "готово"