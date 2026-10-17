#!/bin/bash -e

PIGEN="${PIGEN:-/opt/raspberry_tmp/pi-gen}"
SRC="$(cd "$(dirname "$0")" && pwd)"

if [ ! -d "$PIGEN/.git" ]; then
	echo "pi-gen not found at $PIGEN" >&2
	exit 1
fi

git -C "$PIGEN" fetch --quiet origin
git -C "$PIGEN" checkout -q bookworm
git -C "$PIGEN" reset --hard -q origin/bookworm

cp "$SRC/config" "$PIGEN/config"

# Не ставить лишнее: NetworkManager тянется только raspberrypi-net-mods (зависимых
# у неё нет) — исключаем их из базовой установки pi-gen (а не «ставим и чистим»).
sed -i '/^raspberrypi-net-mods$/d; /^network-manager$/d' \
	"$PIGEN/stage2/02-net-tweaks/00-packages"

rm -rf "$PIGEN/stage-malina"
cp -a "$SRC/stage-malina" "$PIGEN/stage-malina"
find "$PIGEN/stage-malina" -name '*-run.sh' -exec chmod +x {} +
chmod +x "$PIGEN/stage-malina/prerun.sh" "$PIGEN/stage-malina/EXPORT_IMAGE"

touch "$PIGEN/stage2/SKIP_IMAGES"

# root 3 ГиБ: занято ~1,4 ГиБ (ОС), остальное — запас; p3 /malina (статистике)
# отдаётся остаток SD (карты 8-32 ГиБ). См. docs/telemetry-store.md.
sed -i 's|^ROOT_PART_SIZE=.*|ROOT_PART_SIZE=$((3 * 1024 * 1024 * 1024))|' \
	"$PIGEN/export-image/prerun.sh"

cat > "$PIGEN/export-image/03-network/01-run.sh" <<'EOS'
#!/bin/bash -e

rm -f "${ROOTFS_DIR}/etc/resolv.conf"
ln -sf /run/NetworkManager/resolv.conf "${ROOTFS_DIR}/etc/resolv.conf"
EOS
chmod +x "$PIGEN/export-image/03-network/01-run.sh"

sed -i '1a sed -i "s| init=/usr/lib/raspberrypi-sys-mods/firstboot||" "${ROOTFS_DIR}/boot/firmware/cmdline.txt"' \
	"$PIGEN/export-image/05-finalise/01-run.sh"

echo "pi-gen prepared: $PIGEN (branch bookworm)"
