#!/bin/bash
# Единый деплой артефактов проекта на устройство (единый список сервисов).
#
# Использование:
#   deploy/deploy.sh [--build] [--first-run] [--only s1,s2,...] [--purge-bms-history] [user@host]
#
#   --build      пересобрать ARM-бинарники выбранных сервисов
#   --first-run  ПЕРВИЧНОЕ развёртывание «голого» устройства: units/конфиги по
#                `deploy/manifest.tsv`, fstab-tmpfs `/run/malina-ts-raw`,
#                `systemd-tmpfiles`, сборка `pamcheck`, enable юнитов, чистка
#                legacy; корень монтируется rw на весь блок. (Всегда полное;
#                `--only` при `--first-run` игнорируется.)
#   --only LIST  точечное обновление ТОЛЬКО выбранных сервисов (через запятую):
#                web,netd,map,kes,bmsr,bmsp,tsdb,grid.
#   --purge-bms-history
#                одноразовая миграция: остановить bms-reader/tsdb, удалить ВСЮ
#                историю bms (старые ID BASE-NNNNN) в avg и raw, затем поднять
#                сервисы. Требует bmsr или --first-run; tsdb добавляется в --only.
#   --all        обновить ВСЕ сервисы (весь скрипт). Без `--only`/`--all` обычный
#                деплой НЕ запускается (правило: на .68 деплоим только изменённые).
#   (без флага)  точечное ОБНОВЛЕНИЕ работающего устройства: только бинарники +
#                веб-ассеты + unit-файлы выбранных сервисов; `/etc` — коротким
#                rw-окном.
#   user@host    по умолчанию malina@192.168.13.68 (или env HOST)
#
# Пароль SSH: env MALINA_PASS или строка MALINA_PASS=... в .kilo/secrets (не коммитится).
#
#   --grid-config PATH — установить конфиг grid-reader (иначе не трогаем).
# Сервисы: malina-web, malina-netd, map-reader, kes-reader, bms-reader (bmsr),
# bms-proxy (bmsp), malina-tsdb (avg), malina-tsdb-raw, grid-reader (grid).
# `malina-netd` НЕ перезапускается.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

ALL_SERVICES="web netd map kes bmsr bmsp tsdb grid"

BUILD=0 FIRST=0 ALL=0 PURGE=0 HOSTARG="" ONLY="" GRIDCONFIG=""
parse() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --build) BUILD=1 ;;
      --first-run) FIRST=1 ;;
      --purge-bms-history) PURGE=1 ;;
      --all) ALL=1 ;;
      --only) shift; ONLY="${1:-}" ;;
      --only=*) ONLY="${1#--only=}" ;;
      --grid-config) shift; GRIDCONFIG="${1:-}" ;;
      --grid-config=*) GRIDCONFIG="${1#--grid-config=}" ;;
      -*) echo "неизвестный флаг: $1" >&2; exit 2 ;;
      *) [ -z "$HOSTARG" ] && HOSTARG="$1" ;;
    esac
    shift
  done
}
parse "$@"

# Нормализуем список сервисов.
ONLY="${ONLY//,/ }"
if [ -z "${ONLY// /}" ]; then
  if [ "$ALL" = 1 ] || [ "$FIRST" = 1 ]; then
    ONLY="$ALL_SERVICES"
  else
    echo "укажите --only <список> (обновить только изменённые сервисы) или --all" >&2
    echo "сервисы: $ALL_SERVICES" >&2
    exit 2
  fi
fi
for s in $ONLY; do
  case " $ALL_SERVICES " in
    *" $s "*) ;;
    *) echo "неизвестный сервис: '$s' (допустимо: $ALL_SERVICES)" >&2; exit 2 ;;
  esac
done

