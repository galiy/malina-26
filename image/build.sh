#!/bin/bash -e

SRC="$(cd "$(dirname "$0")" && pwd)"
PIGEN="${PIGEN:-/opt/raspberry_tmp/pi-gen}"

"$SRC/setup.sh"

SUDO_PASS="$(secret-tool lookup service sudo username sasha)"
if [ -z "$SUDO_PASS" ]; then
	echo "sudo password not found in keyring" >&2
	exit 1
fi

cd "$PIGEN"
echo "$SUDO_PASS" | sudo -S -p '' ./build.sh "$@"
