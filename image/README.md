# Сборка образа «Малина» (Raspberry Pi OS Lite, Bookworm, armhf)

Воспроизводимая сборка образа на базе pi-gen (ветка `bookworm`) с
кастомным стейджем `stage-malina`. Образ — для Raspberry Pi 3 Model B (927 МБ RAM).

## Что зафиксировано

- ОС: Raspberry Pi OS Lite, Debian 12 (bookworm), armhf (32-bit).
- Пользователь `malina` и `root` — пароль **`malina-26`**.
- SSH: вход по паролю и по root разрешён.
- Hostname `malina`; локаль `ru_RU.UTF-8`, клавиатура `ru`, TZ `Europe/Moscow`.
- Разделы: boot 512 МБ (FAT, `/boot/firmware`), root 8 ГБ (ext4).
- Корень **read-only**, логи/временные данные — в tmpfs (см. `stage-malina`).
- Раздел `/malina` (rw) — создаётся при деплое артефактов (локально —
  `deploy/local/stage2.sh`), туда переносятся `/root`, `/home`, cron,
  `wpa_supplicant`.
- Пароль по умолчанию (в образе) для `malina` и `root`, а также WPA-пароль точки
  доступа — **`malina-26`**.
- journald `Storage=volatile` (журнал в `/run`, на диск не пишется).
- Сеть: `eth0` по DHCP; сетью управляет `malina-netd`. NetworkManager и
  `raspberrypi-net-mods` **не устанавливаются** — исключены из базовой установки
  pi-gen в `setup.sh` (зависимых у `raspberrypi-net-mods` нет). `alsa-utils`
  остаётся (жёсткая зависимость `raspi-config`) и маскируется; `x11-common` в
  образе нет; `nfs-common` оставлен.
- Из Lite удалены `avahi-daemon`, `triggerhappy`, `dphys-swapfile`.
- Добавлены `mc` и `redis-server` (in-memory кэш телеметрии, конфиг —
  `system/redis/malina.conf`, см. `docs/redis-cache.md`).
- **watchdog**: аппаратный сторож `bcm2835_wdt` включается через systemd
  (`RuntimeWatchdogSec=15s`, файл `system/systemd/watchdog.conf`); отдельный пакет
  `watchdog` не нужен. Пакет `snmpd` — не входит, доставляется позже.

## Файлы

- `config` — конфиг pi-gen.
- `stage-malina/` — кастомный стейдж:
  - `00-packages` — пакеты проекта из репозиториев (`mc`, `rsyslog`, `iw`,
    `rfkill`, `hostapd`, `dnsmasq`, `nftables`, `isc-dhcp-client`, `gcc`,
    `redis-server`) и
    `01-run-chroot.sh` — `apt update && apt upgrade` до текущего состояния
    публичных репо. **Кастомные артефакты проекта (бинарники/юниты/ассеты
    `malina-*`) в образ НЕ добавляются** — они восстанавливаются из репозитория
    после загрузки (см. `deploy/restore.md`);
  - `01-remove` — удаление лишних пакетов;
  - `02-auth` — пароль root и настройки SSH;
  - `03-immutable` — fstab/tmpfs, `ro`, journald, `/etc/mtab`;
  - `04-hostkeys` — предгенерация SSH host keys;
  - `05-map-uart` — UART для МАП (`enable_uart=1`, `dtoverlay=miniuart-bt`,
    без serial-консоли; BT остаётся живым).
- `setup.sh` — раскладывает конфиг и стейдж в pi-gen, патчит `export-image`
  (root 8 ГБ, `resolv.conf` как симлинк в `/run`).
- `build.sh` — `setup.sh` + сборка через `sudo`.

## Сборка

```
image/build.sh
```

**Важно (кэш pi-gen).** pi-gen переиспользует `work/` со стадиями по маркерам: при
изменении стейджа/списков пакетов стадии **пропускаются**, и правки не попадут в
образ. Перед пересборкой после любых изменений очищать кэш:
```
sudo rm -rf /opt/raspberry_tmp/pi-gen/work
```

Требуется: `sudo` (пароль из связки `service=sudo username=sasha`),
`bzimage`-эмуляция armhf (binfmt + `qemu-arm-static`), доступ в интернет,
~15 ГБ свободного места. Результат — в `/opt/raspberry_tmp/pi-gen/deploy/`.

## Запись на SD (позже)

```
sudo dd if=deploy/2026-10-07-malina-lite.img of=/dev/sdX bs=4M conv=fsync status=progress
```

Root-раздел уже 8 ГБ; авторасширение (`resize2fs_once`) отключено.

## Заметки

- Корень смонтирован `ro`. Для доустановки пакетов:
  `mount -o remount,rw /` → `apt ...` → `mount -o remount,ro /`.
- **SSH host keys предгенерены в образе** — при клонировании на несколько
  устройств они будут одинаковыми.
- `fake-hwclock` на ro-корне не сохраняет состояние (чтение работает),
  время подтягивается по NTP.
- Токен `init=/usr/lib/raspberrypi-sys-mods/firstboot` убран из `cmdline.txt`,
  поэтому нет первого ребута и rw-remount корня (rename/resize отключены,
  host keys уже есть).
- `/var/lib/bluetooth`, `/var/lib/systemd`, `/var/lib/dhcp` — tmpfs,
  чтобы включённые службы не писали в ro-корень.
- `systemd-random-seed` и `systemd-machine-id-commit` замаскированы,
  `machine-id` — транзиентный (`/run`).

## UART для МАП (стадия `05-map-uart`)

МАП «Титанатор» подключён к аппаратному UART на пинах **GPIO14/15** («порт
консоли»). Образ отдаёт этот UART МАПу, сохраняя Bluetooth:

- `config.txt` (`[all]`): `enable_uart=1`, `dtoverlay=miniuart-bt`;
- `cmdline.txt`: убран `console=serial0,115200` (остаётся `console=tty1`);
- getty на UART замаскирован (`serial-getty@ttyAMA0`, `serial-getty@ttyS0`).

Итог: `/dev/serial0` → **`ttyAMA0`** (PL011, GPIO14/15) — МАП; Bluetooth уходит
на mini-UART (`ttyS0`) и **остаётся живым**.

Старый образ был иначе (`dtoverlay=pi3-disable-bt`, BT выключен, МАП на
`ttyAMA0`). Вариант с `miniuart-bt` выбран, чтобы не терять BT. Откат:
`dtoverlay=pi3-disable-bt` + `enable_uart=1` (тогда BT отключён).

Подробности по подключению и проверке — `docs/map.md`.
