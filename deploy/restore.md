# Восстановление системы из репозитория

Правило проекта: **«голый стартовый образ + содержимое репозитория = полностью
рабочая система»**. Всё, что меняется на устройстве, должно быть восстановимо из
репо (без ручных «на память» шагов).

## Модель (двухслойная)

1. **Образ (`image/`)** — сборка pi-gen (Lite armhf + `stage-malina`).
   В образ должны «запекаться» артефакты из `system/`:
   - юнит-файлы, PAM-сервис, dhclient-хук, helper-скрипты;
   - rsyslog/logrotate/tmpfiles/journald/timesyncd drop-in'ы;
   - fstab (ro-корень, tmpfs, `/boot/firmware`), `cmdline.txt` (ro),
     `config.txt`;
   - маскирование и отключение лишних юнитов/таймеров;
   - фиксированный `/etc/machine-id`, симлинки (`/root`,`/home` на `/malina`,
     `/etc/wpa_supplicant`, `/etc/resolv.conf`), пакеты;
   - пакеты: `rsyslog`, `iw`, `rfkill`, `hostapd`, `dnsmasq`, `nftables`,
     `isc-dhcp-client`, `gcc` (для сборки pamcheck) и т.п.
2. **Доводка на устройстве (`deploy/`)** — после прошивки:
   - создание раздела `/malina` (p3) и его ФС/монтирование;
   - установка артефактов по `deploy/manifest.tsv` (`deploy/install.sh`);
   - бинарники `malina-web`, `malina-netd`, `malina-pamcheck`;
   - сетевые конфиги режимов, стартовый `active=mode1`.

## Что уже в репозитории
- `system/systemd/*` — юниты (web, hostname, dhclient, logrotate, net, hostapd, dnsmasq).
- `system/pam/malina-web`, `system/dhcp/99-malina-syslog`.
- `system/rsyslog/`, `system/logrotate/`, `system/tmpfiles/`, `system/journald/`,
  `system/timesyncd/` — конфиги.
- `system/bin/` — `malina-rw`, `malina-remote-syslog`.
- `system/network/` — дефолты `mode1.json`, `mode2.json`, `active`.
- `system/provision/` — снимок состояния: `fstab`, `cmdline.txt`, `config.txt`,
  `packages.txt` (apt-mark showmanual), `masked.txt`, `disabled.txt`.
- `web/` — исходники сервиса (web+netd) и `pamcheck.c`.
- `deploy/manifest.tsv`, `deploy/install.sh`.

## Статус
Реализовано в `deploy/provision.sh` + `deploy/manifest.tsv`:
- **создание раздела `/malina` (p3)** и монтирование (форматирование — только для
  нового/пустого раздела, чтобы не затереть данные);
- применение `system/*` по `deploy/manifest.tsv` (юниты, PAM, dhclient-хук, rsyslog/
  logrotate/tmpfiles/journald/timesyncd, helpers, watchdog.conf);
- симлинки `/root`,`/home`→`/malina`, `/etc/wpa_supplicant`→`/malina`,
  `/etc/resolv.conf`→`/run/malina`;
- удаление `NetworkManager` и остатков; установка пакетов (`nftables`, `hostapd`,
  `dnsmasq`, `isc-dhcp-client`, `rsyslog`, `gcc` и др.);
- фиксированный `machine-id`, seed-конфиги сети, установка бинарников
  `malina-web`/`malina-netd`/`malina-pamcheck` и **веб-ассетов**
  (`web/assets` → `/malina/usr/local/share/malina/web`).

Осталось (вне рамок текущей задачи): «запекание» всех этих шагов в `image/stage-malina`
(сейчас применяется `deploy/provision.sh` после прошивки), тираж образов.

### Прочее
- `deploy/install.sh` требует `MALINA_PASS` (env) или `MALINA_PASS=…` в `.kilo/secrets`.

## Порядок восстановления (целевой)
1. Прошить стартовый образ.
2. `deploy/provision.sh` — разделы `/malina`, fstab, симлинки, пакеты, маски,
   machine-id, установка `system/*` и бинарников.
3. Ребут → система поднимается в режиме 1 (AP).

## Временное (на период работ; удалить в конце)
- **Зеркало логов в `/malina/var/log` без ротации** — включено на **весь период
  работы над проектом** (облегчает отладку). Файл растёт.
  Файлы: `system/rsyslog/20-malina-copy.conf`, строка каталога в
  `system/tmpfiles/malina-rsyslog.conf`. **Удалить оба + запись из
  `deploy/manifest.tsv` по завершении проекта.**

## Состояние и хранилище (конвенция)

- **Runtime/transient — всегда `/run/malina-net/` (tmpfs, теряется при ребуте):**
  `mode` (override фолбэка), `hostapd.conf`, `dnsmasq.conf`,
  `wpa_supplicant.conf`, `ruleset.nft`, `dnsmasq.leases`, `serving` (маркер
  «мы раздаём DHCP»), `scan.json`, `monitor.state` (состояние монитора:
  ts/foreign/serving/clean_count).
  Прочее transient: `/run/dhcp-option7`, `/run/dhcp-option7-applied`,
  `/run/rsyslog-remote/remote.conf`, `/run/malina/resolv.conf`,
  `/run/logrotate.status`.
- **Persistent — на rw `/malina`:** `/malina/etc/malina/network/{active,mode1.json,mode2.json}`,
  `/malina/etc/malina/remote-syslog`, `/malina/etc/hostname`,
  `/malina/var/log/malina.log` (временно).
- **Правило:** таймстампы/флаги/счётчики — в `/run` (переживать ребут не должны);
  на `/malina` — только явные настройки. Никакого состояния в репозитории.
