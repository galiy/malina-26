#!/bin/bash
# Точечное обновление артефактов проекта на УЖЕ развёрнутом устройстве.
# (Первичное развёртывание — deploy/install.sh; офлайн-карта — deploy/local/stage2.sh.)
#
# Использование:
#   deploy/update.sh [--build] [user@host]
#
#   --build      пересобрать ARM-бинарники (web + map-reader + kes-reader) перед выкладкой
#   user@host    целевое устройство; по умолчанию malina@192.168.13.60 (или env HOST)
#
# Пароль SSH: env MALINA_PASS или строка MALINA_PASS=... в .kilo/secrets (не коммитится).
#
# Что делает: выкладывает бинарники (malina-web, malina-netd, map-reader, kes-reader)
# и веб-ассеты на rw-раздел /malina, перезапускает сервисы и проверяет их статус.
# malina-netd НЕ перезапускается автоматически (это сетевой стек; рестарт по SSH
# может оборвать связь) — новый бинарник вступит в силу при следующей загрузке.
#
# Стейдж — на /malina (временный /tmp на устройстве — tmpfs всего 64 МБ).
# Бэкапы на устройстве НЕ делаем (правило проекта для .60).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

BUILD=0
HOSTARG=""
for a in "$@"; do
  case "$a" in
    --build) BUILD=1 ;;
    -*) echo "неизвестный флаг: $a" >&2; exit 2 ;;
    *) [ -z "$HOSTARG" ] && HOSTARG="$a" ;;
  esac
done
HOST="${HOSTARG:-${HOST:-malina@192.168.13.60}}"
[ "${HOST#-}" = "$HOST" ] || { echo "некорректный хост: $HOST" >&2; exit 2; }

PASS="${MALINA_PASS:-}"
if [ -z "$PASS" ] && [ -f "$ROOT/.kilo/secrets" ]; then
  PASS="$(sed -n 's/^MALINA_PASS=//p' "$ROOT/.kilo/secrets" | head -1)"
fi
if [ -z "$PASS" ]; then
  echo "не задан SSH-пароль: MALINA_PASS (env) или MALINA_PASS=... в .kilo/secrets" >&2
  exit 1
fi
command -v sshpass >/dev/null || { echo "нет sshpass" >&2; exit 1; }

SSH=(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10)
SCP=(sshpass -p "$PASS" scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null)

if [ "$BUILD" = 1 ]; then
  make -C "$ROOT/web" arm
  make -C "$ROOT/daemons/map-reader" arm
  make -C "$ROOT/daemons/kes-reader" arm
  make -C "$ROOT/daemons/tsdb" arm
fi

BIN_WEB="$ROOT/web/dist/malina-web-armv7"
BIN_NETD="$ROOT/web/dist/malina-netd-armv7"
BIN_MAP="$ROOT/daemons/map-reader/dist/map-reader-armv7"
BIN_KES="$ROOT/daemons/kes-reader/dist/kes-reader-armv7"
BIN_TSDB="$ROOT/daemons/tsdb/dist/malina-tsdb-armv7"
for f in "$BIN_WEB" "$BIN_NETD" "$BIN_MAP" "$BIN_KES" "$BIN_TSDB"; do
  [ -f "$f" ] || { echo "нет $f (собери: $0 --build)"; exit 1; }
done

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp "$BIN_WEB"  "$STAGE/malina-web"
cp "$BIN_NETD" "$STAGE/malina-netd"
cp "$BIN_MAP"  "$STAGE/map-reader"
cp "$BIN_KES"  "$STAGE/kes-reader"
cp "$BIN_TSDB" "$STAGE/malina-tsdb"
tar -C "$ROOT/web/assets" -cf "$STAGE/assets.tar" templates static i18n

echo "==> выкладка на $HOST"
# стейдж на /malina (rw, много места); /tmp на устройстве — tmpfs 64 МБ
"${SSH[@]}" "$HOST" 'sudo mkdir -p /malina/.deploy/tmp && sudo chown -R malina:malina /malina/.deploy/tmp && sudo rm -rf /malina/.deploy/tmp/*'
"${SCP[@]}" "$STAGE/malina-web" "$STAGE/malina-netd" "$STAGE/map-reader" "$STAGE/kes-reader" "$STAGE/malina-tsdb" "$STAGE/assets.tar" \
  "$HOST:/malina/.deploy/tmp/"

"${SSH[@]}" "$HOST" 'bash -s' <<'REMOTE'
set -e
cd /malina/.deploy/tmp
# бинарники (root:root 0755), каталог /malina — rw, remount корня не нужен
sudo install -m0755 malina-web  /malina/usr/local/bin/malina-web
sudo install -m0755 malina-netd /malina/usr/local/sbin/malina-netd
sudo install -m0755 map-reader  /malina/usr/local/sbin/map-reader
sudo install -m0755 kes-reader  /malina/usr/local/sbin/kes-reader
sudo install -m0755 malina-tsdb /malina/usr/local/sbin/malina-tsdb
# веб-ассеты
sudo rm -rf /malina/usr/local/share/malina/web
sudo install -d -m0755 /malina/usr/local/share/malina/web
sudo tar -C /malina/usr/local/share/malina/web -xf assets.tar
sudo chown -R root:root /malina/usr/local/share/malina/web
sudo find /malina/usr/local/share/malina/web -type d -exec chmod 0755 {} +
sudo find /malina/usr/local/share/malina/web -type f -exec chmod 0644 {} +
# перезапуск сервисов (malina-netd не трогаем: сетевой стек)
for u in malina-web map-reader kes-reader malina-tsdb; do
  sudo systemctl restart "$u" 2>/dev/null && echo "restart $u" || echo "skip   $u (не активен/не установлен)"
done
sleep 2
echo "--- status ---"
for u in malina-web map-reader kes-reader malina-tsdb; do
  printf '%-12s %s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null || true)"
done
curl -s -o /dev/null -w "login http=%{http_code}\n" http://127.0.0.1/login || true
sudo rm -rf /malina/.deploy/tmp
REMOTE
echo "==> готово: обновление выложено на $HOST"
