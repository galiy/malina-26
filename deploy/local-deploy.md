# Локальный деплой (с этого ПК) — этапы 1–2

## Зафиксированные требования (порядок деплоя)

- **Целевой деплой — локально на этом ПК** (SD-карта в картридере на ноутбуке).
- **Этап 1:** деплой ОС **без наших артефактов** (базовый образ).
- **Этап 2:** деплой наших артефактов **локально с этого ПК** (монтируем разделы
  карты и раскладываем файлы).
- Затем **запуск ПАК на железе**, проверка; ошибки плана исправляем **по SSH**,
  параллельно правим этот план. Цель — рабочая система.
- Финал: **вынуть SD, стереть, повторить локальный деплой**, поставить в ПАК,
  проконтролировать результат.
- **Главный риск Этапа 2 — права доступа и владелец** (root:root, режимы,
  симлинки). Уделять особое внимание.
- Локальный деплой должен быть воспроизводим идемпотентно (шаг «повторить»).

---

## Этап 1 — ОС (базовый образ)

Детали сборки — `image/README.md`. Кратко:

1. Предпосылки: `pi-gen` в `/opt/raspberry_tmp/pi-gen` (ветка `bookworm`),
   `qemu-arm-static` + binfmt (есть), интернет к `raspbian.raspberrypi.com`
   (по **http**), `archive.raspberrypi.com`, `deb.debian.org`, sudo-пароль из
   связки, ~15 ГБ места.
2. **Чистка кэша pi-gen** (обязательно при повторной сборке после изменений):
   pi-gen переиспользует `work/` по маркерам стадий и пропустит изменённые стадии
   → правки не попадут в образ.
   ```
   sudo rm -rf /opt/raspberry_tmp/pi-gen/work
   ```
3. Сборка: `image/build.sh` (`setup.sh` копирует `config` + `stage-malina` в pi-gen,
   патчит `export-image`; `build.sh` → полная сборка) → результат в
   `/opt/raspberry_tmp/pi-gen/deploy/`.
4. **Состав образа (решено):**
   - входят **только пакеты из репозиториев** (`stage-malina/00-packages` +
     `apt update/upgrade`): mc, rsyslog, iw, rfkill, hostapd, dnsmasq, nftables,
     isc-dhcp-client, gcc;
   - **не устанавливаются**: `network-manager` и `raspberrypi-net-mods` (исключены
     из `stage2/02-net-tweaks/00-packages` в `image/setup.sh`; зависимых у
     `raspberrypi-net-mods` нет);
   - `01-remove`: `avahi-daemon`, `triggerhappy`, `dphys-swapfile`;
   - `alsa-utils` **остаётся** (жёсткая зависимость `raspi-config`) и маскируется;
     `x11-common` в образе отсутствует; `nfs-common` оставлен;
   - fstab (`03-immutable`): `ro`-корень, boot `ro`, tmpfs `/tmp,/var/log,/var/tmp,
     /var/spool,/var/lib/dhcp,/var/lib/bluetooth,/var/lib/systemd`, `/etc/mtab`
     symlink; строки NM-tmpfs нет;
   - пароли по умолчанию в образе: `malina` и `root` — **`malina-26`**
     (`image/config` `FIRST_USER_PASS`, `stage-malina/02-auth` root chpasswd);
   - **наши бинарники/юниты/ассеты — НЕ входят**.
5. Проверка образа (loop-mount ro): нет `/usr/local/bin|sbin` с нашими файлами и
   юнитов `malina-*`; нет `network-manager`/`raspberrypi-net-mods`/`x11-common`;
   есть `alsa-utils`, пакеты проекта; fstab с `/var/lib/dhcp`; hostname/TZ заданы.
6. Запись на SD:
   ```
   sudo dd if=<img> of=/dev/sdX bs=4M conv=fsync status=progress
   # или: sudo bmaptool copy <img> /dev/sdX
   ```
   Образ ставит только p1 (boot) и p2 (root). **p3 (/malina) создаёт Этап 2.**

**Правки образа, потребовавшиеся при обкатке (Этап 1, не относящиеся к артефактам
Этапа 2):**
- не было `tmpfs /var/lib/dhcp` → на ro-корне dhclient не мог писать аренды
  (`Read-only file system`) → добавлено в fstab образа;
- не создавался симлинк `/etc/mtab` → `systemd-tmpfiles … Read-only file system`
  → добавлен в образ (`03-immutable`);
