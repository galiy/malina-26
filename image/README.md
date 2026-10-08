# Сборка образа «Малина» (Raspberry Pi OS Lite, Bookworm, armhf)

Воспроизводимая сборка образа на базе pi-gen (ветка `bookworm`) с
кастомным стейджем `stage-malina`. Образ — для Raspberry Pi 3 Model B (927 МБ RAM).

## Что зафиксировано

- ОС: Raspberry Pi OS Lite, Debian 12 (bookworm), armhf (32-bit).
- Пользователь `malina`/`malina`; root разблокирован, пароль `malina`.
- SSH: вход по паролю и по root разрешён.
- Hostname `malina`; локаль `ru_RU.UTF-8`, клавиатура `ru`, TZ `Europe/Moscow`.
- Разделы: boot 512 МБ (FAT, `/boot/firmware`), root 8 ГБ (ext4).
- Корень **read-only**, логи/временные данные — в tmpfs (см. `stage-malina`).
- journald `Storage=volatile` (журнал в `/run`, на диск не пишется).
- Сеть: NetworkManager, `eth0` по DHCP из коробки; WiFi не поднимается.
- Из Lite удалены `avahi-daemon`, `triggerhappy`, `dphys-swapfile`.
- Добавлен `mc`.
- Пакеты ролей (`snmpd`, `watchdog`) — не входят, доставляются позже.

## Файлы

- `config` — конфиг pi-gen.
- `stage-malina/` — кастомный стейдж:
  - `00-packages` — дополнительные пакеты (`mc`);
  - `01-remove` — удаление лишних пакетов;
  - `02-auth` — пароль root и настройки SSH;
  - `03-immutable` — fstab/tmpfs, `ro`, journald, NetworkManager;
  - `04-hostkeys` — предгенерация SSH host keys.
- `setup.sh` — раскладывает конфиг и стейдж в pi-gen, патчит `export-image`
  (root 8 ГБ, `resolv.conf` как симлинк в `/run`).
- `build.sh` — `setup.sh` + сборка через `sudo`.

## Сборка

```
image/build.sh
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
- `/var/lib/bluetooth`, `/var/lib/systemd`, `/var/lib/NetworkManager` — tmpfs,
  чтобы включённые службы не писали в ro-корень.
- `systemd-random-seed` и `systemd-machine-id-commit` замаскированы,
  `machine-id` — транзиентный (`/run`).
