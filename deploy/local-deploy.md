# Локальный деплой (с этого ПК)

> **Один локальный вариант** — `deploy/local/deploy-local.sh <dev|image>`:
> **перезаливка голого образа + создание p3 `/malina` + раскладка артефактов +
> offline-проверка** одним прогоном (заменил `stage2.sh`/`stage4.sh`).
> Для переноса по сети агент читает шаги этого скрипта и воспроизводит их на
> устройстве вручную — см. `deploy/redeploy-plan.md`, Вариант B.

## Зафиксированные требования (порядок деплоя)

- **Целевой деплой — локально на этом ПК** (SD-карта в картридере на ноутбуке).
- **Этап 1:** собрать голый образ **без наших артефактов** (базовый образ).
- **Локальный деплой:** `deploy/local/deploy-local.sh <dev|image>` — заливает
  образ, создаёт p3 и раскладывает артефакты (все шаги — в скрипте, ниже
  описаны для понимания и ручного повторения в Варианте B).
- Затем **запуск ПАК на железе**, проверка; ошибки плана исправляем **по SSH**,
  параллельно правим этот план. Цель — рабочая система.
- Финал: `deploy-local.sh` идемпотентно повторяет (перезаливка+раскладка+проверка).
- **Главный риск локального деплоя — права доступа и владелец** (root:root,
  режимы, симлинки). Уделять особое внимание.
- Локальный деплой должен быть воспроизводим идемпотентно (шаг «повторить»).
- **Перенос ПАК по сети** (старый ПАК, без вынимания карты) — отдельная методика:
  `deploy/redeploy-plan.md` (Вариант B).

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
   Образ ставит только p1 (boot) и p2 (root). **p3 (/malina) создаёт локальный
   деплой** (`deploy-local.sh`).

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
- Скрипт: `deploy/local/deploy-local.sh <device|image>` (реализация плана; подробности ниже).

### Шаги

