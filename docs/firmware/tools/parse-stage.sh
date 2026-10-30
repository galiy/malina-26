#!/usr/bin/env bash
# Разбор сохранённого этапа (позже, когда разрешено парсить логи).
#
# Использование:
#   ./parse-stage.sh                 # последний каталог в docs/firmware/<устройство>/logs
#                                    # (устройство: FW_DEVICE, по умолчанию titanator)
#   ./parse-stage.sh <каталог_этапа>
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEVICE="${FW_DEVICE:-titanator}"
LOGS="$(dirname "$HERE")/$DEVICE/logs"

DIR="${1:-}"
if [ -z "$DIR" ]; then
    DIR="$(ls -1d "$LOGS"/*/ 2>/dev/null | sort | tail -1)"
    DIR="${DIR%/}"
fi
[ -n "$DIR" ] && [ -d "$DIR" ] || { echo "ОШИБКА: нет каталога этапа ($DIR)" >&2; exit 1; }
[ -f "$DIR/usb.pcapng" ] || { echo "ОШИБКА: в $DIR нет usb.pcapng" >&2; exit 1; }

python3 "$HERE/decode-ftdi.py" "$DIR"
