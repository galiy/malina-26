#!/usr/bin/env bash
# agm-pzem-usbip — находит PZEM-017 на свободном USB-serial порту и публикует
# его через usbip (usbip bind).
#
# Алгоритм обнаружения повторяет agm-reader:
#   - только USB-serial (/dev/ttyUSB*);
#   - порт берётся под flock; занятые другими сервисами порты пропускаются;
#   - проба PZEM: Modbus RTU 9600 8N1, slave 1, функция 0x04, 8 регистров;
#   - не ответил как PZEM — порт не трогаем 30 с.
#
# После bind устройство уходит в драйвер usbip-host (ttyUSB исчезает).
# Раз в CHECK_INTERVAL секунд проверяется busid: если устройство выдернули,
# состояние сбрасывается, и после повторного включения (в тот же или другой
# порт) сервис снова находит и привязывает PZEM.
#
# Зависимости: bash, coreutils, util-linux (flock/logger), stty, usbip/usbipd,
# модуль ядра usbip-host.
set -u

TAG=agm-pzem-usbip
LOCK_FILE=${LOCK_FILE:-/run/agm-pzem-usbip.lock}
STATE_DIR=${STATE_DIR:-/malina/var/lib/agm-pzem-usbip}
BUSID_FILE=$STATE_DIR/busid
SLAVE=${PZEM_SLAVE:-1}
INTERVAL=${CHECK_INTERVAL:-30}
PROBE_TIMEOUT=${PZEM_PROBE_TIMEOUT:-0.3}
FOREIGN_BACKOFF=${FOREIGN_BACKOFF:-30}
NOTICE_INTERVAL=${NOTICE_INTERVAL:-300}
BIND_BACKOFF=${BIND_BACKOFF:-10}
VID=${PZEM_VID:-1a86}
PID=${PZEM_PID:-7523}

USBIP=${USBIP:-$(command -v usbip || true)}
if [ -z "$USBIP" ] && [ -x /usr/sbin/usbip ]; then USBIP=/usr/sbin/usbip; fi
USBIPD=${USBIPD:-$(command -v usbipd || true)}
if [ -z "$USBIPD" ] && [ -x /usr/sbin/usbipd ]; then USBIPD=/usr/sbin/usbipd; fi
LAST_USBIPD_ERR=0
LAST_BIND_WARN=0

TMP_DIR=${TMP_DIR:-/run/agm-pzem-usbip}

init_statedir() {
	if ! mkdir -p "$STATE_DIR" 2>/dev/null || [ ! -w "$STATE_DIR" ]; then
		STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/agm-pzem-usbip.XXXXXX") || {
			echo "$TAG: нет доступного каталога состояния" >&2
			exit 1
		}
		BUSID_FILE=$STATE_DIR/busid
	fi
}

init_tmpdir() {
	if ! mkdir -p "$TMP_DIR" 2>/dev/null || [ ! -w "$TMP_DIR" ]; then
		TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/agm-pzem-usbip-tmp.XXXXXX") || exit 1
	fi
}

init_dirs() {
	init_statedir
	init_tmpdir
}

log() {
	local prio=$1
	shift
	if command -v logger >/dev/null 2>&1; then
		logger -t "$TAG" -p "user.$prio" "$*"
	else
		printf '%s: %s\n' "$TAG" "$*" >&2
	fi
}
info() { log info "$*"; }
warn() { log warning "$*"; }
err() { log err "$*"; }

write_busid() {
	local b=$1 tmp
	tmp=$(mktemp "$STATE_DIR/busid.XXXXXX") || return 1
	if printf '%s\n' "$b" >"$tmp"; then
		mv -f "$tmp" "$BUSID_FILE"
	else
		rm -f "$tmp"
		return 1
	fi
}

err_limited() {
	local now
	now=$(date +%s)
	if ((now - LAST_USBIPD_ERR >= 300)); then
		LAST_USBIPD_ERR=$now
		err "$*"
	fi
}

warn_limited() {
	local now
	now=$(date +%s)
	if ((now - LAST_BIND_WARN >= 60)); then
		LAST_BIND_WARN=$now
		warn "$*"
	fi
}