- `network-manager` и `raspberrypi-net-mods` исключены из установки pi-gen
  (`image/setup.sh`); из образа убраны `tmpfs /var/lib/NetworkManager` и конфиги
  `/etc/NetworkManager`;
- pi-gen переиспользует `work/` по маркерам стадий (см. п.2) — без чистки правки
  в образ не попадают.

**Статус:** образ `2026-10-10-malina-lite.img` пересобран начисто (без NM) и
проверяется (см. журнал).

---

## Этап 2 — артефакты проекта (локально, на ПК)

### Модель и раскладка
- Источник: репозиторий (`$REPO`) + собранные `*/dist/*-armv7`.
- Цель: SD в ридере (`/dev/sdX`) либо тест-образ (`loop`).
- Разделы карты: `p1` boot (FAT, не трогаем), `p2` root (ext4, **ro в fstab**),
  **`p3` /malina (ext4, rw)** — создаём в оставшемся месте карты.
- Все операции — от **root**; владельцы/режимы — как на устройстве.
- Скрипт: `deploy/local/stage2.sh <device|image>` (реализация плана; подробности ниже).

### Шаги

**2.0. Подготовка (ноутбук).** Собрать бинарники:
```
make -C web arm
make -C daemons/map-reader arm
```
Определить SD (`lsblk -o NAME,SIZE,TYPE,LABEL,MOUNTPOINT`), **отключить авто-монтирование**
(udisks может подхватить разделы и помешать). Не путать целевой диск с системными.

**2.1. Разметка p3.** Создать раздел 3 в оставшемся месте + ФС:
```
sfdisk --force --no-reread -N 3 /dev/sdX <<< ", +, 83"
partprobe /dev/sdX ; udevadm settle
mkfs.ext4 -F -q -L malina /dev/sdX3
```
(для тест-образа: увеличить файл `truncate -s +5G`, затем `losetup -Pf`.)

**2.2. Монтирование.** `mount /dev/sdX2 $ROOT` (rw), `mount /dev/sdX3 $MAL`.
Не полагаться на auto-mount.

**2.3. fstab.** Добавить (если нет) `LABEL=malina  /malina  ext4  defaults,noatime  0  2`.
Строку ro-корня/`/tmp`/`/boot/firmware` из образа **не менять**.

**2.4. Дерево /malina и владельцы:**
```
mkdir -p $MAL/{etc/malina/network,etc/wpa_supplicant,home,root,var/spool/cron/crontabs}
cp -a $PROV/../network/mode1.json ... (см. manifest: 0600)
install -m0600 system/network/mode1.json $MAL/etc/malina/network/mode1.json   # if_absent
install -m0600 system/network/mode2.json $MAL/etc/malina/network/mode2.json
install -m0644 system/network/active     $MAL/etc/malina/network/active
chown root:root $MAL/etc $MAL/home
chown root:root $MAL/root ; chmod 0700 $MAL/root
chown root:crontab $MAL/var/spool/cron/crontabs ; chmod 1730 .../crontabs
```

> AP-пароль по умолчанию (`system/network/mode1.json`, ставится по манифесту
> `if_absent`) — **`malina-26`**. Новый юнит `malina-net-wifi.service`
> (опция «Выключить Wi-Fi») также ставится из `deploy/manifest.tsv`.

**2.5. Перенос и симлинки в rootfs** (сначала перенести содержимое, потом заменить):
```
# /root, /home были реальными каталогами в образе — перенести их данные
cp -a $ROOT/root/. $MAL/root/ 2>/dev/null || true
cp -a $ROOT/home/. $MAL/home/ 2>/dev/null || true
rm -rf $ROOT/root $ROOT/home
ln -s /malina/root $ROOT/root
ln -s /malina/home $ROOT/home
# cron и wpa
[ -d $ROOT/var/spool/cron ] && ! [ -L $ROOT/var/spool/cron ] && { cp -a $ROOT/var/spool/cron/. $MAL/var/spool/cron/ 2>/dev/null || true; rm -rf $ROOT/var/spool/cron; ln -s /malina/var/spool/cron $ROOT/var/spool/cron; }
[ -d $ROOT/etc/wpa_supplicant ] && ! [ -L $ROOT/etc/wpa_supplicant ] && { cp -a $ROOT/etc/wpa_supplicant/. $MAL/etc/wpa_supplicant/; rm -rf $ROOT/etc/wpa_supplicant; ln -s /malina/etc/wpa_supplicant $ROOT/etc/wpa_supplicant; }
# resolv.conf, dbus machine-id
mkdir -p $ROOT/run $ROOT/var/lib/dbus
ln -sfn /run/malina-net/resolv.conf $ROOT/etc/resolv.conf
ln -sfn /etc/machine-id $ROOT/var/lib/dbus/machine-id
```