has() { case " $ONLY " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

if [ "$PURGE" = 1 ] && [ "$FIRST" != 1 ] && ! has bmsr; then
  echo "--purge-bms-history требует bmsr в --only или --first-run" >&2
  exit 2
fi
# purge требует совместимый tsdb (validKey 50) — добавляем его в обновление.
if [ "$PURGE" = 1 ] && [ "$FIRST" != 1 ] && ! has tsdb; then
  ONLY="$ONLY tsdb"
fi

HOST="${HOSTARG:-${HOST:-malina@192.168.13.68}}"
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

echo "==> сервисы: $ONLY$([ "$FIRST" = 1 ] && echo ' (first-run — полное развёртывание)')"

# --- сборка (--build) выбранных сервисов ---
if [ "$BUILD" = 1 ]; then
  has web  && make -C "$ROOT/web" arm
  has map  && make -C "$ROOT/daemons/map-reader" arm
  has kes  && make -C "$ROOT/daemons/kes-reader" arm
  has bmsr && make -C "$ROOT/daemons/bms-reader" arm
  has bmsp && make -C "$ROOT/daemons/bms-proxy" arm
  has tsdb && make -C "$ROOT/daemons/tsdb" arm
  has grid && make -C "$ROOT/daemons/grid-reader" arm
fi
# netd собирается вместе с web (один модуль web)
if [ "$BUILD" = 1 ] && has netd && ! has web; then
  make -C "$ROOT/web" arm
fi

BIN_WEB="$ROOT/web/dist/malina-web-armv7"
BIN_NETD="$ROOT/web/dist/malina-netd-armv7"
BIN_MAP="$ROOT/daemons/map-reader/dist/map-reader-armv7"
BIN_KES="$ROOT/daemons/kes-reader/dist/kes-reader-armv7"
BIN_BMSR="$ROOT/daemons/bms-reader/dist/bms-reader-armv7"
BIN_BMSP="$ROOT/daemons/bms-proxy/dist/bms-proxy-armv7"
BIN_TSDB="$ROOT/daemons/tsdb/dist/malina-tsdb-armv7"
BIN_GRID="$ROOT/daemons/grid-reader/dist/grid-reader-armv7"

req() { [ -f "$1" ] || { echo "нет $1 (собери: $0 --build --only $2)"; exit 1; }; }
has web  && { req "$BIN_WEB"  web; }
has netd && { req "$BIN_NETD" netd; }
has map  && { req "$BIN_MAP"  map; }
has kes  && { req "$BIN_KES"  kes; }
has bmsr && { req "$BIN_BMSR" bmsr; }
has bmsp && { req "$BIN_BMSP" bmsp; }
has tsdb && { req "$BIN_TSDB" tsdb; }
has grid && { req "$BIN_GRID" grid; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
has web  && cp "$BIN_WEB"  "$STAGE/malina-web"
has netd && cp "$BIN_NETD" "$STAGE/malina-netd"
has map  && cp "$BIN_MAP"  "$STAGE/map-reader"
has kes  && cp "$BIN_KES"  "$STAGE/kes-reader"
has bmsr && cp "$BIN_BMSR" "$STAGE/bms-reader"
has bmsp && cp "$BIN_BMSP" "$STAGE/bms-proxy"
has tsdb && cp "$BIN_TSDB" "$STAGE/malina-tsdb"
has grid && cp "$BIN_GRID" "$STAGE/grid-reader"

# --- сборка списка файлов для передачи ---
FILES=( )
has web  && FILES+=( "$STAGE/malina-web" )
has netd && FILES+=( "$STAGE/malina-netd" )
has map  && FILES+=( "$STAGE/map-reader" )
has kes  && FILES+=( "$STAGE/kes-reader" )
has bmsr && FILES+=( "$STAGE/bms-reader" )
has bmsp && FILES+=( "$STAGE/bms-proxy" )
has tsdb && FILES+=( "$STAGE/malina-tsdb" )
has grid && FILES+=( "$STAGE/grid-reader" )

if [ "$FIRST" = 1 ]; then
  # --- первичное развёртывание (полное) ---
  mapfile -t DAEMON_FILES < <(awk -F'\t' '$1 ~ /^daemons\// {print $1}' "$ROOT/deploy/manifest.tsv")
  tar -C "$ROOT" -cf "$STAGE/pkg.tar" system deploy/manifest.tsv web/assets "${DAEMON_FILES[@]}"
  cp "$ROOT/web/pamcheck/pamcheck.c" "$STAGE/pamcheck.c"
  echo "==> первичное развёртывание на $HOST"
  "${SSH[@]}" "$HOST" 'rm -rf /tmp/malina-install && mkdir -p /tmp/malina-install'
  "${SCP[@]}" "$STAGE/pkg.tar" "${FILES[@]}" "$STAGE/pamcheck.c" "$HOST:/tmp/malina-install/"
else
  # --- точечное обновление выбранных сервисов ---
  if has web; then
    tar -C "$ROOT/web/assets" -cf "$STAGE/assets.tar" templates static i18n
    FILES+=( "$STAGE/assets.tar" )
  fi
  if has grid && [ -n "$GRIDCONFIG" ]; then
    [ -f "$GRIDCONFIG" ] || { echo "нет --grid-config файла: $GRIDCONFIG" >&2; exit 1; }
    cp "$GRIDCONFIG" "$STAGE/grid-reader.json"
    FILES+=( "$STAGE/grid-reader.json" )
  fi
  UNITS=( )
  has map  && { cp "$ROOT/daemons/map-reader/map-reader.service"     "$STAGE/map-reader.service";        UNITS+=( "$STAGE/map-reader.service" ); }
  has kes  && { cp "$ROOT/daemons/kes-reader/kes-reader.service"     "$STAGE/kes-reader.service";        UNITS+=( "$STAGE/kes-reader.service" ); }
  has bmsr && { cp "$ROOT/daemons/bms-reader/bms-reader.service"     "$STAGE/malina-bms-reader.service"; UNITS+=( "$STAGE/malina-bms-reader.service" ); }
  has bmsp && { cp "$ROOT/daemons/bms-proxy/bms-proxy.service"       "$STAGE/malina-bms-proxy.service"; UNITS+=( "$STAGE/malina-bms-proxy.service" ); }
  has tsdb && { cp "$ROOT/daemons/tsdb/tsdb.service"                 "$STAGE/malina-tsdb.service";       UNITS+=( "$STAGE/malina-tsdb.service" ); }
  has grid && { cp "$ROOT/daemons/grid-reader/grid-reader.service"   "$STAGE/malina-grid-reader.service"; UNITS+=( "$STAGE/malina-grid-reader.service" ); }
  has tsdb && { cp "$ROOT/system/systemd/malina-tsdb-raw.service"    "$STAGE/malina-tsdb-raw.service";   UNITS+=( "$STAGE/malina-tsdb-raw.service" ); }
  # udev latency_timer — при обновлении любого serial-демона
  if has map || has kes || has bmsr || has bmsp; then
    cp "$ROOT/system/udev/60-usb-serial-latency.rules" "$STAGE/60-usb-serial-latency.rules"
    FILES+=( "$STAGE/60-usb-serial-latency.rules" )
  fi
  echo "==> обновление на $HOST: $ONLY"
  # стейдж на /malina (rw, много места); /tmp на устройстве — tmpfs 64 МБ
  "${SSH[@]}" "$HOST" 'sudo mkdir -p /malina/.deploy/tmp && sudo chown -R malina:malina /malina/.deploy/tmp && sudo rm -rf /malina/.deploy/tmp/*'
  "${SCP[@]}" "${FILES[@]}" "${UNITS[@]}" "$HOST:/malina/.deploy/tmp/"
fi

# --- удалённая установка (общая; ветвление по FIRST/ONLY) ---
"${SSH[@]}" "$HOST" "FIRST=$FIRST ONLY='$ONLY' PURGE=$PURGE bash -s" <<'REMOTE'
set -e
has() { case " $ONLY " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }
if [ "${FIRST:-0}" = 1 ]; then
	cd /tmp/malina-install
	cleanup() { sudo mount -o remount,ro / 2>/dev/null || true; }
	trap cleanup EXIT
	sudo mount -o remount,rw /
else
	cd /malina/.deploy/tmp
fi

# бинарники (web — bin, остальные — sbin). Эхо — чтобы видеть, что реально уехало.
echo "install: $(ls -1 2>/dev/null | tr '\n' ' ')"
[ -f malina-web ]    && { sudo install -D -m0755 malina-web    /malina/usr/local/bin/malina-web;  echo "inst /malina/usr/local/bin/malina-web"; }
[ -f malina-netd ]   && { sudo install -D -m0755 malina-netd   /malina/usr/local/sbin/malina-netd; echo "inst malina-netd"; }
[ -f map-reader ]    && { sudo install -D -m0755 map-reader    /malina/usr/local/sbin/map-reader;  echo "inst map-reader"; }
[ -f kes-reader ]    && { sudo install -D -m0755 kes-reader    /malina/usr/local/sbin/kes-reader;  echo "inst kes-reader"; }
[ -f bms-reader ]    && { sudo install -D -m0755 bms-reader    /malina/usr/local/sbin/bms-reader;  echo "inst bms-reader"; }
[ -f bms-proxy ]     && { sudo install -D -m0755 bms-proxy     /malina/usr/local/sbin/bms-proxy;   echo "inst bms-proxy"; }
[ -f malina-tsdb ]   && { sudo install -D -m0755 malina-tsdb   /malina/usr/local/sbin/malina-tsdb; echo "inst malina-tsdb"; }
[ -f grid-reader ]   && { sudo install -D -m0755 grid-reader   /malina/usr/local/sbin/grid-reader; echo "inst grid-reader"; }
[ -f grid-reader.json ] && { sudo install -D -m0600 grid-reader.json /malina/etc/malina/grid-reader.json; echo "inst grid-reader.json"; }

# веб-ассеты — только если уезжали (обновление web/first-run)
if [ -f assets.tar ] || [ "${FIRST:-0}" = 1 ]; then
	echo "inst assets.tar -> /malina/usr/local/share/malina/web"
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
fi

if [ "${FIRST:-0}" = 1 ]; then
	# --- первичная инфраструктура (полная) ---
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
	for u in malina-hostname.service malina-sysconfig.service; do
		sudo systemctl disable "$u" >/dev/null 2>&1 || true
		sudo rm -f "/etc/systemd/system/$u"
	done
	sudo rm -f /malina/usr/local/libexec/malina-sysconfig
	sudo rm -f /malina/etc/hostname /malina/etc/timezone
	sudo systemctl daemon-reload

	grep -q '/run/malina-ts-raw' /etc/fstab || echo 'tmpfs  /run/malina-ts-raw  tmpfs  size=200M,mode=0755,nosuid,nodev  0  0' | sudo tee -a /etc/fstab >/dev/null
	sudo mkdir -p /run/malina-ts-raw
	sudo mountpoint -q /run/malina-ts-raw || sudo mount /run/malina-ts-raw 2>/dev/null || true

	for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader malina-bms-reader malina-bms-proxy malina-tsdb malina-tsdb-raw malina-grid-reader; do
		sudo systemctl enable "$u" >/dev/null 2>&1 || true
	done
else
	# --- точечное обновление: unit-файлы выбранных сервисов (rw-окно под /etc) ---
	if [ -f malina-tsdb.service ] || [ -f malina-tsdb-raw.service ] || [ -f map-reader.service ] || [ -f kes-reader.service ] || [ -f malina-bms-reader.service ] || [ -f malina-bms-proxy.service ] || [ -f malina-grid-reader.service ] || [ -f 60-usb-serial-latency.rules ]; then
		sudo mount -o remount,rw /
		[ -f malina-tsdb.service ]       && sudo install -m0644 malina-tsdb.service       /etc/systemd/system/malina-tsdb.service
		[ -f malina-tsdb-raw.service ]   && sudo install -m0644 malina-tsdb-raw.service   /etc/systemd/system/malina-tsdb-raw.service
		[ -f map-reader.service ]        && sudo install -m0644 map-reader.service        /etc/systemd/system/map-reader.service
		[ -f kes-reader.service ]        && sudo install -m0644 kes-reader.service        /etc/systemd/system/kes-reader.service
		[ -f malina-bms-reader.service ] && sudo install -m0644 malina-bms-reader.service /etc/systemd/system/malina-bms-reader.service
		[ -f malina-bms-proxy.service ]  && sudo install -m0644 malina-bms-proxy.service  /etc/systemd/system/malina-bms-proxy.service
		[ -f malina-grid-reader.service ] && sudo install -m0644 malina-grid-reader.service /etc/systemd/system/malina-grid-reader.service
		[ -f 60-usb-serial-latency.rules ] && sudo install -m0644 60-usb-serial-latency.rules /etc/udev/rules.d/60-usb-serial-latency.rules
		sudo systemctl daemon-reload
		sync
		sudo mount -o remount,ro / || true
	fi
	if [ -f 60-usb-serial-latency.rules ]; then
		# Политика: latency_timer всегда 1 (docs/map.md). Значение — из rule-файла,
		# но иное значение считается ошибкой конфигурации.
		LT=$(sed -n 's/.*ATTR{latency_timer}="\([0-9][0-9]*\)".*/\1/p' 60-usb-serial-latency.rules | head -1)
		[ -n "$LT" ] || LT=1
		if [ "$LT" != 1 ]; then
			echo "ОШИБКА: latency_timer=$LT, политика — всегда 1 (docs/map.md)" >&2
			exit 1
		fi
		sudo udevadm control --reload-rules 2>/dev/null || true
		for d in /sys/bus/usb-serial/devices/ttyUSB*; do
			[ -e "$d/latency_timer" ] || continue
			sudo sh -c "echo $LT > $d/latency_timer" >/dev/null 2>&1 || true
		done
		echo "latency_timer=$LT applied"
	fi
fi

# Одноразовая миграция ID: удаляем весь namespace bms (старые <BASE>-NNNNN).
# Выполняется после установки бинарников/юнитов, до перезапуска сервисов.
PURGE_MARKER=/malina/etc/malina/.bms-history-purged
RWCTL=/malina/usr/local/sbin/malina-rw
PURGE_RUN=0
HAS_WEB_UNIT=0
if systemctl cat malina-web >/dev/null 2>&1; then HAS_WEB_UNIT=1; fi
if [ "${PURGE:-0}" = 1 ]; then
	if [ -e "$PURGE_MARKER" ]; then
		echo "purge: уже выполнялся (маркер $PURGE_MARKER); удаление пропускаю"
		echo "purge: для повторного удаления истории удалите маркер вручную"
	else
		PURGE_RUN=1
	fi
	# Если прошлый запуск оставил runtime-mask, снимаем его для retry.
	if [ "$PURGE_RUN" = 0 ]; then
		if ! sudo systemctl unmask --runtime malina-bms-reader; then
			echo "purge: не удалось снять runtime-mask (возможно persistent mask) malina-bms-reader" >&2
			sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
			if [ "$HAS_WEB_UNIT" = 1 ]; then
				sudo systemctl start malina-web 2>/dev/null || true
			fi
			exit 1
		fi
	fi
fi
if [ "$PURGE_RUN" = 1 ]; then
	echo "purge: удаляю всю историю bms (avg+raw)"
	for u in malina-tsdb malina-tsdb-raw malina-bms-reader; do
		if ! systemctl cat "$u" >/dev/null 2>&1; then
			echo "purge: unit $u не установлен" >&2
			exit 1
		fi
	done
	# Проверяем записываемость маркера ДО удаления истории.
	if ! sudo mkdir -p "$(dirname "$PURGE_MARKER")" \
		|| ! sudo touch "$(dirname "$PURGE_MARKER")/.purge-write-test" \
		|| ! sudo sync \
		|| ! sudo rm -f "$(dirname "$PURGE_MARKER")/.purge-write-test"; then
		echo "purge: каталог маркера недоступен — удаление отменено" >&2
		exit 1
	fi
	if [ ! -x "$RWCTL" ]; then
		echo "purge: нет $RWCTL — удаление отменено" >&2
		exit 1
	fi
	# Reader выключаем из автозапуска ДО удаления: при любом сбое он не должен
	# писать новые bms-ряды без маркера (в т.ч. после ребута).
	if ! sudo "$RWCTL" run systemctl disable malina-bms-reader; then
		echo "purge: не удалось disable malina-bms-reader — удаление отменено" >&2
		exit 1
	fi
	# Runtime-mask отменяет Restart=on-failure: SIGKILL не должен вернуть reader
	# во время удаления/до маркера. Маска снимается после успешного purge.
	if ! sudo systemctl mask --runtime malina-bms-reader 2>/dev/null; then
		case "$(systemctl is-enabled malina-bms-reader 2>/dev/null || true)" in
			masked|masked-runtime) ;;
			*) echo "purge: не удалось mask malina-bms-reader — удаление отменено" >&2; exit 1 ;;
		esac
	fi
	sudo systemctl stop malina-bms-reader malina-web malina-tsdb malina-tsdb-raw 2>/dev/null || true
	for u in malina-tsdb malina-tsdb-raw malina-bms-reader; do
		if systemctl is-active --quiet "$u"; then
			if [ "$u" = malina-bms-reader ]; then
				echo "purge: bms-reader не остановился — SIGKILL" >&2
				sudo systemctl kill --signal=SIGKILL "$u" 2>/dev/null || true
				sleep 1
			fi
			if systemctl is-active --quiet "$u"; then
				echo "purge: $u не остановился — удаление отменено" >&2
				sudo systemctl unmask --runtime malina-bms-reader 2>/dev/null || true
				sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
				[ "$HAS_WEB_UNIT" = 1 ] && sudo systemctl start malina-web 2>/dev/null || true
				# bms-reader оставляем остановленным/disabled до успешного purge.
				exit 1
			fi
		fi
	done
	if [ "$HAS_WEB_UNIT" = 1 ] && systemctl is-active --quiet malina-web; then
		echo "purge: malina-web не остановился — удаление отменено" >&2
		sudo systemctl unmask --runtime malina-bms-reader 2>/dev/null || true
		sudo systemctl start malina-tsdb malina-tsdb-raw malina-web 2>/dev/null || true
		exit 1
	fi
	if ! sudo rm -rf /malina/var/malina-ts/bms /run/malina-ts-raw/bms; then
		echo "purge: ошибка удаления — поднимаю tsdb/web, reader оставляю остановленным" >&2
		sudo systemctl unmask --runtime malina-bms-reader 2>/dev/null || true
		sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
		[ "$HAS_WEB_UNIT" = 1 ] && sudo systemctl start malina-web 2>/dev/null || true
		exit 1
	fi
	# Маркер — СРАЗУ после успешного удаления, до запуска bms-reader:
	# иначе после сбоя повторный purge удалит уже новые ряды.
	if ! sudo mkdir -p "$(dirname "$PURGE_MARKER")" || ! sudo touch "$PURGE_MARKER" || ! sudo sync; then
		echo "purge: не удалось надёжно создать маркер $PURGE_MARKER" >&2
		# bms-reader уже disabled до удаления; новых bms-рядов быть не должно,
		# пока маркер не создан.
		sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
		if [ "$HAS_WEB_UNIT" = 1 ]; then
			sudo systemctl start malina-web 2>/dev/null || true
		fi
		exit 1
	fi
	# Маркер создан — снимаем runtime-mask, иначе общий RESTART не поднимет reader.
	if ! sudo systemctl unmask --runtime malina-bms-reader; then
		echo "purge: не удалось снять runtime-mask (возможно persistent mask) malina-bms-reader" >&2
		sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
		if [ "$HAS_WEB_UNIT" = 1 ]; then
			sudo systemctl start malina-web 2>/dev/null || true
		fi
		exit 1
	fi
	# Мусорный файл больше не нужен; его ошибка не должна оставлять сервисы лежать.
	sudo rm -f /malina/etc/malina/bms-ident.json 2>/dev/null || true
	# tsdb должен подняться до bms-reader; web — до чтения истории.
	sudo systemctl start malina-tsdb malina-tsdb-raw 2>/dev/null || true
	if ! systemctl is-active --quiet malina-tsdb || ! systemctl is-active --quiet malina-tsdb-raw; then
		echo "purge: tsdb/raw не поднялись; поднимаю web/bms-reader best-effort" >&2
		if [ "$HAS_WEB_UNIT" = 1 ]; then
			sudo systemctl start malina-web 2>/dev/null || true
		fi
		sudo systemctl start malina-bms-reader 2>/dev/null || true
		exit 1
	fi
	if [ "$HAS_WEB_UNIT" = 1 ]; then
		sudo systemctl start malina-web 2>/dev/null || true
	fi
