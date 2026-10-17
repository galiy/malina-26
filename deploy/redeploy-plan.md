# Методика переноса ПАК на новую систему (Вариант B) — инструкции для агента

> Это **инструкции для агента**, а не готовый скрипт. Отдельного исполняемого
> скрипта нет: агент выполняет шаги вручную, формируя команды по контексту.
> Единый источник правды по раскладке — `deploy/local/deploy-local.sh --plan` и
> `deploy/manifest.tsv`.
>
> **Целевой ПАК — `.68`** (старый тест-стенд). Хост `.60` в этой методике
> **не трогаем**. Перед выполнением разрушительных шагов (заливка) — явное
> подтверждение оператора.

---

## Общая цель

Перевести старый ПАК `.68` (Raspbian 8 jessie + legacy-демоны MicroArt) на голый
образ (Raspberry Pi OS bookworm) и развернуть артефакты проекта.

## 0. Предусловия

- Собран голый образ (`image/build.sh`) → `*malina-lite.img` в
  `/opt/raspberry_tmp/pi-gen/deploy/`. Образ содержит только **p1 (boot) + p2
  (root 3 ГиБ)**; p3 `/malina` отсутствует.
- Собраны armhf-бинарники (`make -C web arm` и т.д. по `deploy/restore.md`).
- Ноутбук-раздатчик — **`192.168.13.9`** (br0), целевой ПАК — `192.168.13.68`.
- Пароль SSH по умолчанию в голом образе: `malina` и `root` — **`malina-26`**
  (root-вход разрешён). После входа — сменить.
- На `.68` из клиентов передачи есть `wget`/`curl`; **нет** `nc`/`tftp`/`bmaptool`.
  Поэтому заливка — **raw TCP** (`socat` на `.9` → bash `/dev/tcp` на `.68`),
  без SSH-потока, с контролем целостности.

## 1. Инвентаризация и остановка процессов на `.68` (B1)

**Цель:** исключить любые записи в разделы перед перезаливкой.

Фактическая картина `.68` (снято):
- ОС Raspbian 8 jessie, ядро `4.14.50-v7+`; `eth0` = `192.168.13.68/24`.
- **SD 7,4 ГиБ (8 ГиБ карта)**: старая разметка `p1` boot 63M (ro), `p2` root 4G
  (ro), `p3` `/settings` 2G (rw), ~1,3 ГиБ неразмечено. Новый образ (p1 512M +
  p2 root 3 ГиБ) влезает, p3 `/malina` получится ~3,9 ГиБ.
- root/boot — `ro`; `/db` — tmpfs (в RAM); `/var/log`,`/tmp` — tmpfs.
  Единственный rw-раздел SD — `/settings`.
- Писатели: `fake-hwclock`/`hwclock-save`; легаси `mapd`/`mpptd`/`batmond`/`dbed`/
  `mserver`/`mserverups`/`scd` (пишут `/settings/logs`); `kes-emu`, `bmslistener`;
  `nginx`+`php5-fpm`, `rsyslog`, `snmpd`, `ntp`; cron (`check_daemons.sh` каждую
  минуту перезапускает демоны).

Порядок остановки (важен именно такой — **cron первым**):
1. `crontab -r`; остановить `cron`.
2. Остановить `fake-hwclock`, `hwclock-save` (последние писатели `/settings`).
3. Остановить прикладные systemd-сервисы: `bmslistener`, `kes-emu`, `nginx`,
   `php5-fpm`, `snmpd`, `ntp`, `rsyslog`, `syslog.socket`.
4. Убить легаси-демоны: `mapd`, `mpptd`, `batmond`, `dbed`, `mserver`,
   `mserverups`, `scd`, `check_daemons`.
5. **Оставить** `sshd` и сеть (`networking`, `netplug`, `systemd-networkd`,
   `dhclient`, `wpa_supplicant`), иначе теряется канал. `watchdog` не трогаем.

**Критерий готовности:** `lsof +D /settings` — пусто; `lsof /dev/mmcblk0*` —
пусто; `lsof` по блочным устройствам не показывает писателей; `sync` выполнен;
остались только инфраструктура + `sshd`; `eth0` up.

## 2. Проверка образа перед заливкой

- Снять `SIZE=$(stat -c %s <img>)` и `SHA=$(sha256sum <img> | …)`.
- Убедиться, что разметка — `p1` (boot) + `p2` (root 3 ГиБ), без p3, и `SIZE`
  меньше объёма карты (7,4 ГиБ).

## 3. Заливка образа — raw TCP в `/dev/mmcblk0` (B2)

Данные идут **не через ssh**, а отдельным raw-потоком; образ на `.68` не
сохраняется (места нет) — читается с `.9` и сразу пишется в устройство.

### 3.1. На `.9` (sender)
- Поднять раздатчик ровно на время заливки:
  `socat -u FILE:<img> TCP-LISTEN:9000,reuseaddr`
  (отдаёт файл до EOF на одно соединение; для повтора — перезапустить).
- `SHA` и `SIZE` известны заранее (шаг 2).

### 3.2. На `.68` (receiver) — один bash-сеанс
Ключевые моменты:
- Данные принимаются через bash `/dev/tcp` (не файл): `exec 3<>/dev/tcp/192.168.13.9/9000`.
- Поток подаётся на `dd of=/dev/mmcblk0` **блоками 4 МиБ** с `iflag=fullblock`
  (обязательно: пайп/сокет даёт короткие чтения) и `conv=fsync`; `status=progress`.
- **Целостность — на лету:** `tee` дублирует поток в `sha256sum`, основной поток
  уходит в `dd` без изменений. Любой обрыв/недосыл → хеш не совпадёт.