**2.6. machine-id, hostname:**
```
install -m0444 $PROV/machine-id $ROOT/etc/machine-id
install -m0644 $PROV/hostname   $ROOT/etc/hostname
```

**2.7. Артефакты `system/*` по manifest** (как `install.sh`):
для каждой строки `src dst mode policy` — `install -D -o root -g root -m <mode> $REPO/$src $ROOT$dst`
(политика `if_absent` — не перезаписывать существующее).

**2.8. Бинарники (root:root, 0755):**
```
install -D -o root -g root -m0755 web/dist/malina-web-armv7            $ROOT/usr/local/bin/malina-web
install -D -o root -g root -m0755 web/dist/malina-netd-armv7          $ROOT/usr/local/sbin/malina-netd
install -D -o root -g root -m0755 daemons/map-reader/dist/map-reader-armv7  $ROOT/usr/local/sbin/map-reader
```

**2.9. Веб-ассеты (на /malina, root:root):**
```
rm -rf $MAL/usr/local/share/malina/web
mkdir -p $MAL/usr/local/share/malina/web
cp -a web/assets/templates web/assets/static web/assets/i18n $MAL/usr/local/share/malina/web/
find $MAL/usr/local/share/malina/web -type d -exec chmod 0755 {} +
find $MAL/usr/local/share/malina/web -type f -exec chmod 0644 {} +
chown -R root:root $MAL/usr/local
```

**2.10. PAM-хелпер (armhf) — компиляция в chroot образа (qemu):**
```
cp /usr/bin/qemu-arm-static $ROOT/usr/bin/ 2>/dev/null || true
chroot $ROOT /usr/bin/gcc -O2 -o /tmp/malina-pamcheck /tmp/pamcheck.c -ldl
install -m0755 $ROOT/tmp/malina-pamcheck $MAL/usr/local/libexec/malina-pamcheck
rm -f $ROOT/tmp/malina-pamcheck $ROOT/usr/bin/qemu-arm-static
```
(альтернатива — `arm-linux-gnueabihf-gcc` на ПК, если установлен.)

**2.11. tmpfiles/journald/timesyncd/rsyslog/logrotate** — уже ставятся по manifest (2.7).
`systemd-tmpfiles --root=$ROOT --create` — best-effort (каталоги `/run/*` создаются
на загрузке).

**2.12. Юниты offline (включение/маски/отключение):**
```
for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader; do
  systemctl --root=$ROOT enable "$u" || true
done
# mask/disable по system/provision/{masked,disabled}.txt
while read -r u; do [ -n "$u" ] && systemctl --root=$ROOT mask "$u" || true; done < $PROV/masked.txt
while read -r u; do [ -n "$u" ] && systemctl --root=$ROOT disable "$u" || true; done < $PROV/disabled.txt
```

**2.13. Удаление NetworkManager** (provision это делает): в chroot `apt-get purge -y network-manager`,
или (если apt в chroot неудобен) `systemctl --root=$ROOT disable network-manager` и удалить
юнит/бинарники. Приоритетно — purge, как на устройстве.

**2.14. Синхронизация/размонтирование:**
```
sync
umount $MAL $ROOT   # (для loop: losetup -d)
```

**2.15. Проверки offline перед извлечением карты:**
```
# владельцы: не root там, где должен быть root
find $ROOT/etc $ROOT/usr $MAL/usr -not -user root -printf '%u %p\n' | head
# ключевые файлы и режимы
ls -l $ROOT/etc/systemd/system/malina-*.service $ROOT/usr/local/bin/malina-web $ROOT/usr/local/sbin/{map-reader,malina-netd}
ls -l $MAL/etc/malina/network $MAL/usr/local/share/malina/web | head
readlink $ROOT/root $ROOT/home $ROOT/etc/wpa_supplicant $ROOT/etc/resolv.conf
grep malina $ROOT/etc/fstab
```

### Нюансы (обязательно к соблюдению)

- **Владелец/права:** всё, что добавляем — `root:root`, кроме явного:
  `/home/malina` (malina:malina 0700), `/malina/root` (root 0700),
  `/malina/var/spool/cron/crontabs` (root:crontab 1730). После копий делать `chown -R`.
  Не допускать попадания uid:sasha (1000) в rootfs/`/malina`.
