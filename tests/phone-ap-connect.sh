#!/usr/bin/env bash
# Автотест: подключение Android-телефона к точке доступа ПАК «Малина» и
# проверка связности/веб-UI со стороны телефона.
#
# Управление — через ADB + uiautomator (без сторонних приложений на телефоне).
# Телефон должен быть подключён по USB с разрешённой отладкой.
#
# Использование:
#   tests/phone-ap-connect.sh [SSID] [PASSWORD] [GATEWAY]
# По умолчанию: SSID=Malina  PASSWORD=malina123  GATEWAY=10.10.100.254
#
# Переменные окружения:
#   ADB          — путь к adb (по умолчанию adb)
#   ADB_SERIAL   — серийный номер устройства, если их несколько
#   WAIT_CONNECT — сколько секунд ждать подключения (по умолчанию 60)
#
# Код возврата: 0 — успех, 1 — ошибка.
set -u

ADB="${ADB:-adb}"
SSID="${1:-Malina}"
PASSWORD="${2:-malina123}"
GATEWAY="${3:-10.10.100.254}"
WAIT_CONNECT="${WAIT_CONNECT:-60}"

WORK="$(mktemp -d)"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
trap 'rm -rf "$WORK"' EXIT

adb_() {
  if [ -n "${ADB_SERIAL:-}" ]; then
    "$ADB" -s "$ADB_SERIAL" "$@"
  else
    "$ADB" "$@"
  fi
}

log()  { printf '%s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

dump_ui() {
  adb_ shell uiautomator dump /sdcard/malina-ui.xml >/dev/null 2>&1 || true
  adb_ shell cat /sdcard/malina-ui.xml > "$1" 2>/dev/null || true
}

# xpath_find <file> <mode> <pattern> -> "x y" для первого совпадения.
# mode=text: совпадение text/content-desc по regex (без учёта регистра);
# mode=id:   совпадение resource-id.
xpath_find() {
  python3 - "$1" "$2" "$3" <<'PY'
import re, sys
xml = open(sys.argv[1]).read()
mode, pat = sys.argv[2], sys.argv[3]
rx = re.compile(pat, re.I)
for m in re.finditer(r'<node\b[^>]*>', xml):
    s = m.group(0)
    if mode == "id":
        if 'resource-id="%s"' % pat not in s:
            continue
    else:
        label = ""
        for key in ("text", "content-desc"):
            mm = re.search(key + r'="([^"]*)"', s)
            if mm and mm.group(1):
                label = mm.group(1)
                break
        if not label or not rx.search(label):
            continue
    b = re.search(r'bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"', s)
    if not b:
        continue
    x1, y1, x2, y2 = map(int, b.groups())
    print((x1 + x2) // 2, (y1 + y2) // 2)
    break
PY
}

tap_text() { local xy; xy="$(xpath_find "$1" text "$2")"; [ -n "$xy" ] || return 1; adb_ shell input tap $xy; }
tap_id()   { local xy; xy="$(xpath_find "$1" id   "$2")"; [ -n "$xy" ] || return 1; adb_ shell input tap $xy; }

wifi_state() {
  adb_ shell dumpsys wifi 2>/dev/null | grep -m1 'mWifiInfo SSID'
}

is_connected() {
  case "$(wifi_state)" in
    *"SSID: $SSID,"*"Supplicant state: COMPLETED"*) return 0 ;;
    *) return 1 ;;
  esac
}

LIST="$WORK/list.xml"
EDIT="$WORK/edit.xml"

open_wifi_settings() {
  adb_ shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
  adb_ shell wm dismiss-keyguard >/dev/null 2>&1 || true
  sleep 1
  adb_ shell am start -n com.android.settings/.wifi.WifiSettings >/dev/null 2>&1
  sleep 3
  for _ in $(seq 1 6); do
    dump_ui "$LIST"
    if grep -qE 'ДОСТУПНЫЕ СЕТИ|Available networks|Добавить сеть|Add network' "$LIST" 2>/dev/null; then
      return 0
    fi
    adb_ shell am start -a android.settings.WIFI_SETTINGS >/dev/null 2>&1
    sleep 3
  done
  return 0
}

