#!/bin/bash -e

# UART для МАП «Титанатор».
#
# МАП подключён к аппаратному UART на пинах GPIO14/15 («порт консоли»).
# Нам нужен аппаратный PL011 (ttyAMA0), а Bluetooth должен остаться живым,
# поэтому уводим BT на mini-UART: dtoverlay=miniuart-bt + enable_uart=1.
# Serial-консоль отключаем (console=tty1), getty на UART — маскируем.
#
# Результат: /dev/serial0 -> ttyAMA0 (PL011, GPIO14/15), BT — на ttyS0 (mini-UART).
# Устройство МАП отвечает по протоколу Микроарт (проверено: native, 115200 8N1).

CFG="${ROOTFS_DIR}/boot/firmware/config.txt"
CMDLINE="${ROOTFS_DIR}/boot/firmware/cmdline.txt"

# Идемпотентно: убрать прежние вхождения, добавить в конец [all].
sed -i '/^enable_uart=/d; /^dtoverlay=miniuart-bt/d; /^dtoverlay=pi3-miniuart-bt/d' "$CFG"
cat >> "$CFG" <<'EOF'

[all]
enable_uart=1
dtoverlay=miniuart-bt
EOF

# Убрать serial-консоль, оставить console=tty1.
sed -i 's/console=serial0,[0-9]* //' "$CMDLINE"

on_chroot <<'EOF'
systemctl mask serial-getty@ttyAMA0.service serial-getty@ttyS0.service 2>/dev/null || true
EOF
