#!/usr/bin/env bash
# Стоп пассивного USB-перехвата и разбор в артефакты этапа.
#
# Использование:
#   ./capture-stop.sh
#
# Что делает:
#   1. шлёт SIGINT tshark (захват корректно дописывает pcapng);
#   2. забирает файл у root, ставит права;
#   3. дописывает meta.json временем окончания и размерами.
#
# Разбор НЕ выполняется: по правилу оператора во время этапов прошивки логи
# только складываются. Разбор — позже: ./parse-stage.sh <каталог_этапа>.
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
FW_DIR="$(dirname "$HERE")"
RUN="$FW_DIR/.run"
PIDFILE="$RUN/capture.pid"
ACTIVEFILE="$RUN/capture.dir"
TMPFILE="$RUN/capture.tmp"

die() { printf 'ОШИБКА: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

PARSE=0
for a in "$@"; do
    case "$a" in
        --parse) PARSE=1 ;;
        *) die "неизвестный аргумент: $a (есть только --parse)" ;;
    esac
done

[ -f "$PIDFILE" ] || die "нет $PIDFILE — захват не запущен"
PID="$(cat "$PIDFILE")"
DIR="$(cat "$ACTIVEFILE" 2>/dev/null || true)"
[ -n "$DIR" ] && [ -d "$DIR" ] || die "не найден каталог этапа ($DIR)"

SUDO_OK=0
if sudo -n true 2>/dev/null; then
    SUDO_OK=1
else
    PASS="$(secret-tool lookup service sudo username "$(id -un)" 2>/dev/null || true)"
    if [ -n "$PASS" ] && printf '%s\n' "$PASS" | sudo -S -p '' -v 2>/dev/null; then
        SUDO_OK=1
    fi
fi
[ "$SUDO_OK" = 1 ] || die "нет sudo"

# --- остановка ----------------------------------------------------------------
sudo -n kill -INT "$PID" 2>/dev/null || true
for _ in $(seq 1 50); do
    sudo -n kill -0 "$PID" 2>/dev/null || break
    sleep 0.1
done
sudo -n kill -0 "$PID" 2>/dev/null && sudo -n kill -KILL "$PID" 2>/dev/null || true

rm -f "$PIDFILE" "$ACTIVEFILE"

PCAP_TMP="$(cat "$TMPFILE" 2>/dev/null || true)"
[ -n "$PCAP_TMP" ] && [ -f "$PCAP_TMP" ] || die "нет временного дампа ($PCAP_TMP)"
PCAP="$DIR/usb.pcapng"
sudo -n mv -f "$PCAP_TMP" "$PCAP"
sudo -n chown "$(id -u):$(id -g)" "$PCAP"
chmod 0644 "$PCAP"
rm -f "$TMPFILE"

# --- метаданные ---------------------------------------------------------------
python3 - "$DIR/meta.json" "$PCAP" <<'PY'
import json, sys, os, datetime
meta_path, pcap = sys.argv[1:3]
m = json.load(open(meta_path))
now = datetime.datetime.now().astimezone()
msk = datetime.timezone(datetime.timedelta(hours=3))
m["stopped_local"] = now.isoformat(timespec="seconds")
m["stopped_msk"] = now.astimezone(msk).isoformat(timespec="seconds")
m["pcap"] = {"file": os.path.basename(pcap), "size": os.path.getsize(pcap)}
json.dump(m, open(meta_path, "w"), ensure_ascii=False, indent=2)
PY

# --- разбор (только по явному --parse) ----------------------------------------
if [ "$PARSE" = 1 ]; then
    python3 "$HERE/decode-ftdi.py" "$DIR"
else
    say "разбор не выполнялся (правило: во время прошивки только складываем логи)"
    say "разобрать позже: $HERE/parse-stage.sh '$DIR'"
fi

say "этап сохранён: $DIR"
ls -la "$DIR"
