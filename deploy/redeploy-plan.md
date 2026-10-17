# Методика переноса ПАК на новую систему (Вариант B) — инструкции для агента

> Это **инструкции для агента**, а не готовый скрипт. Агент читает шаги
> `deploy/local/deploy-local.sh` и данные `deploy/manifest.tsv` и воспроизводит
> их на устройстве вручную, с контролем каждого шага. Генератора плана
> (`--plan`) нет — единственный источник истины это сам скрипт раскладки.
>
> **Конкретный хост в документе не зашит.** Какой хост перезаливать, кто
> раздатчик и какие пароли — **задаёт оператор** (см. §0). `.68`/`.9` по тексту —
> только пример.
> Перед разрушительными шагами (заливка) — явное подтверждение оператора.

---

## 0. Входные параметры (задаёт оператор)

Перед работами оператор **явно указывает**:
- `<TARGET_HOST>` — **какой хост перезаливать** (IP/имя);
- `<TARGET_USER>` — SSH-пользователь старого ПАК (обычно `root`);
- **пароль SSH к старой системе — ЗАПРОСИТЬ У ОПЕРАТОРА** (не искать в репо;
  `MALINA_PASS` в `.kilo/secrets` относится к `.60`, для других хостов не использовать);
- `<SENDER_HOST>` — хост-раздатчик, где лежат образ и репозиторий (обычно машина
  агента; в примере — `.9`);
- путь к образу `<IMG>` (самый свежий сборки, root 3 ГиБ).

**Не предполагать хост по умолчанию.** Если оператор не указал `<TARGET_HOST>` —
остановиться и запросить. Другие устройства (например, `.60`) без указания не трогать.

## Общая цель

Перевести указанный старый ПАК (пример: Raspbian 8 jessie + legacy-демоны
MicroArt) на голый образ (Raspberry Pi OS bookworm) и развернуть артефакты проекта.

## 1. Инвентаризация и остановка процессов на `<TARGET_HOST>` (B1)

**Цель:** исключить любые записи в разделы перед перезаливкой.
**Инвентарь снимать заново** для конкретного хоста; ниже — пример, снятый на `.68`.

Пример инвентаря `.68`:
- ОС Raspbian 8 jessie, ядро `4.14.50-v7+`; `eth0` = `192.168.13.68/24`.
- SD 7,4 ГиБ (8 ГиБ карта): `p1` boot 63M (ro), `p2` root 4G (ro), `p3`
  `/settings` 2G (rw), ~1,3 ГиБ неразмечено. Новый образ (p1 512M + p2 root
  3 ГиБ) влезает, p3 `/malina` ≈ 3,9 ГиБ.
- root/boot `ro`; `/db`, `/var/log`, `/tmp` — tmpfs; единственный rw-раздел SD —
  `/settings`.
- Писатели: `fake-hwclock`/`hwclock-save`; легаси `mapd`/`mpptd`/`batmond`/`dbed`/
  `mserver`/`mserverups`/`scd`; `kes-emu`, `bmslistener`; `nginx`+`php5-fpm`,
  `rsyslog`, `snmpd`, `ntp`; cron (`check_daemons.sh` каждую минуту поднимает демоны).

Порядок остановки (важен именно такой — **cron первым**; набор демонов сверять
с фактическим инвентарём хоста):
1. `crontab -r`; остановить `cron`.
2. Остановить `fake-hwclock`, `hwclock-save`.
3. Остановить прикладные systemd-сервисы: `bmslistener`, `kes-emu`, `nginx`,
   `php5-fpm`, `snmpd`, `ntp`, `rsyslog`, `syslog.socket`.
4. Убить легаси-демоны: `mapd`, `mpptd`, `batmond`, `dbed`, `mserver`,
   `mserverups`, `scd`, `check_daemons`.
5. **Оставить** `sshd` и сеть (`networking`, `netplug`, `systemd-networkd`,
   `dhclient`, `wpa_supplicant`), иначе теряется канал. `watchdog` не трогаем.

**Критерий готовности:** `lsof +D /settings` — пусто; `lsof /dev/mmcblk0*` —
пусто; `sync`; остались только инфраструктура + `sshd`; сеть up;
`/proc/meminfo`: `Dirty`/`Writeback` = 0 kB.

## 2. Проверка образа перед заливкой

- Выбрать **самый свежий** `*malina-lite.img`; **не** старый с root 8 ГиБ.
- `SIZE=$(stat -c %s <IMG>)`, `SHA=$(sha256sum <IMG> | awk '{print $1}')`.
- Loop-проверка (ro): `p1` boot + `p2` root **3 ГиБ**, p3 отсутствует; `SIZE`
  меньше объёма карты `<TARGET_HOST>` и ≈ 3,5 ГиБ. При несоответствии — стоп.

