#!/bin/bash
# Установка артефактов проекта на устройство (запускать с ноутбука).
# Использование:
#   deploy/install.sh [--build] [user@host]
# Пароль SSH: переменная MALINA_PASS или строка MALINA_PASS=... в .kilo/secrets
# (файл не коммитится — см. правила проекта).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD=0
HOSTARG=""
for a in "$@"; do
  case "$a" in
    --build) BUILD=1 ;;
    *) [ -z "$HOSTARG" ] && HOSTARG="$a" ;;
  esac
done
HOST="${HOSTARG:-${HOST:-malina@192.168.13.60}}"
PASS="${MALINA_PASS:-}"
if [ -z "$PASS" ] && [ -f "$ROOT/.kilo/secrets" ]; then
  PASS="$(sed -n 's/^MALINA_PASS=//p' "$ROOT/.kilo/secrets" | head -1)"
fi
if [ -z "$PASS" ]; then
  echo "не задан SSH-пароль: MALINA_PASS (env) или MALINA_PASS=... в .kilo/secrets" >&2
  exit 1
fi

SSH=(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null)
SCP=(sshpass -p "$PASS" scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null)

if [ "$BUILD" = 1 ]; then
  make -C "$ROOT/web" arm
fi

BIN_WEB="$ROOT/web/dist/malina-web-armv7"
BIN_NETD="$ROOT/web/dist/malina-netd-armv7"
[ -f "$BIN_WEB" ] || { echo "нет $BIN_WEB (собери: --build)"; exit 1; }
[ -f "$BIN_NETD" ] || { echo "нет $BIN_NETD (собери: --build)"; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp "$BIN_WEB" "$STAGE/malina-web"
cp "$BIN_NETD" "$STAGE/malina-netd"
cp "$ROOT/web/pamcheck/pamcheck.c" "$STAGE/pamcheck.c"
tar -C "$ROOT" -cf "$STAGE/pkg.tar" system deploy/manifest.tsv web/assets

"${SSH[@]}" "$HOST" 'rm -rf /tmp/malina-install && mkdir -p /tmp/malina-install'
"${SCP[@]}" "$STAGE/pkg.tar" "$STAGE/malina-web" "$STAGE/malina-netd" "$STAGE/pamcheck.c" "$HOST:/tmp/malina-install/"

"${SSH[@]}" "$HOST" 'bash -s' <<'REMOTE'
set -e
cd /tmp/malina-install
tar -xf pkg.tar
cleanup() { sudo mount -o remount,ro / 2>/dev/null || true; }
trap cleanup EXIT
sudo mount -o remount,rw /

apply() { # src dest mode policy
	src="$1"; dest="$2"; mode="$3"; pol="$4"
	if [ "$pol" = "if_absent" ] && [ -e "$dest" ]; then
		echo "keep  $dest"
		return
	fi
	sudo install -D -m "$mode" "$src" "$dest"
	echo "inst  $dest"
}

while IFS=$'\t' read -r src dest mode pol; do
	[ -z "${src:-}" ] && continue
	case "$src" in \#*) continue ;; esac
	apply "$src" "$dest" "$mode" "$pol"
done < deploy/manifest.tsv

# Бинарники (собраны/скопированы)
sudo install -D -m0755 malina-web  /malina/usr/local/bin/malina-web
sudo install -D -m0755 malina-netd /malina/usr/local/sbin/malina-netd
echo "inst  /malina/usr/local/bin/malina-web"
echo "inst  /malina/usr/local/sbin/malina-netd"

# Веб-ассеты (шаблоны/статика/i18n) — путь из malina-web.service (-assets).
sudo rm -rf /malina/usr/local/share/malina/web
sudo install -d -m0755 /malina/usr/local/share/malina/web
sudo cp -a web/assets/templates web/assets/static web/assets/i18n /malina/usr/local/share/malina/web/
echo "inst  /malina/usr/local/share/malina/web"

# PAM-хелпер компилируем на устройстве
if command -v gcc >/dev/null 2>&1; then
	sudo gcc -O2 -o /tmp/malina-pamcheck pamcheck.c -ldl
	sudo install -D -m0755 /tmp/malina-pamcheck /malina/usr/local/libexec/malina-pamcheck
	rm -f /tmp/malina-pamcheck
	echo "inst  /malina/usr/local/libexec/malina-pamcheck"
fi

sudo systemd-tmpfiles --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf >/dev/null 2>&1 || true
sudo systemctl daemon-reload
for u in dhclient-eth0 malina-hostname malina-web malina-net malina-net-fallback.timer malina-logrotate.timer; do
	sudo systemctl enable "$u" >/dev/null 2>&1 || true
done
rm -rf /tmp/malina-install
echo "install: done"
REMOTE
echo "Готово: артефакты установлены на $HOST"
