# Восстановление системы из репозитория — модель, состояние и конвенции

> Это **справочник** (модель восстановимости и правила состояния), а не процедура.
> Порядок действий:
> - локальная перезаливка + раскладка — `deploy/local-deploy.md`
>   (`deploy/local/deploy-local.sh`);
> - перенос работающего ПАК по сети — `deploy/redeploy-plan.md` (Вариант B);
> - обновление/перезапуск сервисов на развёрнутом ПАК — `deploy/deploy.sh`.

Правило проекта: **«голый стартовый образ + содержимое репозитория = полностью
рабочая система»**. Всё, что меняется на устройстве, должно быть восстановимо из
репо — без ручных «на память» шагов.

## Модель (двухслойная)

1. **Образ (`image/`)** — сборка pi-gen (Lite armhf + `stage-malina`). В образ
   «запекаются» системные артефакты и конфиги:
   - юнит-файлы, PAM-сервис, dhclient-хук, helper-скрипты;
   - rsyslog/logrotate/tmpfiles/journald/timesyncd drop-in'ы;
   - fstab (ro-корень, tmpfs, `/boot/firmware`), `cmdline.txt` (ro), `config.txt`;
   - маскирование/отключение лишних юнитов и таймеров;
   - фиксированный `/etc/machine-id`, симлинки (`/root`,`/home`→`/malina`,
     `/etc/wpa_supplicant`, `/etc/resolv.conf`);
   - пакеты из репозиториев: `rsyslog`, `iw`, `rfkill`, `hostapd`, `dnsmasq`,
     `nftables`, `isc-dhcp-client`, `gcc` (сборка pamcheck) и т.п.;
   - разделы: `p1` boot + `p2` root 3 ГиБ; `p3` `/malina` в образ **не входит**.
2. **Доводка на устройстве (`deploy/`)** — после прошивки:
   - создание раздела `/malina` (p3) и его ФС/монтирование;
   - установка артефактов по `deploy/manifest.tsv`;
   - бинарники `malina-web`, `malina-netd`, `map/kes/bms-reader`, `bms-proxy`,
     `malina-tsdb`, `malina-pamcheck`;
   - веб-ассеты, seed-конфиги сети, симлинки, включение юнитов.
   Кастомные артефакты (бинарники/юниты/ассеты) **в образ не запекаются** —
   ставятся из репо (`deploy/local/deploy-local.sh` / `deploy/deploy.sh --first-run`).

## Что где лежит (репо → устройство)

- `system/systemd/*` — юниты (web, dhclient, logrotate, net, hostapd, dnsmasq,
  tsdb/tsdb-raw, readers).
- `system/pam/malina-web`, `system/dhcp/99-malina-syslog`.
- `system/rsyslog/`, `system/logrotate/`, `system/tmpfiles/`, `system/journald/`,
  `system/timesyncd/` — конфиги.
- `system/bin/` — `malina-rw`, `malina-remote-syslog`.
- `system/udev/` — `60-usb-serial-latency.rules` (`latency_timer=1` — политика,
  не менять; эхо-протокол МАП, см. `docs/map.md`).
- `system/network/` — дефолты `mode1.json`, `mode2.json`, `active`.
- `system/provision/` — **снимок состояния** (не источник истины): `fstab`,
  `cmdline.txt`, `config.txt`, `packages.txt`, `masked.txt`, `disabled.txt`,
  `machine-id`, `hostname`. `deploy-local.sh` использует только
  `masked/disabled/machine-id/hostname`; `fstab`/`cmdline.txt`/`config.txt` —
  справочные, их PARTUUID может не совпадать с текущим образом.
- `daemons/tsdb/` — хранилище рядов телеметрии (`malina-tsdb`): append-only
  сегменты на `/malina/var/malina-ts`, двоичный API, без фоновых оптимизаций
  (`docs/telemetry-store.md`).
- `image/stage-malina/05-map-uart/` — UART для МАП (`enable_uart=1` +
  `dtoverlay=miniuart-bt`, PL011 на GPIO14/15, BT на mini-UART); см. `docs/map.md`.
- `web/` — исходники сервиса (web+netd) и `pamcheck.c`.
- `deploy/manifest.tsv` — манифест установки артефактов; `deploy/deploy.sh`.

## Какие юниты включаются при первичном развёртывании

`deploy/deploy.sh --first-run` включает только базовые: `dhclient-eth0`,
`malina-web`, `malina-net`, `malina-net-fallback.timer`, `malina-logrotate.timer`,
`map-reader`, `kes-reader`, `bms-reader`, `bms-proxy`, `malina-tsdb`,
`malina-tsdb-raw`.

Юниты режимов/сети (`malina-hostapd`, `malina-dnsmasq`, `malina-wpa`,
`malina-net-wifi`, `malina-dhclient-wlan0`, `malina-net-monitor`,
`malina-net-scan`, `malina-net-fallback.service`, `malina-logrotate.service`)
включаются/выключаются **по режиму сети** (`malina-netd`) и здесь намеренно
не трогаются.

## Проверка после восстановления

```
systemctl is-active malina-web malina-netd map-reader kes-reader bms-reader bms-proxy malina-tsdb rsyslog ssh cron
ip -br a                                   # режим AP/STA
curl -sI http://127.0.0.1/login | head -1  # web :80
ls -l /dev/shm/malina-map-state            # shm map-reader
ls -l /dev/shm/malina-kes-state            # shm kes-reader
ls -l /dev/shm/malina-bms-state            # shm bms-reader (список + телеметрия); источник bms-proxy
findmnt /malina                            # rw-раздел
```

Обновления на уже развёрнутом ПАК — только точечно (`deploy/deploy.sh`;
см. «Обновления на развёрнутом устройстве» в `deploy/local-deploy.md`).

## Логи

Зеркалирование логов в `/malina/var/log` **отключено** (ОС стабильна, флэшку не
изнашиваем; логи смотрим на `.253`). Ранее зеркало включалось на период отладки;
конфиг `system/rsyslog/20-malina-copy.conf` и строка `/malina/var/log` в
`system/tmpfiles/malina-rsyslog.conf` удалены, запись в `deploy/manifest.tsv`
убрана.

## Состояние и хранилище (конвенция)

- **Runtime/transient — всегда `/run/malina-net/` (tmpfs, теряется при ребуте):**
  `mode` (override фолбэка), `hostapd.conf`, `dnsmasq.conf`, `wpa_supplicant.conf`,
  `ruleset.nft`, `dnsmasq.leases`, `serving` (маркер «мы раздаём DHCP»),
  `scan.json`, `monitor.state` (ts/foreign/serving/clean_count).
  Прочее transient: `/run/dhcp-option7`, `/run/dhcp-option7-applied`,
  `/run/rsyslog-remote/remote.conf`, `/run/malina/resolv.conf`,
  `/run/logrotate.status`.
- **Persistent — на rw `/malina`:** `/malina/etc/malina/network/{active,mode1.json,mode2.json}`,
  `/malina/etc/malina/remote-syslog`, `/malina/etc/malina/snmp-monitoring.json`.
  Имя хоста и часовой пояс — **в `/etc`** (`/etc/hostname`, `/etc/timezone`,
  симлинк `/etc/localtime`), не на `/malina`: применяются из UI через короткое
  rw-окно (`malina-rw run`: запись `/etc` → remount ro). В устойчивом состоянии
  записи в ro-корень нет.
- **Правило:** таймстампы/флаги/счётчики — в `/run` (переживать ребут не должны);
  на `/malina` — только явные настройки. Никакого состояния в репозитории.