- **Режимы:** юниты/конфиги `0644`, хелперы/скрипты `0755`, `mode*.json` `0600`,
  `machine-id` `0444`.
- **Симлинки** не разыменовывать: `cp -a`, `install` для файлов; сам симлинк — `ln -s`.
- **ro-корень:** fstab образа уже `ro`; локально пишем, т.к. монтируем rw. Не менять ro-опции.
- **boot (FAT)** не трогаем (в образе уже UART/cmdline/config).
- **systemctl --root** работает offline; проверять, что symlink появился в
  `$ROOT/etc/systemd/system/*.wants/`.
- **Авто-монтирование** раздела карты на ПК отключить, монтировать вручную, чтобы
  не поймать гонку/частичную запись.
- **Тест-образ vs SD:** p3 нельзя создать в «обрезанном» образе — для теста файл
  увеличиваем (`truncate`) и работаем через `losetup -Pf`.
- **Идемпотентность:** повторный прогон не должен ломать существующее (режимы
  `if_absent`, проверки `! -L`).

### Уроки локального прогона (проверено на копии образа)
- **p3:** начало считать от конца p2 (`/sys/class/block/<p2>/start|size`), а не
  через `, +` в `sfdisk` — иначе раздел создаётся крошечным и с перекрытием.
- **Владелец:** на ПК **нет** пользователей `malina`/`crontab` — `chown` только
  **числовыми** uid/gid, взятыми из образа (`/etc/passwd`, `/etc/group`).
- **Нельзя** `chown -R root:root` по всему rootfs `/etc` — это ломает системные
  группы (`/etc/shadow`→`shadow`, `gshadow`, `sudoers`). Артефакты ставить
  `install -o root -g root`; точечные `chown` только для добавленного.
- **Дистрибутивные конфликты:** образ держит включёнными `dnsmasq.service`,
  `hostapd.service`, `wpa_supplicant.service`, `ModemManager.service`. Сеть проекта
  управляется `malina-netd` через свои обёртки `malina-{hostapd,dnsmasq,wpa}.service`
  → дистрибутивные нужно **disable** (иначе `dnsmasq` держит `:53`/`10.10.100.254`
  и наш `malina-dnsmasq` падает). Внесены в `system/provision/disabled.txt`.
- **`/` ro:** `systemctl disable/mask` на живой системе не сработает (симлинки в
  `/etc`); делать через rw-окно (`/malina/usr/local/sbin/malina-rw run systemctl …`)
  либо офлайн в `stage2.sh` (там rootfs ещё rw).
- **Повреждённый inode:** наблюдался случай, когда ошибка стабильно указывала на
  **один inode** (`/etc/hostname`): `EXT4-fs error … inode #N: corrupted inode
  contents` при записи, при этом `e2fsck -fn` считает ФС чистой. Лечение —
  **пересоздать файл** (`rm` + заново создать), т.е. заменить inode, затем `sync` и
  `e2fsck -fn`. `sync` в `malina-rw` сам по себе это не устраняет. Подтверждено:
  после пересоздания inode `/etc/hostname` смена «Хост» работает, `ext4 errors=0`.
  Артефакт ужесточён — `SetHost` пишет `/etc/hostname`/`/etc/timezone` через
  временный файл + `mv` (новый inode, существующий не перезаписывается).
- **rw-окно и `commit=60`:** `malina-rw` делает `sync` перед `remount,ro`
  (иначе при `commit=60` остаётся unprocessed orphan inode list → следующий
  `remount,rw` падает, а запись даёт `EXT4-fs error … corrupted inode contents`).
  Добавлено `sync` в `malina-rw run/off`. При сбое ФС восстанавливается журналом
  (монтирование rw/`e2fsck -fn` даёт clean), т.е. это не смерть носителя.
- **Пути артефактов:** ВСЕ исполняемые/данные проекта живут на `/malina`
  (`/malina/usr/local/{bin,sbin,libexec,share/...}`) — так их ждут юниты
  (`ExecStart=/malina/usr/local/...`). В rootfs (`/etc/...`) — только юниты/конфиги.
  Назначения манифеста и бинарники с префиксом `/malina/` класть на **p3**, не в
  rootfs (иначе сервисы падают с `203/EXEC` и AP не поднимается).
