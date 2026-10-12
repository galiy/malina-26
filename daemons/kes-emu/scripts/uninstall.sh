#!/bin/sh
# Удаление службы эмулятора КЭС. По умолчанию бинарник и карту памяти НЕ удаляет.
#   uninstall.sh [-p]   # -p — также удалить бинарник, карту памяти и каталог /opt/kes-emu
# Учитывает ro-корень: для правки /etc/systemd/system временно remount,rw / .
set -eu

PROG=kes-emu
UNIT=/etc/systemd/system/${PROG}.service

PURGE=0
[ "${1:-}" = "-p" ] && PURGE=1

systemctl stop "${PROG}" 2>/dev/null || true
systemctl disable "${PROG}" 2>/dev/null || true

ROOT_RW=0
if [ ! -w /etc/systemd/system ]; then
	mount -o remount,rw / && ROOT_RW=1
fi
trap 'if [ "${ROOT_RW}" = 1 ]; then sync; mount -o remount,ro / || true; fi' EXIT INT TERM

rm -f "${UNIT}"
systemctl daemon-reload

if [ "${PURGE}" = "1" ]; then
	for d in /malina /settings /opt/kes-emu; do
		[ -d "${d}" ] || continue
		rm -f "${d}/${PROG}" "${d}/${PROG}.json"
	done
	rmdir /opt/kes-emu 2>/dev/null || true
fi
echo "служба ${PROG} удалена"