- **Перед dd:** `umount /settings && sync` — единственный rw-раздел, гасим
  writeback журнала ext4 (`commit=60`), иначе он может записать «хвост» поверх
  свежего образа.
- **После dd:** старый корень уже перезаписан — нельзя запускать не заранее
  подгруженные бинарники. Поэтому: утилиты (`dd`, `tee`, `sha256sum`, `sleep`,
  `sync`) прогреть в page cache ДО заливки; сравнение хеша — builtin/bash
  (`read`), ребут — через `/proc/sysrq-trigger` (не читает диск).

Ориентир команд (агент подставляет `<img>`/`SHA`, выполняет вручную):
```
# предзагрузка утилит/библиотек в RAM до перезаписи корня
# umount /settings; sync
exec 3<>/dev/tcp/192.168.13.9/9000
tee >(sha256sum > /tmp/rx.sha) <&3 \
  | dd of=/dev/mmcblk0 bs=4M iflag=fullblock conv=fsync status=progress
rc=${PIPESTATUS[1]}          # код dd
sync; sleep 2
read -r rx _ < /tmp/rx.sha   # bash builtin
if [ "$rc" = 0 ] && [ "$rx" = "$SHA" ]; then
  echo VERIFY_OK
  echo b > /proc/sysrq-trigger      # жёсткий ребут, без чтения диска
else
  echo "VERIFY_FAIL rx=$rx exp=$SHA rc=$rc — НЕ перезагружаем"
fi
```

**Обоснование размера блока:** `bs=4M` — мало syscall, крупные выровненные
записи для SD, буфер 4 МиБ (при 927 МБ RAM не проблема). Узкое место — сеть
100 Мбит (~11,8 МБ/с), поэтому 8–16 МиБ выигрыша не дают. `oflag=direct` не
используется (на SD медленнее, требует жёсткого выравнивания).

**Критерий готовности:** `VERIFY_OK` (dd rc=0 И принятый хеш == `SHA`), после
чего выполняется sysrq-ребут. При `VERIFY_FAIL` — **не перезагружать**, сообщить
оператору (повторить заливку, перезапустив `socat`).

**Остаточный риск:** заливается блок-устройство работающей системы. Снимается
тем, что root `ro`, писатели остановлены (B1), `/settings` размонтирован,
а ребут форсируется сразу после проверки. Не прерывать заливку/проверку.

## 4. Перезагрузка и вход в систему (B3)

- После sysrq-ребута `.68` грузится с нового образа (bookworm), p3 ещё нет.
- Вход по SSH: `malina`/`root`, пароль **`malina-26`**; сменить пароли.
- Проверить: `systemctl is-active` — нет сервисов `malina-*`; `mount` — корень
  `ro`; `fdisk -l` — только `p1` (boot) + `p2` (root 3 ГиБ).

## 5. Создание p3 и раскладка (B4) — вручную, источник шагов динамический

- Получить актуальную последовательность: `deploy/local/deploy-local.sh --plan <target>`
  (печатает шаги: разметка p3, дерево `/malina`, бинарники, ассеты, симлинки,
  `manifest.tsv`, юниты, pamcheck, purge NM, проверка) и свериться с
  `deploy/manifest.tsv`.
- Воспроизвести эти шаги **на `.68`** (через SSH), адаптируя пути под устройство
  (карта внутри `.68`, а не в картридере). Порядок — как в `--plan`:
  1. p3: начало от конца p2, `size=+` (весь остаток) → `mkfs.ext4 -L malina`.
  2. Точка `/malina` в rootfs; fstab `LABEL=malina`.
  3. Дерево `/malina` + seed-конфиги сети (`mode1.json`,`mode2.json`,`active`).
  4. Симлинки `/root`,`/home`,`/etc/wpa_supplicant` → `/malina/*`;
     `/etc/resolv.conf`; dbus machine-id.
  5. `machine-id`,`hostname` (свежие inode: rm+install).
  6. Артефакты по `deploy/manifest.tsv` (`always`/`if_absent`; `/malina/*` на p3).
  7. Бинарники (armhf) в `/malina/usr/local/{bin,sbin}`.
  8. Веб-ассеты → `/malina/usr/local/share/malina/web`.
  9. `malina-pamcheck` (chroot+qemu, gcc из образа).
  10. Владельцы/режимы (числовые uid/gid из образа; НЕ `chown -R` по `/etc`).
  11. `systemctl --root` enable базовых юнитов + mask/disable по
      `system/provision/{masked,disabled}.txt`.
  12. Purge NetworkManager; фиксы fstab (`/var/lib/dhcp`, `/run/malina-ts-raw`
      200M, убрать NM-tmpfs); tmpfs-точки; `rm -rf /etc/NetworkManager`.
  13. tmpfiles; `sync`; offline-проверка (как в `deploy-local.sh`).

**Критерий готовности:** `systemctl is-active malina-web malina-netd map-reader
kes-reader bms-reader bms-proxy malina-tsdb rsyslog ssh cron` — рабочие;
`/malina` смонтирован; веб отвечает; корень `ro`.

## 6. Пост-проверка

- `ip -br a`, `nft list ruleset`, режим сети (AP/STA).
- Включить/настроить reader-демоны (`enabled:true`).
- Сменить дефолтные пароли.
- Зафиксировать результат в документации.

---

## Замечания

- `.60` в этой методике не затрагивается.
- Готовых скриптов нет: команды выше — ориентир, агент выполняет и адаптирует.
- Всё, что меняется на `.68` в ходе раскладки, должно быть восстановимо из репо
  (`system/`, `deploy/manifest.tsv`, `deploy/local/deploy-local.sh`).