### 2.1. Копия образа с bootstrap cron-hook (вариант B)

Скопируй образ (`cp --reflink=auto <IMG> <OUT>`), loop-смонтируй p2 копии rw и
положи **ровно один** файл `/etc/cron.d/vb-bootstrap` (`root:root 0644`):

```
# ВРЕМЕННЫЙ bootstrap — удалить после входа!
@reboot root /bin/sh -c '/usr/sbin/ip link set eth0 up; /usr/sbin/ip addr add <TARGET_IP>/24 dev eth0 2>/dev/null || true'
```

- `<TARGET_IP>` — IP хоста до перезаливки (из §1).
- `cron.service` включён, `/etc/cron.d` лежит на rootfs; `/var/spool/cron` в этом
  образе tmpfs, поэтому файл кладём сюда.
- Оригинал `<IMG>` не менять; заливать копию `$OUT`, SHA — с копии.

## 3. Заливка образа — raw TCP в `/dev/mmcblk0` (B2)

Данные идут **не через ssh**, а отдельным raw-потоком; образ на целевом хосте не
сохраняется (места нет) — читается с `<SENDER_HOST>` и сразу пишется в устройство.

### 3.1. На `<SENDER_HOST>` (sender)
```
SHA=$(sha256sum <IMG> | awk '{print $1}')      # запомнить, подставить ниже
socat -u FILE:<IMG> TCP-LISTEN:9000,reuseaddr  # в фоне, отдаёт до EOF (1 соединение)
```
Для повтора — перезапустить `socat`.

### 3.2. Приём — фоновым процессом, без ребута (B2)

Заливка идёт ~5–7 мин (медленная карта — дольше). Инструмент агента по умолчанию
обрывает вызов через **2 мин**, поэтому нельзя держать приём в одном коротком
вызове: обрыв убьёт управляющий SSH/оболочку. Приём запускается **фоновым
процессом** (`background_process start`), который не ограничен этим таймаутом;
прогресс и результат — через `status`/`logs`.

Данные идут `socat → /dev/tcp` напрямую, но процессы `tee`/`dd` — дети
управляющей SSH-сессии, поэтому «независимыми» от неё они не являются: при
обрыве сессии могут получить SIGHUP. При обрыве — **не запускать второй writer**,
дождаться/убить `dd`/`tee`, убедиться, что запись остановлена, и повторять
**с offset 0** (карта частично перезаписана).

**Шаг A. Подготовка (короткий SSH-вызов, до фонового приёма):**
1. `echo 1 > /proc/sys/kernel/sysrq` — иначе ребут на шаге D не сработает.
2. `umount /settings && sync` (единственный rw-раздел; путь сверять с инвентарём).
   Предпроверка: `grep -E '^(Dirty|Writeback):' /proc/meminfo` → оба **0 kB**;
   `mount | grep mmcblk0` → только `ro`.
3. Прогреть в page cache `dd tee sha256sum sync sleep bash` (+ их библиотеки) —
   после перезаписи корня они должны уже быть в RAM.
4. Убедиться, что `SHA` (с `<SENDER_HOST>`) известен.

**Шаг B. Фоновый приём (`background_process start`, БЕЗ ребута):**
Команда на `<TARGET_HOST>` (один bash):
```
exec 3<>/dev/tcp/<SENDER_HOST>/9000
tee >(sha256sum > /dev/shm/rx.sha) <&3 \
  | dd of=/dev/mmcblk0 bs=4M iflag=fullblock conv=fsync 2> /dev/shm/dd.err
rc=${PIPESTATUS[1]}
sync
read -r rx _ < /dev/shm/rx.sha
if [ "$rc" = 0 ] && [ "$rx" = "<SHA>" ]; then
  echo "VERDICT=OK rc=$rc rx=$rx"
else
  echo "VERDICT=FAIL rc=$rc rx=$rx exp=<SHA>"
fi
```
- `tee` дублирует поток в `sha256sum`, в `dd` уходит тот же байт-поток.
- `rc`, хеш и `dd.err` — в `/dev/shm` (tmpfs): после `dd` старый корень уже
  перезаписан, писать в него и запускать оттуда бинарники ненадёжно.
- **Ребут здесь НЕ выполняется** — только печать вердикта (его заберёт
  `background_process logs`).