require_int() {
	local name=$1 val=$2 min=$3 max=$4
	case "$val" in
		'' | *[!0-9]*)
			echo "$TAG: $name=$val — нужно целое" >&2
			exit 2
			;;
	esac
	val=$((10#$val))
	if [ "$val" -lt "$min" ] || [ "$val" -gt "$max" ]; then
		echo "$TAG: $name=$val вне $min..$max" >&2
		exit 2
	fi
	echo "$val"
}

validate_settings() {
	SLAVE=$(require_int PZEM_SLAVE "$SLAVE" 1 247) || exit 2
	INTERVAL=$(require_int CHECK_INTERVAL "$INTERVAL" 1 3600) || exit 2
	FOREIGN_BACKOFF=$(require_int FOREIGN_BACKOFF "$FOREIGN_BACKOFF" 5 86400) || exit 2
	BIND_BACKOFF=$(require_int BIND_BACKOFF "$BIND_BACKOFF" 1 300) || exit 2
	NOTICE_INTERVAL=$(require_int NOTICE_INTERVAL "$NOTICE_INTERVAL" 10 86400) || exit 2
	case "$PROBE_TIMEOUT" in
		'' | *[!0-9.]* | *.*.*)
			echo "$TAG: PZEM_PROBE_TIMEOUT=$PROBE_TIMEOUT — нужно число секунд" >&2
			exit 2
			;;
	esac
	if ! awk -v v="$PROBE_TIMEOUT" 'BEGIN { exit !(v + 0 == v && v >= 0.05 && v <= 2) }'; then
		echo "$TAG: PZEM_PROBE_TIMEOUT=$PROBE_TIMEOUT вне 0.05..2 с" >&2
		exit 2
	fi
}

usage() {
	cat <<'EOF'
agm-pzem-usbip [--once|--status|--probe DEV|--selftest|--help]

  (без аргументов)  цикл: найти PZEM, usbip bind, следить за busid
  --once            один проход поиска/привязки и выход
  --status          показать состояние привязки
  --probe DEV [--force]  проверить порт (занятый без --force не трогаем)
  --selftest        проверить CRC/разбор кадра на эталоне
EOF
}

# CRC16 Modbus (poly 0xA001, init 0xFFFF).
crc16() {
	local crc=65535 b i
	for b in "$@"; do
		crc=$((crc ^ b))
		for ((i = 0; i < 8; i++)); do
			if ((crc & 1)); then
				crc=$(((crc >> 1) ^ 0xA001))
			else
				crc=$((crc >> 1))
			fi
		done
	done
	echo "$crc"
}

# fix_crc ИМЯ_МАССИВА — пересчитать CRC в 21-байтном кадре (для selftest).
fix_crc() {
	local -n a=$1
	local c
	c=$(crc16 "${a[@]:0:19}")
	a[19]=$((c & 255))
	a[20]=$((c >> 8))
}

# pzem_frame_valid 21_байт... — проверка ответа PZEM.
pzem_frame_valid() {
	local -a b=("$@")
	[ "${#b[@]}" -eq 21 ] || return 1
	[ "${b[0]}" -eq "$SLAVE" ] || return 1
	[ "${b[1]}" -eq 4 ] || return 1
	[ "${b[2]}" -eq 16 ] || return 1

	local crc=$((b[19] | (b[20] << 8)))
	local calc
	calc=$(crc16 "${b[@]:0:19}")
	[ "$calc" -eq "$crc" ] || return 1

	local u=$((b[3] * 256 + b[4]))
	local i=$((b[5] * 256 + b[6]))
	local p=$((b[7] * 256 + b[8] + (b[9] * 256 + b[10]) * 65536))
	local ah=$((b[15] * 256 + b[16]))
	local al=$((b[17] * 256 + b[18]))

	# 0.01 В, 0.01 А, 0.1 Вт; флаги аварий только 0/0xFFFF.
	((u >= 5 && u <= 30000)) || return 1
	((i <= 30000)) || return 1
	((p <= 1200000)) || return 1
	((ah == 0 || ah == 65535)) || return 1
	((al == 0 || al == 65535)) || return 1
	return 0
}

