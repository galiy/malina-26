#!/bin/bash
# Единый деплой артефактов проекта на устройство (единый список сервисов).
#
# Использование:
#   deploy/deploy.sh [--build] [--first-run] [user@host]
#
#   --build      пересобрать ARM-бинарники (web, map-reader, kes-reader, tsdb)
#   --first-run  ПЕРВИЧНОЕ развёртывание «голого» устройства: units/конфиги по
#                `deploy/manifest.tsv`, fstab-tmpfs `/run/malina-ts-raw`,
#                `systemd-tmpfiles`, сборка `pamcheck`, enable юнитов, чистка
#                legacy; корень монтируется rw на весь блок.
#   (без флага)  точечное ОБНОВЛЕНИЕ работающего устройства: только бинарники +
#                веб-ассеты + unit-файлы сервисов; `/etc` — коротким rw-окном;
#                ничего не создаёт (fstab/tmpfiles/enable не трогает).
#   user@host    по умолчанию malina@192.168.13.60 (или env HOST)
#
# Пароль SSH: env MALINA_PASS или строка MALINA_PASS=... в .kilo/secrets (не коммитится).
#
# Сервисы проекта (бинарники/юниты): malina-web, malina-netd, map-reader,
# kes-reader, malina-tsdb (avg), malina-tsdb-raw. `malina-netd` НЕ перезапускается
# (сетевой стек; новый бинарник вступит в силу при следующей загрузке).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

BUILD=0 FIRST=0 HOSTARG=""
for a in "$@"; do
  case "$a" in
    --build) BUILD=1 ;;
    --first-run) FIRST=1 ;;
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

# --- сборка (--build) ---
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

# --- стейдж и передача ---
if [ "$FIRST" = 1 ]; then
  # В pkg.tar кладём только нужное для `--first-run`: system/ и assets целиком, но
  # из daemons/ — лишь файлы из манифеста (.service/drop-in), без dist/ и исходников.
  mapfile -t DAEMON_FILES < <(awk -F'\t' '$1 ~ /^daemons\// {print $1}' "$ROOT/deploy/manifest.tsv")
  tar -C "$ROOT" -cf "$STAGE/pkg.tar" system deploy/manifest.tsv web/assets "${DAEMON_FILES[@]}"
  cp "$ROOT/web/pamcheck/pamcheck.c" "$STAGE/pamcheck.c"
  echo "==> первичное развёртывание на $HOST"
  "${SSH[@]}" "$HOST" 'rm -rf /tmp/malina-install && mkdir -p /tmp/malina-install'
  "${SCP[@]}" "$STAGE/pkg.tar" "$STAGE/malina-web" "$STAGE/malina-netd" "$STAGE/map-reader" \
    "$STAGE/kes-reader" "$STAGE/malina-tsdb" "$STAGE/pamcheck.c" "$HOST:/tmp/malina-install/"
else
  tar -C "$ROOT/web/assets" -cf "$STAGE/assets.tar" templates static i18n
  cp "$ROOT/daemons/tsdb/tsdb.service"              "$STAGE/malina-tsdb.service"
  cp "$ROOT/system/systemd/malina-tsdb-raw.service" "$STAGE/malina-tsdb-raw.service"
  cp "$ROOT/daemons/map-reader/map-reader.service"  "$STAGE/map-reader.service"
  cp "$ROOT/daemons/kes-reader/kes-reader.service"  "$STAGE/kes-reader.service"
  echo "==> обновление на $HOST"
  # стейдж на /malina (rw, много места); /tmp на устройстве — tmpfs 64 МБ
  "${SSH[@]}" "$HOST" 'sudo mkdir -p /malina/.deploy/tmp && sudo chown -R malina:malina /malina/.deploy/tmp && sudo rm -rf /malina/.deploy/tmp/*'
  "${SCP[@]}" "$STAGE/malina-web" "$STAGE/malina-netd" "$STAGE/map-reader" "$STAGE/kes-reader" \
    "$STAGE/malina-tsdb" "$STAGE/assets.tar" "$STAGE/malina-tsdb.service" "$STAGE/malina-tsdb-raw.service" \
    "$STAGE/map-reader.service" "$STAGE/kes-reader.service" "$HOST:/malina/.deploy/tmp/"
fi

# --- удалённая установка (общая; ветвление по FIRST) ---
"${SSH[@]}" "$HOST" "FIRST=$FIRST bash -s" <<'REMOTE'
set -e
if [ "${FIRST:-0}" = 1 ]; then
	cd /tmp/malina-install
	tar -xf pkg.tar
	cleanup() { sudo mount -o remount,ro / 2>/dev/null || true; }
	trap cleanup EXIT
	sudo mount -o remount,rw /
else
	cd /malina/.deploy/tmp
fi

# бинарники (web — bin, остальные — sbin)
sudo install -D -m0755 malina-web  /malina/usr/local/bin/malina-web
sudo install -D -m0755 malina-netd /malina/usr/local/sbin/malina-netd
sudo install -D -m0755 map-reader  /malina/usr/local/sbin/map-reader
sudo install -D -m0755 kes-reader  /malina/usr/local/sbin/kes-reader
sudo install -D -m0755 malina-tsdb /malina/usr/local/sbin/malina-tsdb