**Шаг C. Периодическая проверка процесса заливки:**
- `background_process status/logs` — жив ли процесс, появился ли `VERDICT`;
- на `<SENDER_HOST>` — завершился ли `socat` (отдал файл до EOF);
- на `<TARGET_HOST>` во время записи можно смотреть `pgrep -f 'dd of=/dev/mmcblk0'`
  и хвост `/dev/shm/dd.err` (пока `dd` идёт, старый userland ещё доступен).

**Шаг D. Проверка и завершение (отдельно от приёма):**
- Убедиться: процесс завершён; в логе `VERDICT=OK` (dd rc=0 и принятый хеш == `SHA`);
  `Dirty`/`Writeback` = 0 kB.
- После `dd` старая система может **умереть** — это нормально: корень уже перезаписан,
  и работающий старый userland не переживает подмену (наблюдалось на практике: сначала
  SSH-хендшейк сбрасывается, затем хост перестаёт пинговаться). Если `VERDICT=OK`, но
  хост не отвечает — **это не повод перезаливать**: карта уже записана, нужен
  **power-cycle** (кнопка/питание), после чего загрузится новый образ.
- Если хост ещё отвечает: `echo 1 > /proc/sys/kernel/sysrq; echo b > /proc/sysrq-trigger`.
- Если `VERDICT=FAIL` или вердикт не получен — **не перезагружать**: запись неполная.
  Повтор — **с offset 0** (в т.ч. физически: карту в картридер на `<SENDER_HOST>` и
  локальный `deploy-local.sh`). Второй writer не запускать.

**Критерий готовности:** `VERDICT=OK` в логе фонового процесса (+ `Dirty/Writeback=0`,
если хост ещё жив). Затем — sysrq-ребут или power-cycle; при `FAIL`/нет вердикта —
повторная заливка с offset 0.

## 4. Перезагрузка и вход в новый образ (B3)

- Если после `VERDICT=OK` хост не отвечает (старый userland умер при перезаписи) —
  это ожидаемо: сделай **power-cycle** (кнопка/питание) и дождись загрузки нового
  образа. Признак успеха — поднимается SSH уже нового образа.
- После ребута/power-cycle SSH-сессия старая рвётся. **Host key сменился** — удалить
  старую запись: `ssh-keygen -R <TARGET_HOST>` (или `-o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null`).
- Подключаться с ретраями, пока SSH не поднимется (до 2–3 мин).
- Вход: `<TARGET_USER>`/`malina`, пароль **`malina-26`**. **Новые пароли —
  запросить у оператора** и установить `passwd`.
- Проверить «голость»: нет сервисов `malina-*`; корень `ro`; `fdisk -l` — только
  `p1` (boot) + `p2` (root 3 ГиБ), p3 нет.

### 4.1. Удаление bootstrap cron-hook (сразу после входа)

Root в образе `ro` (`malina-rw` нет):

```
mount -o remount,rw /
rm -f /etc/cron.d/vb-bootstrap
mount -o remount,ro /
```

IP остаётся в текущем ядре; **до §5 не ребутиться**. После §5 сеть поднимает `netd`.

## 5. Создание p3 и раскладка (B4) — агент читает скрипт и ведёт шаги с контролем

**Никакого `--plan`/ранбука нет.** Источник истины:
1. **сам скрипт** `deploy/local/deploy-local.sh` — его шаги (p3, fstab, дерево,
   симлинки, machine-id, manifest, бинарники, ассеты, pamcheck, владельцы, юниты,
   purge NM, tmpfiles, проверка);
2. **`deploy/manifest.tsv`** — артефакты (`src → dst`, режим, `always|if_absent`);
3. `system/provision/{masked,disabled}.txt`, `system/provision/{machine-id,hostname}`.

**`deploy-local.sh` НА ЦЕЛЕВОМ ХОСТЕ НЕ ЗАПУСКАТЬ** — он для карты в картридере
(loop, `dd`, локальный mount, chroot+qemu). Агент только читает его шаги и
воспроизводит вручную: `ROOT=/`, `MAL=/malina`, без loop/dd, `pamcheck` — нативно.

### 5.1. Инструменты и rw-окно
- Проверить: `command -v sfdisk mkfs.ext4 gcc systemctl tar install`
  (если `sfdisk` нет — `parted`/`fdisk`).
- Открыть rw-окно корня (`ro`): `mount -o remount,rw /`; `mkdir -p /malina`
  (в голом образе каталога `/malina` нет). `malina-rw` использовать нельзя — он
  ставится в ходе раскладки (в `/malina`).

