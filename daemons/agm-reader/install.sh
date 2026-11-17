#!/usr/bin/env bash
# Установка agm-pzem-usbip на ПАК. Запускать от root:
#   sudo ./install.sh        (из каталога daemons/agm-reader)
#
# Скрипт и unit кладутся в /malina и /etc; /etc меняется через malina-rw
# (rw-окно с sync), чтобы не оставить грязный журнал на ro-корне.
set -euo pipefail

SRC=$(cd "$(dirname "$0")" && pwd)
BIN=/malina/usr/local/sbin/agm-pzem-usbip
UNIT=/etc/systemd/system/malina-agm-pzem-usbip.service
NAME=malina-agm-pzem-usbip

if [ "$(id -u)" -ne 0 ]; then
	exec sudo "$0" "$@"
fi

if ! command -v usbip >/dev/null 2>&1 && [ ! -x /usr/sbin/usbip ]; then
	echo "ВНИМАНИЕ: нет usbip (пакет usbip)" >&2
fi
if ! command -v usbipd >/dev/null 2>&1 && [ ! -x /usr/sbin/usbipd ]; then
	echo "ВНИМАНИЕ: нет usbipd (пакет usbip)" >&2
fi
if ! modinfo usbip-host >/dev/null 2>&1; then
	echo "ВНИМАНИЕ: модуль usbip-host не найден в текущем ядре" >&2
fi

# malina-rw лежит на /malina, а не в PATH.
RW=()
if [ -x /malina/usr/local/sbin/malina-rw ]; then
	RW=(/malina/usr/local/sbin/malina-rw run)
elif command -v malina-rw >/dev/null 2>&1; then
	RW=(malina-rw run)
else
	echo "нет malina-rw: не могу безопасно изменить /etc на ro-корне" >&2
	exit 1
fi

# Атомарная замена бинарника на rw-разделе /malina.
install -D -m0755 "$SRC/agm-pzem-usbip.sh" "$BIN.new"
mv -f "$BIN.new" "$BIN"

# Unit + enable/restart в одном rw-окне; замена через mv, без перезаписи inode.
"${RW[@]}" sh -ec "
	install -m0644 '$SRC/agm-pzem-usbip.service' '$UNIT.new'
	mv -f '$UNIT.new' '$UNIT'
	systemctl daemon-reload
	systemctl enable '$NAME'
	systemctl restart '$NAME'
"

systemctl --no-pager --lines=5 status "$NAME" || true
