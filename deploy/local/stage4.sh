#!/bin/bash
# Этап 4: полный локальный деплой SD «с чистого листа» —
#   wipe (по факту перезаписи образа) → dd образа → stage2.sh → offline-проверка.
# Запуск: deploy/local/stage4.sh   (карта в ПК). Переопределения: IMG=…, SUDO_ASKPASS=…
set -euo pipefail

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
IMG="${IMG:-/opt/raspberry_tmp/pi-gen/deploy/2026-10-10-malina-lite.img}"
export SUDO_ASKPASS="${SUDO_ASKPASS:-/tmp/kilo/askpass.sh}"
S="sudo -A"
log() { echo "[stage4] $*"; }

if [ ! -f "$SUDO_ASKPASS" ]; then
	printf '#!/bin/sh\nsecret-tool lookup service sudo username sasha\n' > "$SUDO_ASKPASS"
	chmod +x "$SUDO_ASKPASS"
fi
[ -f "$IMG" ] || { log "нет образа $IMG"; exit 1; }

# --- определить SD: съёмный блочный диск > 8 ГБ ---
TARGET=""
for blk in /sys/block/sd*; do
	bn=${blk##*/}
	[ "$(cat "$blk/removable" 2>/dev/null || echo 0)" = 1 ] || continue
	sz=$(cat "$blk/size" 2>/dev/null || echo 0)
	[ "$sz" -gt 10000000 ] || continue
	TARGET="/dev/$bn"; break
done
[ -n "$TARGET" ] || { log "SD-карта не найдена (removable >8G)"; exit 1; }
log "target=$TARGET size=$(blockdev --getsize64 "$TARGET")"
log "image=$IMG ($(stat -c %y "$IMG" | cut -d. -f1))"

# --- размонтировать всё на карте ---
while read -r pn; do
	[ -n "$pn" ] && $S umount "/dev/$pn" 2>/dev/null && log "unmounted /dev/$pn" || true
done < <(lsblk -ln -o NAME "$TARGET" | tail -n +2)

# --- Этап 1: образ ---
log "запись образа (dd)…"
$S dd if="$IMG" of="$TARGET" bs=4M conv=fsync status=progress 2>&1 | tail -2
sync
$S partprobe "$TARGET" 2>/dev/null || true
$S udevadm settle 2>/dev/null || sleep 1
log "после dd: $(lsblk -ln -o NAME,SIZE,FSTYPE,LABEL "$TARGET" | tr '\n' '|')"

# --- Этап 2: артефакты ---
log "Этап 2 (stage2.sh)…"
SUDO_ASKPASS="$SUDO_ASKPASS" SUDO="sudo -A" "$REPO/deploy/local/stage2.sh" "$TARGET" 2>&1 | grep -E "^==|ОШИБКА|error" | tail -40

# --- offline-проверка ---
for p in "${TARGET}2" "${TARGET}3"; do $S umount "$p" 2>/dev/null || true; done
mkdir -p /tmp/kilo/r /tmp/kilo/m
$S mount -o ro "${TARGET}2" /tmp/kilo/r
$S mount -o ro "${TARGET}3" /tmp/kilo/m
echo "[verify] /malina dir: $( [ -d /tmp/kilo/r/malina ] && echo ok || echo MISSING )"
echo "[verify] fstab: malina=$(grep -c 'LABEL=malina' /tmp/kilo/r/etc/fstab) dhcp=$(grep -c '/var/lib/dhcp' /tmp/kilo/r/etc/fstab) nm=$(grep -c NetworkManager /tmp/kilo/r/etc/fstab)"
echo "[verify] /malina binaries: $(ls /tmp/kilo/m/usr/local/bin /tmp/kilo/m/usr/local/sbin 2>/dev/null | grep -cE 'malina-web|malina-netd|map-reader|malina-rw')/4"
echo "[verify] rootfs /usr/local пусто: $(ls -A /tmp/kilo/r/usr/local/bin /tmp/kilo/r/usr/local/sbin 2>/dev/null | wc -l)"
echo "[verify] assets: card=$(find /tmp/kilo/m/usr/local/share/malina/web -type f 2>/dev/null | wc -l)"
$S umount /tmp/kilo/m /tmp/kilo/r
log "DONE"