fi

# Если purge ранее оставил runtime-mask, обычный deploy не должен «успешно»
# пропустить запуск reader: требуем повторить purge.
if [ "${PURGE:-0}" != 1 ] && has bmsr; then
	case "$(systemctl is-enabled malina-bms-reader 2>/dev/null || true)" in
		masked|masked-runtime)
			echo "deploy: malina-bms-reader masked после purge; повторите --purge-bms-history" >&2
			exit 1
			;;
		disabled)
			echo "deploy: malina-bms-reader disabled; если это после purge — повторите --purge-bms-history, иначе включите unit" >&2
			exit 1
			;;
	esac
fi

# перезапуск выбранных сервисов (malina-netd не трогаем: сетевой стек)
RESTART=""
add_restart() { case " $RESTART " in *" $1 "*) ;; *) RESTART="${RESTART:+$RESTART }$1" ;; esac; }
# purge: tsdb/raw первыми, затем web, bms-reader — позже в общем порядке.
if [ "$PURGE" = 1 ]; then
	add_restart malina-tsdb
	add_restart malina-tsdb-raw
	if [ "$HAS_WEB_UNIT" = 1 ]; then add_restart malina-web; fi
fi
has web  && add_restart malina-web
has map  && add_restart map-reader
has kes  && add_restart kes-reader
has bmsr && add_restart malina-bms-reader
has bmsp && add_restart malina-bms-proxy
has tsdb && { add_restart malina-tsdb; add_restart malina-tsdb-raw; }
has grid && add_restart malina-grid-reader
for u in $RESTART; do
	[ -n "$u" ] || continue
	sudo systemctl restart "$u" 2>/dev/null && echo "restart $u" || echo "skip   $u (не активен/не установлен)"