**2.0. Подготовка (ноутбук).** Собрать бинарники:
```
make -C web arm
make -C daemons/map-reader arm
make -C daemons/kes-reader arm
make -C daemons/bms-reader arm
make -C daemons/grid-reader arm
make -C daemons/bms-proxy arm
make -C daemons/tsdb arm
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

**2.7. Артефакты `system/*` по manifest** (как `deploy.sh --first-run`):
для каждой строки `src dst mode policy` — `install -D -o root -g root -m <mode> $REPO/$src $ROOT$dst`
(политика `if_absent` — не перезаписывать существующее).

**2.8. Бинарники (root:root, 0755):**
```
install -D -o root -g root -m0755 web/dist/malina-web-armv7            $ROOT/usr/local/bin/malina-web
install -D -o root -g root -m0755 web/dist/malina-netd-armv7          $ROOT/usr/local/sbin/malina-netd
install -D -o root -g root -m0755 daemons/map-reader/dist/map-reader-armv7  $ROOT/usr/local/sbin/map-reader
install -D -o root -g root -m0755 daemons/kes-reader/dist/kes-reader-armv7  $ROOT/usr/local/sbin/kes-reader
install -D -o root -g root -m0755 daemons/bms-reader/dist/bms-reader-armv7  $ROOT/usr/local/sbin/bms-reader
install -D -o root -g root -m0755 daemons/grid-reader/dist/grid-reader-armv7 $ROOT/usr/local/sbin/grid-reader
install -D -o root -g root -m0755 daemons/bms-proxy/dist/bms-proxy-armv7  $ROOT/usr/local/sbin/bms-proxy
install -D -o root -g root -m0755 daemons/tsdb/dist/malina-tsdb-armv7        $ROOT/usr/local/sbin/malina-tsdb
```

**2.9. Веб-ассеты и manuals (на /malina, root:root):**
```
rm -rf $MAL/usr/local/share/malina/web
mkdir -p $MAL/usr/local/share/malina/web
cp -a web/assets/templates web/assets/static web/assets/i18n $MAL/usr/local/share/malina/web/
find $MAL/usr/local/share/malina/web -type d -exec chmod 0755 {} +
find $MAL/usr/local/share/malina/web -type f -exec chmod 0644 {} +

rm -rf $MAL/usr/local/share/malina/manuals
install -d -m0755 $MAL/usr/local/share/malina/manuals
cp -a manuals/. $MAL/usr/local/share/malina/manuals/
find $MAL/usr/local/share/malina/manuals -type d -exec chmod 0755 {} +
find $MAL/usr/local/share/malina/manuals -type f -exec chmod 0644 {} +
chown -R root:root $MAL/usr/local
```

`manuals/` обязателен: `malina-web` ищет справку в
`/malina/usr/local/share/malina/manuals`; без каталога `/help?doc=…` отдаёт 404.

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
for u in dhclient-eth0 malina-web malina-net malina-net-fallback.timer malina-logrotate.timer map-reader kes-reader malina-bms-reader malina-bms-proxy malina-grid-reader; do
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
ls -l $ROOT/etc/systemd/system/malina-*.service $ROOT/usr/local/bin/malina-web $ROOT/usr/local/sbin/{map-reader,kes-reader,bms-reader,bms-proxy,malina-netd}
ls -l $MAL/etc/malina/network $MAL/usr/local/share/malina/web $MAL/usr/local/share/malina/manuals | head
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
  либо офлайн в `deploy-local.sh` (там rootfs ещё rw).
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
  `deploy-local.sh` удаляет NM (и каталог) → `mount` tmpfs падает → `local-fs.target` →
  **emergency mode**, `malina-net` не стартует. Поэтому `deploy-local.sh` после purge
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
  `local-fs.target` → **emergency**. `deploy-local.sh` создаёт `$ROOTM/malina` (0755);
  на старой карте лечится в аварийной консоли: `mount -o remount,rw /; mkdir -p
  /malina; mount -o remount,ro /; systemctl default`.
- **`/var/lib/dhcp`:** если в fstab образа строки нет — `deploy-local.sh` добавляет её и
  создаёт точку (иначе dhclient-аренды пишутся на ro-корень).
- **udisks2:** включён в образе и пишет `/var/lib/udisks2` на ro → ошибки + шум
  mdraid; добавлен в `disabled.txt`.
- **Остатки NM:** `apt purge` не удаляет `/etc/NetworkManager` (создавал образ) →
  `deploy-local.sh` делает `rm -rf /etc/NetworkManager`.
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
systemctl status malina-web map-reader kes-reader bms-reader bms-proxy malina-net rsyslog ssh
ip -br a ; nft list ruleset
curl -sI http://127.0.0.1/login | head -1
```
4. Добиться рабочей системы (режим 1 AP + web + map-reader + логи).
5. Опция «Выключить Wi-Fi» (mode1, `wifi_off_on_eth`): при подключённом Ethernet
   AP гаснет (`hostapd`/`dnsmasq` inactive, `wlan0 DOWN`, `rfkill` soft-blocked),
   при отключении кабеля — поднимается; watchdog/idle-перезапуск hostapd при
   погашенном Wi-Fi **не циклит**. STA (mode2) не затронут.

## Контроль-повтор (полный цикл)

1. Локально: `deploy/local/deploy-local.sh /dev/sdX` — перезаливка образа + p3 +
   раскладка + offline-проверка (заменяет прежние Этап 1+2+повтор).
2. Вставить, загрузить, проверить (`systemctl is-active …`, веб). План/скрипт —
   уже уточнённые.
3. **Перенос ПАК по сети** (старый ПАК, без вынимания карты) — по методике
   `deploy/redeploy-plan.md` (Вариант B), не через локальный скрипт.

---

## Вариант 3 — релизный образ

Готовый полный образ диска для публикации и тиража: p1 + p2 + p3 `/malina` с
разложенными артефактами. Пользователь только пишет образ и включает устройство.

Базовый образ собирается по **варианту 1** (`image/README.md`, `image/build.sh`).

Параметры:

- p3 `/malina` — **3 ГиБ**;
- итоговый образ — **6,51 ГиБ** (6 988 759 040 байт);
- носитель — SD **8 ГБ** с реальной ёмкостью не меньше ~7 ГиБ;
- архив — ZIP64 (нативно открывается Windows).

Последовательность:

1. Собрать базовый образ (вариант 1).
2. Выполнить локальную доводку файла-образа: создать p3 `/malina` и разложить
   артефакты (этот вариант, режим файла-образа).
3. Проверить образ offline.
4. Архивировать образ в формате, нативно поддерживаемом Windows.
5. Передать архив в публикацию.

Расчёт размера и команды — `docs/image-size.md`.

### Практическая последовательность

Актуальные адреса, логин и пароль VPS — в приватном `.kilo/vps-release.md`
(в репозиторий не попадают). В командах ниже используются плейсхолдеры:
`<VPS_IP>`, `<VPS_USER>`, `<VPS_PASSWORD>`, `<VPS_PATH>`.

1. Собрать базовый образ (вариант 1) и ARM-бинарники:

```bash
image/build.sh
make -C web arm
for d in map-reader kes-reader bms-reader bms-proxy tsdb grid-reader; do
  make -C daemons/$d arm
done
```

2. Сделать копию базового образа и разложить p3 + артефакты текущего репозитория:

```bash
BASE=/opt/raspberry_tmp/pi-gen/deploy/2026-10-17-malina-lite.img
IMG=/opt/raspberry_tmp/pi-gen/deploy/2026-10-19-malina-release.img
sudo cp --sparse=always "$BASE" "$IMG"
sudo chown "$USER:$USER" "$IMG"   # deploy-local делает truncate от своего имени
MAL_P3_GB=3 deploy/local/deploy-local.sh "$IMG"
```

3. Проверить образ offline (скрипт печатает `[verify] ... ok`) и упаковать:

```bash
ls -l --block-size=1 "$IMG"       # ожидаемо 6 988 759 040
sfdisk -d "$IMG"                  # p1 512M, p2 3G, p3 3G
cd /opt/raspberry_tmp/pi-gen/deploy
sudo zip -9 malina-2026.10.19-rpi3-8gb.img.zip 2026-10-19-malina-release.img
sha256sum malina-2026.10.19-rpi3-8gb.img.zip \
  > malina-2026.10.19-rpi3-8gb.img.zip.sha256
```

4. Выложить на VPS, заменив предыдущую версию (на VPS ~2,5 ГБ свободно,
старые архивы не хранить):

```bash
export SSHPASS='<VPS_PASSWORD>'
sshpass -e scp malina-2026.10.19-rpi3-8gb.img.zip \
  malina-2026.10.19-rpi3-8gb.img.zip.sha256 \
  <VPS_USER>@<VPS_IP>:<VPS_PATH>/

sshpass -e ssh <VPS_USER>@<VPS_IP> \
  'cd <VPS_PATH> && sha256sum -c malina-2026.10.19-rpi3-8gb.img.zip.sha256; df -h /'
```

Так как имя файла фиксировано, новый архив заменяет старый. Если появились
файлы прошлых версий — удалить их и старые `.sha256` в `<VPS_PATH>`, затем
проверить `df -h /`. Nginx отдаёт только два exact-URL (архив и `.sha256`),
остальные пути — 404; при смене имени файла обновить
`/etc/nginx/sites-enabled/malina-download` и перезагрузить nginx.

5. Обновить публичный репозиторий `malina-26-public` (ветка `main`):
в `README.md` — версия, размер архива, SHA256 и ссылки на скачивание; при
изменении пользовательской документации — синхронизировать `manuals/`.
Затем закоммитить и запушить.

## Быстрый прогон

```
# Этап 1 уже собран: /opt/raspberry_tmp/pi-gen/deploy/<дата>-malina-lite.img
# Локальный деплой (заливка образа + p3 + раскладка + проверка):
deploy/local/deploy-local.sh /dev/sdX
```

---

## Обновления на развёрнутом устройстве (точечно)

`deploy/deploy.sh --first-run` и `deploy/local/deploy-local.sh` — **только для первичного
развёртывания** (полный
прогон `manifest.tsv`, компиляция `pamcheck` и т.п.). На уже работающем устройстве
так делать не нужно и не следует: обновляют **только изменившиеся артефакты**
(штатно — `deploy/deploy.sh` без флага).

При переходе на `ID = BASE` один раз удаляют **всю историю типа `bms`**
(старые `<BASE>-NNNNN` и возможные новые `<BASE>` — запускать до накопления новых):

```
deploy/deploy.sh --purge-bms-history --only bmsr
```

Флаг выключает `bms-reader` из автозапуска через `malina-rw` и маскирует его
runtime-маской (чтобы `Restart=on-failure` не поднял процесс), останавливает
`malina-web` (если активен) и обе tsdb, удаляет `bms` в avg и raw, затем поднимает
tsdb/raw, web (если unit установлен) и `bms-reader` с проверкой; маска снимается
перед общим рестартом. `tsdb` автоматически добавляется в `--only`,
чтобы обновить бинарник с `validKey=50`. После успеха создаётся маркер
`/malina/etc/malina/.bms-history-purged`; повторный запуск без удаления маркера
пропускает удаление, но выполняет деплой/рестарт и проверки. При abort до удаления
runtime-маска снимается, но reader остаётся disabled; при ошибке маркера маска
остаётся, reader disabled. Runtime-маска исчезает после ребута, `disable` сохраняется.
Обычный `--only bmsr` в таком состоянии завершится ошибкой и потребует повторить
`--purge-bms-history`; persistent mask снимается вручную
`systemctl unmask malina-bms-reader`. Это **разрушающая** операция.

`deploy/deploy.sh --only s1,s2,…` — **точечное обновление**: выкладывает и перезапускает
**только выбранные сервисы** (`web,netd,map,kes,bmsr,bmsp,tsdb,grid`). Для
`grid` конфиг ставится только при явном `--grid-config PATH`; без флага файл на
устройстве не меняется. **Без `--only`/`--all` обычный
деплой не запускается** (правило: на `.68` — только изменённые сервисы); `--all` — весь
скрипт; `netd` никогда не перезапускается; при `--first-run` `--only` игнорируется.
Полный деплой выкладывает бинарники (`malina-web`,
`malina-netd`, `map-reader`, `kes-reader`, `bms-reader`, `bms-proxy`, `malina-tsdb`, `grid-reader`) + веб-ассеты и обновляет
**unit-файлы сервисов** — `malina-tsdb`, `malina-tsdb-raw`, `map-reader`,
`kes-reader`, `malina-bms-reader`, `malina-bms-proxy`, `malina-grid-reader` (коротким rw-окном под `/etc`).
При `--only map,kes,bmsr,bmsp` также устанавливается
`system/udev/60-usb-serial-latency.rules` и его значение применяется к существующим
`/sys/bus/usb-serial/devices/ttyUSB*/latency_timer` (если атрибут есть).
Политика проекта — **всегда 1 мс**; deploy завершится ошибкой, если в правиле иное
значение. Локальные **drop-in**'ы
(`*.service.d/*.conf`, напр. `map-reader.service.d/sim.conf`) — отдельный каталог,
этим не затрагиваются. `malina-netd` не перезапускается (сетевой стек).

Пример — обновление веб-сервиса (типовой случай):

```
make -C web arm
scp web/dist/malina-web-armv7 malina@<ip>:/tmp/malina-web
scp system/systemd/malina-web.service malina@<ip>:/tmp/malina-web.service
ssh malina@<ip> 'sudo mount -o remount,rw / && \
  sudo install -m0755 /tmp/malina-web /malina/usr/local/bin/malina-web && \
  sudo install -m0644 /tmp/malina-web.service /etc/systemd/system/malina-web.service && \
  sudo cp -a /tmp/web-assets/* /malina/usr/local/share/malina/web/ && \
  sudo rm -rf /malina/usr/local/share/malina/manuals && sudo cp -a manuals/. /malina/usr/local/share/malina/manuals/ && \
  sudo systemctl daemon-reload && sudo systemctl restart malina-web && \
  sudo mount -o remount,ro /'
```

Для ассетов аналогично копируется содержимое `web/assets/{templates,static,i18n}` в
`/malina/usr/local/share/malina/web/`, а `manuals/` — в
`/malina/usr/local/share/malina/manuals/`. Root-файлы требуют rw-окна (`mount -o
remount,rw /` … `remount,ro`), каталог `/malina` — уже rw.

## Настройки reader-демонов (map/kes/bms-reader)

`map-reader.json`, `kes-reader.json` и `bms-reader.json` (поля `enabled`,
`log_level`) ставятся по `deploy/manifest.tsv` с политикой **`if_absent`** —
существующие на устройстве файлы **не перезаписываются**.

Дефолты в поставляемых файлах: **МАП — включён**, **КЭС — выключен**,
**БМС — выключен**. Поэтому на чистом образе КЭС и BMS-опрос стартуют
**выключенными**; включаются в веб-админке (МАП → Настройка, КЭС → Настройка,
БМС → Настройка → рамка «BMS-Reader») либо записью `enabled:true` в конфиг +
`reload` по control-сокету.

> На рабочем устройстве (`.68`) все три включены (требование «у нас»): выставьте
> `enabled:true` в `/malina/etc/malina/{map,kes}-reader.json` и в
> `/malina/etc/malina/bms-reader.json`, затем `systemctl reload`/команда
> `reload` по сокету.

> **Переименование конфига BMS-Receiver:** прежний `bms-gateways.json` → `bms-reader.json`.
> Демон и веб читают старое имя как **fallback**, поэтому шлюзы не теряются; запись
> идёт уже в `bms-reader.json`. При желании перенести заранее:
> `cp /malina/etc/malina/bms-gateways.json /malina/etc/malina/bms-reader.json`
> (только на существующем устройстве; на чистом ставится новый файл по манифесту).