# веб-ассеты (шаблоны/статика/i18n) — путь из malina-web.service (-assets)
sudo rm -rf /malina/usr/local/share/malina/web
sudo install -d -m0755 /malina/usr/local/share/malina/web
if [ "${FIRST:-0}" = 1 ]; then
	sudo cp -a web/assets/templates web/assets/static web/assets/i18n /malina/usr/local/share/malina/web/
else
	sudo tar -C /malina/usr/local/share/malina/web -xf assets.tar
fi
sudo chown -R root:root /malina/usr/local/share/malina/web
sudo find /malina/usr/local/share/malina/web -type d -exec chmod 0755 {} +
sudo find /malina/usr/local/share/malina/web -type f -exec chmod 0644 {} +

if [ "${FIRST:-0}" = 1 ]; then
	# --- первичная инфраструктура ---
	apply() { # src dest mode policy
		src="$1"; dest="$2"; mode="$3"; pol="$4"
		if [ "$pol" = "if_absent" ] && [ -e "$dest" ]; then echo "keep  $dest"; return; fi
		sudo install -D -m "$mode" "$src" "$dest"
		echo "inst  $dest"
	}
	while IFS=$'\t' read -r src dest mode pol; do
		[ -z "${src:-}" ] && continue
		case "$src" in \#*) continue ;; esac
		apply "$src" "$dest" "$mode" "$pol"
	done < deploy/manifest.tsv

	# PAM-хелпер компилируем на устройстве. Без него вход в web (PAM) не работает —
	# при первичном развёртывании это критично, поэтому без gcc падаем.
	if command -v gcc >/dev/null 2>&1; then
		sudo gcc -O2 -o /tmp/malina-pamcheck pamcheck.c -ldl
		sudo install -D -m0755 /tmp/malina-pamcheck /malina/usr/local/libexec/malina-pamcheck
		sudo rm -f /tmp/malina-pamcheck
		echo "inst  /malina/usr/local/libexec/malina-pamcheck"
	else
		echo "ОШИБКА: нет gcc — malina-pamcheck не собран; вход в web через PAM сломан" >&2
		exit 1
	fi

	sudo systemd-tmpfiles --create /etc/tmpfiles.d/malina.conf /etc/tmpfiles.d/malina-rsyslog.conf >/dev/null 2>&1 || true
	# Устаревшие юниты: hostname/sysconfig больше не нужны.
	for u in malina-hostname.service malina-sysconfig.service; do
		sudo systemctl disable "$u" >/dev/null 2>&1 || true
		sudo rm -f "/etc/systemd/system/$u"
	done
	sudo rm -f /malina/usr/local/libexec/malina-sysconfig
	sudo rm -f /malina/etc/hostname /malina/etc/timezone
	sudo systemctl daemon-reload

	# raw-tsdb: отдельный tmpfs 200 МБ под детальное сырьё (§14)
	grep -q '/run/malina-ts-raw' /etc/fstab || echo 'tmpfs  /run/malina-ts-raw  tmpfs  size=200M,mode=0755,nosuid,nodev  0  0' | sudo tee -a /etc/fstab >/dev/null
	sudo mkdir -p /run/malina-ts-raw
	sudo mountpoint -q /run/malina-ts-raw || sudo mount /run/malina-ts-raw 2>/dev/null || true

	# Включаем только базовые юниты. Юниты режимов/сети (malina-hostapd, malina-dnsmasq,
	# malina-wpa, malina-net-wifi, malina-dhclient-wlan0, malina-net-monitor,
	# malina-net-scan, malina-net-fallback.service, malina-logrotate.service)
	# включаются/выключаются по режиму сети (malina-netd) и потому здесь не трогаются.
	for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader malina-tsdb malina-tsdb-raw; do
		sudo systemctl enable "$u" >/dev/null 2>&1 || true
	done
else
	# --- точечное обновление: unit-файлы сервисов коротким rw-окном под /etc ---
	sudo mount -o remount,rw /
	sudo install -m0644 malina-tsdb.service     /etc/systemd/system/malina-tsdb.service
	sudo install -m0644 malina-tsdb-raw.service /etc/systemd/system/malina-tsdb-raw.service
	sudo install -m0644 map-reader.service      /etc/systemd/system/map-reader.service
	sudo install -m0644 kes-reader.service      /etc/systemd/system/kes-reader.service
	sudo systemctl daemon-reload
	sync
	sudo mount -o remount,ro / || true
fi

# перезапуск сервисов (malina-netd не трогаем: сетевой стек)
for u in malina-web map-reader kes-reader malina-tsdb malina-tsdb-raw; do
	sudo systemctl restart "$u" 2>/dev/null && echo "restart $u" || echo "skip   $u (не активен/не установлен)"
done
sleep 2
echo "--- status ---"
for u in malina-web map-reader kes-reader malina-tsdb malina-tsdb-raw; do
	printf '%-16s %s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null || true)"
done
curl -s -o /dev/null -w "login http=%{http_code}\n" http://127.0.0.1/login || true

if [ "${FIRST:-0}" = 1 ]; then
	sudo rm -rf /tmp/malina-install
	echo "deploy: первичное развёртывание завершено"
else
	sudo rm -rf /malina/.deploy/tmp
fi
REMOTE
echo "==> готово ($([ "$FIRST" = 1 ] && echo 'первичное развёртывание' || echo 'обновление')) на $HOST"