# port_busy REAL — порт держится другим процессом на запись (O_WRONLY/O_RDWR).
# Read-only держатели игнорируются, как в BusyRdevs agm-reader; тогда возможен
# конфликт с пассивным наблюдателем (описано в README).
port_busy() {
	local target=$1 pids pid fd link tid info flags mode
	tid=$(stat -Lc '%d:%i' "$target" 2>/dev/null) || return 1
	if command -v fuser >/dev/null 2>&1; then
		pids=$(fuser "$target" 2>/dev/null | tr -s ' ' '\n' | grep -E '^[0-9]+$' || true)
	else
		pids=$(printf '%s\n' /proc/[0-9]* | sed 's#.*/##')
	fi
	for pid in $pids; do
		[ -d "/proc/$pid/fd" ] || continue
		for fd in /proc/$pid/fd/*; do
			link=$(readlink "$fd" 2>/dev/null) || continue
			if [ "$link" != "$target" ]; then
				[ "$(stat -Lc '%d:%i' "$fd" 2>/dev/null)" = "$tid" ] || continue
			fi
			info=$(cat "/proc/$pid/fdinfo/$(basename "$fd")" 2>/dev/null) || {
				return 0
			}
			flags=$(printf '%s\n' "$info" | awk '/^flags:/{print $2; exit}')
			[ -n "$flags" ] || return 0
			case "$flags" in *[!0-7]*) return 0 ;; esac
			mode=$((0$flags & 3))
			[ "$mode" -ne 0 ] && return 0
		done
	done
	return 1
}

# tty_usb_ids DEV — "VID:PID" USB-устройства для tty-узла.
tty_usb_ids() {
	local b
	b=$(tty_busid "$1") || return 1
	[ -d "/sys/bus/usb/devices/$b" ] || return 1
	printf '%s:%s\n' "$(cat "/sys/bus/usb/devices/$b/idVendor" 2>/dev/null)" \
		"$(cat "/sys/bus/usb/devices/$b/idProduct" 2>/dev/null)"
}

# find_pzem_frame байты... — индекс 21-байтного кадра PZEM в буфере (учёт эха).
find_pzem_frame() {
	local -a b=("$@")
	local i
	for ((i = 0; i + 21 <= ${#b[@]}; i++)); do
		if pzem_frame_valid "${b[@]:i:21}"; then
			echo "$i"
			return 0
		fi
	done
	return 1
}

# probe_port DEV — true, если на порту отвечает PZEM. Работает в подоболочке,
# чтобы не стать лидером сессии (иначе tty может стать управляющим терминалом).
# Держит flock, пока открыт порт, и закрывает его перед возвратом.
probe_port() (
	local dev=$1 fd tmp bytes n
	local -a req

	[ -c "$dev" ] || return 2
	{ exec {fd}<>"$dev"; } 2>/dev/null || return 2
	if ! flock -n "$fd"; then
		exec {fd}>&-
		return 2
	fi
	if ! stty -F "$dev" 9600 cs8 -cstopb -parenb -echo -ixon -ixoff -crtscts raw clocal 2>/dev/null; then
		exec {fd}>&-
		return 2
	fi

	# Дренаж возможного мусора.
	timeout 0.05 dd bs=1 count=64 <&$fd of=/dev/null 2>/dev/null || true

	req=("$SLAVE" 4 0 0 0 8)
	local crc
	crc=$(crc16 "${req[@]}")
	req+=($((crc & 255)) $((crc >> 8)))
	local esc
	printf -v esc '\\x%02x' "${req[@]}"
	printf '%b' "$esc" >&$fd || {
		exec {fd}>&-
		return 2
	}

	tmp=$(mktemp "$TMP_DIR/probe.XXXXXX") || return 2
	trap 'rm -f "$tmp"' EXIT
	: >"$tmp"
	# Читаем с запасом: некоторые USB-RS485 возвращают эхо запроса перед
	# ответом. Кадр ищем по сигнатуре, а не только с начала буфера.
	timeout "$PROBE_TIMEOUT" dd bs=1 count=128 <&$fd of="$tmp" 2>/dev/null || true
	exec {fd}>&-

	n=$(stat -c %s "$tmp" 2>/dev/null || echo 0)
	if [ "$n" -lt 21 ]; then
		rm -f "$tmp"
		return 1
	fi
	bytes=($(od -An -v -tu1 "$tmp" 2>/dev/null))
	rm -f "$tmp"
	find_pzem_frame "${bytes[@]}" >/dev/null
)

# tty_busid DEV — USB busid (например 1-1.3.2) для tty-узла.
tty_busid() {
	local dev=$1 name p b
	dev=$(readlink -f "$dev" 2>/dev/null || echo "$dev")
	name=$(basename "$dev")
	p=$(readlink -f "/sys/class/tty/$name/device" 2>/dev/null) || return 1
	b=$(basename "$p")
	case "$b" in
		ttyUSB* | ttyACM*) b=$(basename "$(dirname "$p")") ;;
	esac
	b=${b%%:*}
	[ -n "$b" ] || return 1
	echo "$b"
}

ensure_usbipd() {
	local now
	pgrep -x usbipd >/dev/null 2>&1 && return 0
	now=$(date +%s)
	modprobe usbip-host 2>/dev/null || true
	if systemctl cat usbipd.service >/dev/null 2>&1; then
		# Один владелец: если пакетный unit есть, запускаем его, а не свой.
		systemctl start usbipd.service >/dev/null 2>&1 || true
	fi
	if ! pgrep -x usbipd >/dev/null 2>&1 && [ -n "$USBIPD" ]; then
		# Unit отсутствует/не поднялся — запасной путь.
		# Не отдаём демону fd блокировки (иначе flock останется у usbipd).
		"$USBIPD" -D 8>&- >/dev/null 2>&1 || true
	fi
	if ! pgrep -x usbipd >/dev/null 2>&1; then
		if ((now - LAST_USBIPD_ERR >= 300)); then
			LAST_USBIPD_ERR=$now
			err "не удалось запустить usbipd"
		fi
		return 1
	fi
	sleep 1
	pgrep -x usbipd >/dev/null 2>&1
}

# bind_port DEV — привязать найденный PZEM к usbip-host.
bind_port() {
	local dev=$1 busid vid pid drv
	busid=$(tty_busid "$dev") || {
		warn "$dev: не удалось определить USB busid"
		return 1
	}
	[ -d "/sys/bus/usb/devices/$busid" ] || {
		warn "$dev: busid $busid не найден в sysfs"
		return 1
	}
	vid=$(cat "/sys/bus/usb/devices/$busid/idVendor" 2>/dev/null || true)
	pid=$(cat "/sys/bus/usb/devices/$busid/idProduct" 2>/dev/null || true)
	if [ "$vid" != "$VID" ] || [ "$pid" != "$PID" ]; then
		warn_limited "busid $busid: неожиданный VID:PID $vid:$pid (ожидался $VID:$PID)"
		return 1
	fi

	ensure_usbipd || return 1
	modprobe usbip-host 2>/dev/null || true

	if [ -z "$USBIP" ]; then
		err_limited "нет usbip (пакет usbip/linux-tools-common не установлен)"
		return 1
	fi
	if port_busy "$(readlink -f "$dev" 2>/dev/null || echo "$dev")"; then
		warn_limited "$dev: порт заняли между пробой и bind — повторю позже"
		return 1
	fi
	if "$USBIP" bind -b "$busid" >"$STATE_DIR/usbip.out" 2>&1; then
		:
	else
		drv=$(readlink -f "/sys/bus/usb/devices/$busid/driver" 2>/dev/null || true)
		case "$drv" in
			*usbip-host*) ;; # уже привязан
			*)
				warn_limited "usbip bind $busid не удался: $(tr '\n' ' ' <"$STATE_DIR/usbip.out" 2>/dev/null | tail -c 300)"
				return 1
				;;
		esac
	fi

	drv=$(readlink -f "/sys/bus/usb/devices/$busid/driver" 2>/dev/null || true)
	case "$drv" in
		*usbip-host*) ;;
		*)
			warn_limited "usbip bind $busid вернул успех, но драйвер: ${drv:-none}"
			return 1
			;;
	esac
	write_busid "$busid" || {
		warn_limited "не удалось сохранить busid=$busid"
		return 1
	}
	info "PZEM-017 найден на $dev, опубликован через usbip (busid=$busid)"
	return 0
}

# check_bound — проверить, что сохранённый busid ещё существует и привязан.
# Возвращает 0, если привязка жива; 1 — state сброшен/нужен поиск.
check_bound() {
	local b drv vid pid
	[ -s "$BUSID_FILE" ] || return 1
	b=$(cat "$BUSID_FILE")
	if [ ! -d "/sys/bus/usb/devices/$b" ]; then
		info "PZEM busid=$b пропал (выдернут) — жду повторного подключения"
		rm -f "$BUSID_FILE"
		return 1
	fi
	vid=$(cat "/sys/bus/usb/devices/$b/idVendor" 2>/dev/null || true)
	pid=$(cat "/sys/bus/usb/devices/$b/idProduct" 2>/dev/null || true)
	if [ "$vid" != "$VID" ] || [ "$pid" != "$PID" ]; then
		warn "busid=$b теперь $vid:$pid, а не PZEM — сбрасываю состояние без unbind"
		rm -f "$BUSID_FILE"
		return 1
	fi
	drv=$(readlink -f "/sys/bus/usb/devices/$b/driver" 2>/dev/null || true)
	case "$drv" in
		*usbip-host*) return 0 ;;
		*)
			warn "busid=$b больше не в usbip-host ($drv) — пересканирую"
			rm -f "$BUSID_FILE"
			return 1
			;;
	esac
}

# find_bound_ch340 — busid уже привязанного CH340 (восстановление после
# аварии/ручного bind, когда state потерян). Возвращает первый найденный.
find_bound_ch340() {
	local d b vid pid drv count=0 first=""
	for d in /sys/bus/usb/devices/*; do
		[ -d "$d" ] || continue
		vid=$(cat "$d/idVendor" 2>/dev/null || true)
		pid=$(cat "$d/idProduct" 2>/dev/null || true)
		[ "$vid" = "$VID" ] && [ "$pid" = "$PID" ] || continue
		drv=$(readlink -f "$d/driver" 2>/dev/null || true)
		case "$drv" in
			*usbip-host*)
				count=$((count + 1))
				first=$(basename "$d")
				;;
		esac
	done
	if [ "$count" -eq 1 ]; then
		echo "$first"
		return 0
	fi
	if [ "$count" -gt 1 ]; then
		local marker=$TMP_DIR/multi_warn now last=0
		now=$(date +%s)
		[ -f "$marker" ] && last=$(cat "$marker" 2>/dev/null || echo 0)
		if ((now - last >= 300)); then
			echo "$now" >"$marker"
			warn "в usbip-host несколько CH340 ($count) — автовосстановление пропущено"
		fi
	fi
	return 1
}

declare -A BACKOFF=()
LAST_NOTICE=0

scan_once() {
	local now dev real b
	local -A seen=()
	now=$(date +%s)

	ensure_usbipd || return 1
	if check_bound; then
		return 0
	fi
	if [ ! -s "$BUSID_FILE" ]; then
		if b=$(find_bound_ch340); then
			if write_busid "$b"; then
				info "восстановлено состояние: CH340 уже в usbip-host (busid=$b)"
				return 0
			fi
			warn_limited "не удалось сохранить восстановленный busid=$b"
			return 1
		fi
	fi

	for dev in /dev/ttyUSB* /dev/serial/by-id/*; do
		[ -c "$dev" ] || continue
		real=$(readlink -f "$dev" 2>/dev/null || echo "$dev")
		[ -z "${seen[$real]:-}" ] || continue
		seen[$real]=1
		# PZEM-017 — CH340; не шлём Modbus-пробы в чужие USB-serial.
		[ "$(tty_usb_ids "$dev" 2>/dev/null)" = "$VID:$PID" ] || continue
		if port_busy "$real"; then
			continue
		fi
		if [ -n "${BACKOFF[$real]:-}" ] && [ "$now" -lt "${BACKOFF[$real]}" ]; then
			continue
		fi
		if probe_port "$dev"; then
			if bind_port "$dev"; then
				unset "BACKOFF[$real]"
				return 0
			fi
			BACKOFF[$real]=$((now + BIND_BACKOFF))
		else
			# Не наше/нет ответа: 30 с не трогаем, чтобы другие сервисы
			# успели найти на этом порту своё устройство.
			BACKOFF[$real]=$((now + FOREIGN_BACKOFF))
		fi
	done

	if ((now - LAST_NOTICE >= NOTICE_INTERVAL)); then
		LAST_NOTICE=$now
		info "PZEM-017 не найден среди свободных USB-serial портов"
	fi
	return 1
}

status() {
	local b=-
	[ -s "$BUSID_FILE" ] && b=$(cat "$BUSID_FILE")
	echo "state_dir=$STATE_DIR"
	echo "busid=$b"
	if [ "$b" != "-" ] && [ -d "/sys/bus/usb/devices/$b" ]; then
		echo "driver=$(readlink -f "/sys/bus/usb/devices/$b/driver" 2>/dev/null || echo none)"
		echo "vendor=$(cat "/sys/bus/usb/devices/$b/idVendor" 2>/dev/null || echo '?')"
		echo "product=$(cat "/sys/bus/usb/devices/$b/idProduct" 2>/dev/null || echo '?')"
	fi
	pgrep -x usbipd >/dev/null 2>&1 && echo "usbipd=running" || echo "usbipd=stopped"
}

selftest() {
	local SLAVE=1
	local -a req=(1 4 0 0 0 8)
	local -a resp=(1 4 16 20 227 0 7 0 37 0 0 0 1 0 0 0 0 0 0 88 225)
	local crc
	crc=$(crc16 "${req[@]}")
	if [ "$crc" -ne $((0xCCF1)) ]; then
		echo "FAIL: CRC запроса $crc" >&2
		return 1
	fi
	if ! pzem_frame_valid "${resp[@]}"; then
		echo "FAIL: эталонный ответ PZEM не принят" >&2
		return 1
	fi
	local rcrc esc
	rcrc=$(crc16 "${req[@]}")
	req+=($((rcrc & 255)) $((rcrc >> 8)))
	local -a echoed=("${req[@]}" "${resp[@]}")
	if [ "$(find_pzem_frame "${echoed[@]}")" != 8 ]; then
		echo "FAIL: кадр с эхом не найден" >&2
		return 1
	fi
	local -a bad=("${resp[@]}")
	bad[0]=2
	fix_crc bad
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: чужой slave принят" >&2
		return 1
	fi
	bad=("${resp[@]}")
	bad[2]=15
	fix_crc bad
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: неверный bytecount принят" >&2
		return 1
	fi
	bad=("${resp[@]}")
	bad[1]=3
	fix_crc bad
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: неверная функция принята" >&2
		return 1
	fi
	bad=("${resp[@]:0:20}")
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: короткий кадр принят" >&2
		return 1
	fi
	bad=("${resp[@]}")
	bad[15]=1
	fix_crc bad
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: мусорный регистр аварии принят" >&2
		return 1
	fi
	bad=("${resp[@]}")
	bad[3]=0
	bad[4]=0
	fix_crc bad
	if pzem_frame_valid "${bad[@]}"; then
		echo "FAIL: нулевое напряжение принято" >&2
		return 1
	fi
	resp[19]=0
	if pzem_frame_valid "${resp[@]}"; then
		echo "FAIL: битый CRC принят" >&2
		return 1
	fi
	echo "selftest: ok"
	return 0
}

main() {
	case "${1:-}" in
		--help | -h)
			usage
			exit 0
			;;
		--selftest)
			selftest
			exit $?
			;;
	esac
	validate_settings
	case "${1:-}" in
		--status)
			status
			exit 0
			;;
		--probe)
			[ -n "${2:-}" ] || {
				echo "укажи устройство" >&2
				exit 2
			}
			init_tmpdir
			real=$(readlink -f "$2" 2>/dev/null || echo "$2")
			if [ "${3:-}" != "--force" ] && port_busy "$real"; then
				echo "занят: $2 (--force чтобы проверить принудительно)" >&2
				exit 3
			fi
			probe_port "$2"
			rc=$?
			if [ "$rc" -eq 0 ]; then
				echo "PZEM: $2"
				exit 0
			fi
			if [ "$rc" -eq 2 ]; then
				echo "занят/не удалось открыть: $2" >&2
				exit 3
			fi
			echo "не PZEM: $2"
			exit 1
			;;
		--once)
			init_dirs
			exec 8>"$LOCK_FILE" || exit 1
			if ! flock -n 8; then
				err "уже запущен другой экземпляр ($LOCK_FILE)"
				exit 1
			fi
			ensure_usbipd || true
			scan_once
			exit $?
			;;
		"") ;;
		*)
			usage >&2
			exit 2
			;;
	esac

	exec 8>"$LOCK_FILE" || exit 1
	if ! flock -n 8; then
		err "уже запущен другой экземпляр ($LOCK_FILE)"
		exit 1
	fi
	init_dirs
	info "старт: slave=$SLAVE, интервал=${INTERVAL}s, state=$STATE_DIR"
	cleanup() {
		local b vid pid
		b=$(cat "$BUSID_FILE" 2>/dev/null || true)
		if [ -n "$b" ] && [ -n "$USBIP" ]; then
			vid=$(cat "/sys/bus/usb/devices/$b/idVendor" 2>/dev/null || true)
			pid=$(cat "/sys/bus/usb/devices/$b/idProduct" 2>/dev/null || true)
			if [ "$vid" = "$VID" ] && [ "$pid" = "$PID" ]; then
				if "$USBIP" unbind -b "$b" >/dev/null 2>&1; then
					rm -f "$BUSID_FILE"
				else
					warn "usbip unbind $b не удался — состояние оставлено для повтора"
				fi
			else
				rm -f "$BUSID_FILE"
			fi
		else
			rm -f "$BUSID_FILE"
		fi
		info "останов"
	}
	trap 'cleanup; exit 0' TERM INT
	while :; do
		ensure_usbipd || true
		scan_once || true
		sleep "$INTERVAL"
	done
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
