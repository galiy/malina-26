#!/bin/sh
# Провижининг устройства из репозитория (запускать НА устройстве, от root).
# Идемпотентно. Ожидает копию репозитория в $REPO (по умолчанию /opt/malina-repo).
#
# Делает: раздел/монтирование /malina, пакеты, маски/отключения, установку
# артефактов по deploy/manifest.tsv, бинарников и PAM-хелпера, machine-id,
# симлинки, tmpfiles.
set -e

REPO="${REPO:-/opt/malina-repo}"
MALINA_SIZE_MB="${MALINA_SIZE_MB:-5120}"   # 5 GiB под «16 ГБ» карту
PROV="$REPO/system/provision"

[ -d "$REPO" ] || { echo "нет репозитория: $REPO" >&2; exit 1; }

restore_ro() { mount -o remount,ro / 2>/dev/null || true; }
mount -o remount,rw /
trap restore_ro EXIT

echo "== 1. Раздел /malina =="
if ! findmnt -n /malina >/dev/null 2>&1; then
	created=0
	if [ ! -b /dev/mmcblk0p3 ]; then
		p2s="$(cat /sys/class/block/mmcblk0p2/start)"
		p2z="$(cat /sys/class/block/mmcblk0p2/size)"
		start=$(( ((p2s + p2z + 2047) / 2048) * 2048 ))
		secs=$(( MALINA_SIZE_MB * 1024 * 1024 / 512 ))
		echo "  создаю p3: start=$start size=$secs (секторов)"
		echo "${start},${secs},83" | sfdisk --no-reread -N 3 /dev/mmcblk0
		partx -a -n 3 /dev/mmcblk0 2>/dev/null || partprobe /dev/mmcblk0 2>/dev/null || true
		udevadm settle 2>/dev/null || sleep 1
		created=1
	fi
	# Форматируем ТОЛЬКО новый раздел или заведомо пустой (без ФС).
	# Иначе повторный провижининг уничтожил бы данные в /malina.
	if [ "$created" = 1 ] || ! blkid /dev/mmcblk0p3 >/dev/null 2>&1; then
		mkfs.ext4 -F -q -L malina /dev/mmcblk0p3 2>/dev/null || true
	else
		echo "  p3 уже содержит ФС — форматирование пропущено"
	fi
	mkdir -p /malina
	mount LABEL=malina /malina 2>/dev/null || mount /dev/mmcblk0p3 /malina
fi
grep -q 'LABEL=malina' /etc/fstab || echo 'LABEL=malina  /malina  ext4  defaults,noatime  0  2' >> /etc/fstab

echo "== 2. Пакеты =="
apt-get update -qq || true
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
	rsyslog iw rfkill hostapd dnsmasq nftables isc-dhcp-client gcc 2>&1 | tail -2 || true

echo "== 3. Маски/отключения =="
if [ -f "$PROV/masked.txt" ]; then
	while read -r u; do [ -n "$u" ] && systemctl mask "$u" >/dev/null 2>&1 || true; done < "$PROV/masked.txt"
fi
if [ -f "$PROV/disabled.txt" ]; then
	while read -r u; do [ -n "$u" ] && systemctl disable --now "$u" >/dev/null 2>&1 || true; done < "$PROV/disabled.txt"
fi

echo "== 4. Артефакты по манифесту =="
while IFS="	" read -r src dest mode pol; do
	[ -z "${src:-}" ] && continue
	case "$src" in \#*) continue ;; esac
	if [ "$pol" = "if_absent" ] && [ -e "$dest" ]; then
		continue
	fi
	install -D -m "$mode" "$REPO/$src" "$dest"
done < "$REPO/deploy/manifest.tsv"

echo "== 4b. Веб-ассеты =="
mkdir -p /malina/usr/local/share/malina/web
rm -rf /malina/usr/local/share/malina/web/templates /malina/usr/local/share/malina/web/static /malina/usr/local/share/malina/web/i18n
cp -a "$REPO/web/assets/templates" "$REPO/web/assets/static" "$REPO/web/assets/i18n" /malina/usr/local/share/malina/web/

echo "== 5. Бинарники и PAM-хелпер =="
[ -f "$REPO/web/dist/malina-web-armv7" ] && install -D -m0755 "$REPO/web/dist/malina-web-armv7" /malina/usr/local/bin/malina-web
[ -f "$REPO/web/dist/malina-netd-armv7" ] && install -D -m0755 "$REPO/web/dist/malina-netd-armv7" /malina/usr/local/sbin/malina-netd
if command -v gcc >/dev/null 2>&1 && [ -f "$REPO/web/pamcheck/pamcheck.c" ]; then
	gcc -O2 -o /tmp/malina-pamcheck "$REPO/web/pamcheck/pamcheck.c" -ldl
	install -D -m0755 /tmp/malina-pamcheck /malina/usr/local/libexec/malina-pamcheck
	rm -f /tmp/malina-pamcheck
fi

echo "== 6. machine-id, hostname, симлинки =="
[ -s "$PROV/machine-id" ] && install -m0444 "$PROV/machine-id" /etc/machine-id
[ -s "$PROV/hostname" ] && { mkdir -p /malina/etc; install -m0644 "$PROV/hostname" /malina/etc/hostname; }
mkdir -p /var/lib/dbus
ln -sf /etc/machine-id /var/lib/dbus/machine-id
mkdir -p /run/malina-net
ln -sf /run/malina-net/resolv.conf /etc/resolv.conf
mkdir -p /malina/etc /malina/home /malina/root
[ -d /etc/wpa_supplicant ] && [ ! -L /etc/wpa_supplicant ] && {
	cp -a /etc/wpa_supplicant /malina/etc/wpa_supplicant 2>/dev/null || true
	rm -rf /etc/wpa_supplicant
	ln -s /malina/etc/wpa_supplicant /etc/wpa_supplicant
}

echo "== 7. tmpfiles, daemon-reload, enable =="
systemd-tmpfiles --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf >/dev/null 2>&1 || true
systemctl daemon-reload
for u in dhclient-eth0 malina-hostname malina-web malina-net malina-net-fallback.timer malina-logrotate.timer; do
	systemctl enable "$u" >/dev/null 2>&1 || true
done

echo "provision: done (reboot рекомендован)"