- **fstab-tmpfs:** образ содержит `tmpfs /var/lib/NetworkManager` (нужен, пока есть NM).
  Этап 2 удаляет NM (и каталог) → `mount` tmpfs падает → `local-fs.target` →
  **emergency mode**, `malina-net` не стартует. Поэтому `stage2.sh` после purge
  удаляет эту строку из fstab и на всякий случай создаёт все tmpfs-точки из fstab.
- **Аппаратное (SD/питание) — Этап 3:** на двух разных картах **первая же запись в
  rw-окно** (например, смена «Хост») даёт `EXT4-fs error … corrupted inode
  contents` → `EFSCORRUPTED`, после чего `remount,rw /` блокируется
  («unprocessed orphan inode list»). Это уровень ФС/носителя, не наш код. Перед
  деплоем проверять носитель (`f3`/`h2testw` на ПК, `e2fsck -f`) и питание
  (`vcgencmd get_throttled`, отсутствие `Under voltage`); при сомнении — менять
  карту/БП. В эксплуатации rw-записи минимизировать.
- **Точка монтирования `/malina`:** в rootfs **должен существовать каталог
  `/malina`** (его нет в базовом образе). Без него `malina.mount` падает →
  `local-fs.target` → **emergency**. `stage2.sh` создаёт `$ROOTM/malina` (0755);
  на старой карте лечится в аварийной консоли: `mount -o remount,rw /; mkdir -p
  /malina; mount -o remount,ro /; systemctl default`.
- **`/var/lib/dhcp`:** если в fstab образа строки нет — `stage2.sh` добавляет её и
  создаёт точку (иначе dhclient-аренды пишутся на ro-корень).
- **udisks2:** включён в образе и пишет `/var/lib/udisks2` на ro → ошибки + шум
  mdraid; добавлен в `disabled.txt`.
- **Остатки NM:** `apt purge` не удаляет `/etc/NetworkManager` (создавал образ) →
  `stage2.sh` делает `rm -rf /etc/NetworkManager`.
- **rsyslog (UDP-пересылка):** до появления маршрута сыпет `Network is unreachable`;
  в `system/bin/malina-remote-syslog` добавлены `action.resumeRetryCount="-1"` и
  `queue.type="linkedList"` (буферизация, отправка после поднятия сети).
- **Имя хоста и часовой пояс:** хранятся в `/etc` (`/etc/hostname`, `/etc/timezone`,
  симлинк `/etc/localtime`) и меняются из UI через короткое rw-окно `malina-rw`
  (**не** на `/malina`).
- Проверено на dry-run (loop-копия образа): p3=остаток места; симлинки
  `/root`,`/home`,`/etc/wpa_supplicant`,`/etc/resolv.conf`; fstab `ro`+`LABEL=malina`;
  юниты (`.service`→multi-user, `.timer`→timers) включены; `pamcheck` собрался как
  ELF ARM (chroot+qemu); NetworkManager удалён; владельцы добавленного `root:root`,
  `/malina/home/malina` = 1000:1000, `crontabs` = root:`crontab`-gid; `/etc/shadow`
  не пострадал.

---

## Этап 3 — запуск на ПАК и отладка

1. Вставить SD в ПАК, включить.
2. SSH (`malina@…`, пароль дефолтный; затем сменить).
3. Диагностика и правки **по SSH**, параллельно — правки этого плана/скрипта:
```
systemctl --failed
journalctl -b -p warning --no-pager
systemctl status malina-web map-reader malina-net rsyslog ssh
ip -br a ; nft list ruleset
curl -sI http://127.0.0.1/login | head -1
```
4. Добиться рабочей системы (режим 1 AP + web + map-reader + логи).
5. Опция «Выключить Wi-Fi» (mode1, `wifi_off_on_eth`): при подключённом Ethernet
   AP гаснет (`hostapd`/`dnsmasq` inactive, `wlan0 DOWN`, `rfkill` soft-blocked),
   при отключении кабеля — поднимается; watchdog/idle-перезапуск hostapd при
   погашенном Wi-Fi **не циклит**. STA (mode2) не затронут.

## Этап 4 — контроль-повтор

1. Вынуть SD, стереть (`wipefs`/`dd` нули начала), повторить Этап 1 + Этап 2.
2. Вставить, загрузить, проверить. План/скрипт — уже уточнённые.

---

## Быстрый прогон

```
# Этап 1 уже собран: /opt/raspberry_tmp/pi-gen/deploy/2026-10-10-malina-lite.img
sudo dd if=<img> of=/dev/sdX bs=4M conv=fsync status=progress
# Этап 2:
deploy/local/stage2.sh /dev/sdX
```
