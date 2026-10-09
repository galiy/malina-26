#!/bin/sh
# Пере-деплой map-reader на устройство: сборка arm → scp бинарника и юнита →
# установка → enable --now. Идемпотентно; для будущих правок/багфиксов.
#
# Использование:
#   MALINA_PASS=<пароль> daemons/map-reader/deploy.sh user@host
set -e

HOST="${1:?usage: deploy.sh user@host}"
: "${MALINA_PASS:?set MALINA_PASS (пароль root на устройстве)}"
DIR=$(cd "$(dirname "$0")" && pwd)

echo "== build arm =="
make -C "$DIR" arm

SSH="sshpass -p $MALINA_PASS ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10"
SCP="sshpass -p $MALINA_PASS scp -o StrictHostKeyChecking=no"

echo "== copy =="
$SCP "$DIR/dist/map-reader-armv7" "$HOST:/tmp/map-reader.new"
$SCP "$DIR/map-reader.service" "$HOST:/tmp/map-reader.service"

echo "== install/enable =="
$SSH "$HOST" '
	set -e
	install -D -m0755 /tmp/map-reader.new /malina/usr/local/sbin/map-reader
	mount -o remount,rw /
	install -m0644 /tmp/map-reader.service /etc/systemd/system/map-reader.service
	systemctl daemon-reload
	systemctl enable map-reader
	sync
	mount -o remount,ro /
	systemctl restart map-reader
	rm -f /tmp/map-reader.new /tmp/map-reader.service
	echo "== status =="
	systemctl --no-pager --full status map-reader | head -12 || true
'

echo "== done: $HOST =="
