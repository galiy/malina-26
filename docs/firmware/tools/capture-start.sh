#!/usr/bin/env bash
# Старт пассивного USB-перехвата обмена с МАП (FTDI FT232R) через usbmon.
#
# Использование:
#   ./capture-start.sh <этап> ["примечание"]
# Пример:
#   ./capture-start.sh monitor "включение монитора без прошивки"
#
# Что делает:
#   1. находит FTDI 0403:6001 в sysfs (busnum/devnum/serial);
#   2. создаёт docs/firmware/<устройство>/logs/<дата_время_MSK>_<этап>/;
#   3. пишет meta.json (этап, время, устройство, целевая прошивка+SHA-256);
#   4. запускает `tshark -i usbmonN -w usb.pcapng` (пассивно, порт не открывает).
#
# Требования: загружен модуль usbmon (`sudo modprobe usbmon`), tshark/dumpcap,
# sudo (пароль — из связки ключей, см. AGENTS.md).
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
FW_DIR="$(dirname "$HERE")"
ROOT="$(cd -- "$FW_DIR/../.." && pwd)"
DEVICE="${FW_DEVICE:-titanator}"
LOGS="$FW_DIR/$DEVICE/logs"
RUN="$FW_DIR/.run"
PIDFILE="$RUN/capture.pid"
ACTIVEFILE="$RUN/capture.dir"
TMPFILE="$RUN/capture.tmp"

die() { printf 'ОШИБКА: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

STAGE="${1:-}"
NOTE="${2:-}"
[ -n "$STAGE" ] || die "укажи этап: $0 <этап> [примечание]"
case "$STAGE" in *[!A-Za-z0-9_.-]*) die "в имени этапа только A-Za-z0-9_.-";; esac

# --- не запускать второй захват -------------------------------------------------
[ -d "$FW_DIR/$DEVICE" ] || die "нет каталога устройства: docs/firmware/$DEVICE"
mkdir -p "$RUN" "$LOGS"
if [ -f "$PIDFILE" ] && sudo -n kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    die "захват уже идёт (pid $(cat "$PIDFILE"), $(cat "$ACTIVEFILE" 2>/dev/null)); сначала capture-stop.sh"
fi
rm -f "$PIDFILE" "$ACTIVEFILE" "$TMPFILE"

# --- usbmon -------------------------------------------------------------------
[ -e /dev/usbmon0 ] || die "нет /dev/usbmon0: загрузи модуль (sudo modprobe usbmon)"
command -v tshark >/dev/null || die "нет tshark"

# --- поиск FTDI ---------------------------------------------------------------
BUS="" DEVNUM="" SERIAL="" PRODUCT="" SYSDEV=""
for d in /sys/bus/usb/devices/*; do
    [ -f "$d/idVendor" ] || continue
    [ "$(cat "$d/idVendor" 2>/dev/null)" = "0403" ] || continue
    [ "$(cat "$d/idProduct" 2>/dev/null)" = "6001" ] || continue
    BUS="$(cat "$d/busnum" 2>/dev/null || true)"
    DEVNUM="$(cat "$d/devnum" 2>/dev/null || true)"
    SERIAL="$(cat "$d/serial" 2>/dev/null || true)"
    PRODUCT="$(cat "$d/product" 2>/dev/null || true)"
    SYSDEV="$(basename "$d")"
    break
done
[ -n "$BUS" ] || die "FTDI 0403:6001 не найден в sysfs"

# --- sudo ---------------------------------------------------------------------
SUDO_OK=0
if sudo -n true 2>/dev/null; then
    SUDO_OK=1
else
    PASS="$(secret-tool lookup service sudo username "$(id -un)" 2>/dev/null || true)"
    if [ -n "$PASS" ] && printf '%s\n' "$PASS" | sudo -S -p '' -v 2>/dev/null; then
        SUDO_OK=1
    fi
fi
[ "$SUDO_OK" = 1 ] || die "нет sudo (нужен для чтения /dev/usbmonN)"

# --- каталог этапа ------------------------------------------------------------
TS="$(date '+%Y%m%d_%H%M%S')"
DIR="$LOGS/${TS}_${STAGE}"
mkdir -p "$DIR"
PCAP="$DIR/usb.pcapng"
# tshark под root сбрасывает привилегии и не может писать в каталог пользователя,
# поэтому пишем в world-writable /tmp, а на остановке переносим в каталог этапа.
PCAP_TMP="/tmp/malina-usb-${TS}-${STAGE}.pcapng"

# целевая прошивка и её хеш (если файл известен)
FW_FILE="${MAP2GUI_FW:-$ROOT/map2gui/mXP1p1v4_5_Titanator_tst3.map2}"
FW_SHA=""
[ -f "$FW_FILE" ] && FW_SHA="$(sha256sum "$FW_FILE" | awk '{print $1}')"

python3 - "$DIR/meta.json" "$STAGE" "$NOTE" "$BUS" "$DEVNUM" "$SERIAL" "$PRODUCT" "$SYSDEV" "$FW_FILE" "$FW_SHA" "$DEVICE" <<'PY'
import json, sys, datetime, platform, subprocess
out, stage, note, bus, dev, serial, product, sysdev, fw, fwsha, device = sys.argv[1:12]
now = datetime.datetime.now().astimezone()
def sh(c):
    try: return subprocess.check_output(c, shell=True, text=True, stderr=subprocess.DEVNULL).strip()
    except Exception: return ""
meta = {
    "device": device,
    "stage": stage,
    "note": note,
    "started_local": now.isoformat(timespec="seconds"),
    "started_msk": now.astimezone(datetime.timezone(datetime.timedelta(hours=3))).isoformat(timespec="seconds"),
    "usb": {"bus": int(bus) if bus.isdigit() else bus, "devnum": int(dev) if dev.isdigit() else dev,
            "serial": serial, "product": product, "sysfs": sysdev,
            "idVendor": "0403", "idProduct": "6001"},
    "firmware": {"path": fw, "sha256": fwsha},
    "host": platform.node(),
    "kernel": platform.release(),
    "tshark": sh("tshark -v | head -1"),
}
json.dump(meta, open(out, "w"), ensure_ascii=False, indent=2)
PY

# --- запуск захвата -----------------------------------------------------------
# PID пишет сам root-shell, чтобы потом было чем слать SIGINT.
sudo -n sh -c "echo \$\$ > '$PIDFILE'; exec tshark -i 'usbmon$BUS' -w '$PCAP_TMP' -q -s 0" \
    >"$DIR/capture.stdout" 2>"$DIR/capture.stderr" &
sleep 1

if ! sudo -n kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null; then
    echo "--- capture.stderr ---"; cat "$DIR/capture.stderr" >&2
    die "tshark не запустился"
fi

printf '%s\n' "$DIR" > "$ACTIVEFILE"
printf '%s\n' "$PCAP_TMP" > "$TMPFILE"
say "захват пошёл: этап '$STAGE'"
say "  устройство: FTDI 0403:6001 serial=$SERIAL bus=$BUS dev=$DEVNUM"
say "  устройство: $DEVICE"
say "  каталог:    $DIR"
say "  остановка:  $HERE/capture-stop.sh"