### 5.2. Раздел p3 (шаг 1 скрипта)
- Начало p3 = конец p2 (align 1 МиБ): `start`/`size` p2 из
  `/sys/class/block/mmcblk0p2/{start,size}`; `sfdisk --no-reread -N 3 /dev/mmcblk0`
  с `size=+`; `partprobe /dev/mmcblk0`; `udevadm settle`;
  `mkfs.ext4 -F -q -L malina /dev/mmcblk0p3`.
- **Контроль:** `blkid /dev/mmcblk0p3` → `TYPE="ext4"`, `LABEL="malina"`.

### 5.3. Доставка репозитория и бинарников (на целевом хосте их нет)
`rsync` в образе нет — тянуть `tar`-потоком с `<SENDER_HOST>` (после `mount /malina`):
```
# на <SENDER_HOST>:
tar --exclude=.git -C <repo> -czf - . \
  | ssh -o StrictHostKeyChecking=accept-new <TARGET_USER>@<TARGET_HOST> \
      'mkdir -p /malina/opt/malina-repo && tar -C /malina/opt/malina-repo -xzf -'
```
- Переносится **весь репозиторий + `*/dist/*-armv7`** (dist в gitignore — `git clone`
  недостаточно).
- **Контроль:** в `/malina/opt/malina-repo` есть `deploy/manifest.tsv`, `system/`,
  `web/assets/`, `web/pamcheck/pamcheck.c` и все `*/dist/*-armv7`.

### 5.4. Раскладка (шаги 2..15 скрипта, с контролем каждого)
Источник — `/malina/opt/malina-repo`; после каждого шага — проверка из скрипта:
- fstab: + `LABEL=malina /malina …` (ровно одна строка);
- дерево `/malina`, seed-сеть (`mode1.json`,`mode2.json`,`active`, 0600/0644);
- переносы `/root`,`/home`,cron,wpa + симлинки (проверить `readlink`);
- `machine-id`/`hostname` (rm+install, 0444/0644);
- артефакты по `manifest.tsv` (`/malina/*` → p3, прочее → `/`; `if_absent` не
  перезаписывать) — режим/владелец каждого;
- бинарники в `/malina/usr/local/{bin,sbin}` — 7/7, `file` → ELF 32-bit ARM;
- ассеты → `/malina/usr/local/share/malina/web` — файлов > 0, режимы;
- `pamcheck` нативно: `gcc -O2 -o /malina/usr/local/libexec/malina-pamcheck
  <repo>/web/pamcheck/pamcheck.c -ldl`; `file` → ELF 32-bit ARM;
- владельцы/режимы: числа uid/gid из `/etc`; **не** `chown -R` по `/etc`;
- юниты: `systemctl enable/mask/disable` (на живой системе — **без** `--root`),
  списки из `system/provision/`;
- purge NetworkManager (`apt-get purge -y network-manager … || true`); fstab-фиксы
  (`/var/lib/NetworkManager` убрать; `/var/lib/dhcp`, `/run/malina-ts-raw` 200M
  добавить; tmpfs-точки создать); `rm -rf /etc/NetworkManager`;
- `systemd-tmpfiles --create …` (best-effort).

### 5.5. Завершение
- `sync`; вернуть корень в `ro`: `mount -o remount,ro /`.
- **Reboot** — `/malina` и сервисы поднимутся при загрузке.

## 6. Пост-проверка (после ребута)

- `systemctl is-active malina-web malina-netd map-reader kes-reader bms-reader
  bms-proxy malina-tsdb rsyslog ssh cron` — рабочие; `findmnt /malina` — смонтирован.
- `ip -br a`, `nft list ruleset`, режим сети (AP/STA); `curl -sI http://127.0.0.1/login`.
- Включить/настроить reader-демоны (`enabled:true`); сменить пароли (по согласованию
  с оператором).
- Зафиксировать результат.

## Измерения канала (пример на `.68`, для оценки времени)
`128 МиБ` в `/dev/null` — 11,8 МБ/с; `1 ГиБ` в файл на ext4-SD — 8,8 МБ/с (SHA
совпал). Сырая запись в `/dev/mmcblk0` ожидаемо 8,8–11,8 МБ/с → образ ~3,5 ГиБ
за **~5–7 мин**.

---

## Замечания

- Хост, раздатчик и пароли задаёт оператор; в документе хостов «по умолчанию» нет.
- Другие устройства без явного указания не трогать.
- Готовых скриптов нет: команды — ориентир, агент выполняет и адаптирует.
- Всё, что меняется на целевом хосте, должно быть восстановимо из репо
  (`system/`, `deploy/manifest.tsv`, `deploy/local/deploy-local.sh`).