done
sleep 2
echo "--- status ---"
for u in $RESTART; do
	[ -n "$u" ] || continue
	printf '%-18s %s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null || true)"
done
if [ "$PURGE" = 1 ]; then
	for u in $RESTART; do
		[ -n "$u" ] || continue
		if ! systemctl is-active --quiet "$u"; then
			echo "purge: $u не активен после рестарта" >&2
			exit 1
		fi
	done
	# После успешного purge/повтора reader должен остаться в автозапуске.
	if ! sudo "$RWCTL" run systemctl enable malina-bms-reader; then
		echo "purge: enable не удался; reader активен сейчас, но не в автозапуске; повторите --purge-bms-history" >&2
		exit 1
	fi
fi
# ID = BASE: реестр bms-ident.json больше не используется — убираем мусор после
# рестарта, чтобы старый reader не успел создать его снова. Только если
# обновляем bms-reader или это first-run.
if [ "${FIRST:-0}" = 1 ] || has bmsr; then
	sudo rm -f /malina/etc/malina/bms-ident.json 2>/dev/null || true
fi
has web && curl -s -o /dev/null -w "login http=%{http_code}\n" http://127.0.0.1/login || true

if [ "${FIRST:-0}" = 1 ]; then
	sudo rm -rf /tmp/malina-install
	echo "deploy: первичное развёртывание завершено"
else
	sudo rm -rf /malina/.deploy/tmp
fi
REMOTE
echo "==> готово ($([ "$FIRST" = 1 ] && echo 'первичное развёртывание' || echo "обновление: $ONLY")) на $HOST"