connect_from_list() {
  dump_ui "$LIST"
  tap_text "$LIST" "^$(printf '%s' "$SSID" | sed 's/[][\.*^$/]/\\&/g')$" || return 1
  sleep 3
  dump_ui "$EDIT"
  if tap_text "$EDIT" 'подключить$|^connect$|^connect '; then return 0; fi
  if tap_id "$EDIT" "com.android.settings:id/password"; then
    sleep 1
    adb_ shell input text "$PASSWORD"
    sleep 1
    tap_text "$EDIT" '(подключ|connect|сохран)' && return 0
  fi
  return 1
}

add_manually() {
  dump_ui "$LIST"
  tap_text "$LIST" 'добавить сеть|add network' || return 1
  sleep 3
  dump_ui "$EDIT"
  tap_id "$EDIT" "com.android.settings:id/ssid" || return 1
  sleep 1
  adb_ shell input text "$SSID"
  sleep 1
  dump_ui "$EDIT"
  tap_text "$EDIT" '^(нет|none|открыт|open)$' || return 1
  sleep 2
  dump_ui "$EDIT"
  tap_text "$EDIT" 'wpa' || return 1
  sleep 2
  dump_ui "$EDIT"
  tap_id "$EDIT" "com.android.settings:id/password" || return 1
  sleep 1
  adb_ shell input text "$PASSWORD"
  sleep 1
  tap_text "$EDIT" '(подключ|connect|сохран)'
}

log "== ADB =="
adb_ get-state >/dev/null 2>&1 || fail "adb не видит устройство (разрешите отладку по USB)"

adb_ shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
adb_ shell wm dismiss-keyguard >/dev/null 2>&1 || true
adb_ shell svc wifi enable >/dev/null 2>&1 || true
sleep 2

if is_connected; then
  log "== Уже подключено к '$SSID' =="
else
  connected=0
  for attempt in 1 2 3; do
    log "== Попытка $attempt: подключаюсь к '$SSID' =="
    open_wifi_settings
    if ! connect_from_list; then
      log "   сеть не найдена в списке — добавляю вручную"
      add_manually || log "   ручное добавление не удалось"
    fi
    for _ in $(seq 1 20); do
      is_connected && { connected=1; break; }
      sleep 1
    done
    [ "$connected" = 1 ] && break
  done
fi
is_connected || fail "телефон не подключился к '$SSID'"

log "== Жду связности с шлюзом (до ${WAIT_CONNECT}s) =="
ip=""
ok=0
for _ in $(seq 1 "$WAIT_CONNECT"); do
  ip="$(adb_ shell ip -4 addr show wlan0 2>/dev/null | grep -m1 -o 'inet [0-9.]*' | awk '{print $2}')"
  if [ -n "$ip" ]; then
    ping_out="$(adb_ shell ping -c 2 -W 2 "$GATEWAY" 2>&1)"
    loss="$(printf '%s' "$ping_out" | grep -o '[0-9]*% packet loss' | head -1)"
    if [ "$loss" = "0% packet loss" ]; then ok=1; break; fi
  fi
  sleep 1
done
log "   wlan0 IP: ${ip:-нет}, ping $GATEWAY: ${loss:-нет ответа}"
[ "$ok" = 1 ] || fail "нет связности с $GATEWAY с телефона (IP=${ip:-нет})"

log "== Веб-UI: открываю http://$GATEWAY/ в браузере =="
adb_ shell am start -a android.intent.action.VIEW -d "http://$GATEWAY/" >/dev/null 2>&1
sleep 6
adb_ shell screencap -p /sdcard/malina-ap-test.png >/dev/null 2>&1 || true
if adb_ pull /sdcard/malina-ap-test.png "$WORK/screen.png" >/dev/null 2>&1; then
  mkdir -p "$SCRIPT_DIR/test-results"
  cp "$WORK/screen.png" "$SCRIPT_DIR/test-results/phone-ap-connect.png"
  log "   скриншот: tests/test-results/phone-ap-connect.png"
fi

log "PHONE-AP-CONNECT: PASS ($SSID, ${ip}, $GATEWAY)"
