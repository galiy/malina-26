#!/bin/sh
# Установка эмулятора КЭС как постоянной службы systemd.
#
# Размещение (авто): /malina (устройство .60) → /settings (.68) → /opt/kes-emu (прочие).
# Переопределяется: install.sh [-d <каталог>] [-b <бинарник>] [-- доп. аргументы]
#
# Параметры службы: 3 КЭС, 19200, устройства /dev/ttyUSB9..11 (как реальный USB-адаптер;
# старт с 9, чтобы не занять /dev/ttyUSB0). Учитывает ro-корень: для записи unit
# в /etc/systemd/system временно remount,rw / .
set -eu

PROG=kes-emu
UNIT=/etc/systemd/system/${PROG}.service
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)   # daemons/kes-emu

# Аргументы можно задать заранее через KES_EMU_ARGS (тогда host-правило не применяется).
ARGS_FROM_ENV=${KES_EMU_ARGS:-}
KES_EMU_ARGS=

DEST=
BIN=
while [ $# -gt 0 ]; do
	case "$1" in
	-d) DEST=$2; shift 2 ;;
	-b) BIN=$2; shift 2 ;;
	--) shift; break ;;
	*) break ;;
	esac
done
# Остаток аргументов — это явные параметры эмулятора (отменяют host-правило).
EXTRA=""
while [ $# -gt 0 ]; do EXTRA="${EXTRA} $1"; shift; done

# Каталог размещения: явный → /malina → /settings → /opt/kes-emu.
if [ -z "${DEST}" ]; then
	if [ -d /malina ]; then DEST=/malina
	elif [ -d /settings ]; then DEST=/settings
	else DEST=/opt/kes-emu
	fi
fi

# Бинарник по умолчанию — из dist под архитектуру хоста.
if [ -z "${BIN}" ]; then
	case "$(uname -m)" in
	armv7l|armv6l|arm) BIN="${ROOT_DIR}/dist/${PROG}-armv7" ;;
	*)                 BIN="${ROOT_DIR}/dist/${PROG}" ;;
	esac
fi
[ -x "${BIN}" ] || { echo "нет бинарника: ${BIN} (собери: make arm)" >&2; exit 1; }

# Параметры службы: env → явные → дефолт (3 КЭС, 19200, /dev/ttyUSB9..11).
if [ -n "${ARGS_FROM_ENV}" ]; then
	KES_EMU_ARGS="${ARGS_FROM_ENV}"
elif [ -n "${EXTRA}" ]; then
	KES_EMU_ARGS="-n 3 -baud 19200${EXTRA}"
else
	KES_EMU_ARGS="-n 3 -baud 19200 -dir /dev -prefix ttyUSB -start 9"
fi

# Карта памяти — в каталоге размещения.
KES_EMU_ARGS="${KES_EMU_ARGS} -store ${DEST}"


# Останавливаем службу, если уже стоит: иначе бинарник занят («Text file busy»).
systemctl stop "${PROG}" 2>/dev/null || true

echo "== установка ${PROG}: ${BIN} -> ${DEST}/${PROG}"
mkdir -p "${DEST}"
if [ "${BIN}" = "${DEST}/${PROG}" ]; then
	echo "   (бинарник уже на месте)"
else
	install -m 0755 "${BIN}" "${DEST}/${PROG}"
fi

# ro-корень: unit и enable пишут в /etc/systemd/system — временно переводим / в rw.
ROOT_RW=0
if [ ! -w /etc/systemd/system ]; then
	echo "== / ro: remount,rw для установки unit"
	mount -o remount,rw / && ROOT_RW=1
fi
trap 'if [ "${ROOT_RW}" = 1 ]; then sync; mount -o remount,ro / || true; fi' EXIT INT TERM

echo "== служба: ${UNIT}"
echo "   ExecStart=${DEST}/${PROG} ${KES_EMU_ARGS}"
sed -e "s|@BINDIR@|${DEST}|g" \
    -e "s|@ARGS@|${KES_EMU_ARGS}|g" \
    -e "s|@PREFIX@|${SCRIPT_DIR}|g" \
    "${SCRIPT_DIR}/${PROG}.service.in" > "${UNIT}"

systemctl daemon-reload
systemctl enable "${PROG}"
systemctl restart "${PROG}"
systemctl --no-pager status "${PROG}" || true
echo "== готово"
