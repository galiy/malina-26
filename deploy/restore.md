# Восстановление системы из репозитория

> Актуальный порядок деплоя (Этап 1 — образ, Этап 2 — артефакты локально) —
> `deploy/local-deploy.md`. Ниже — модель и SSH-путь восстановления через
> `deploy/provision.sh`.

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
   - установка артефактов по `deploy/manifest.tsv` (`deploy/deploy.sh --first-run`);
   - бинарники `malina-web`, `malina-netd`, `malina-pamcheck`;
   - сетевые конфиги режимов, стартовый `active=mode1`.

## Что уже в репозитории
- `system/systemd/*` — юниты (web, dhclient, logrotate, net, hostapd, dnsmasq).
- `system/pam/malina-web`, `system/dhcp/99-malina-syslog`.
- `system/rsyslog/`, `system/logrotate/`, `system/tmpfiles/`, `system/journald/`,
  `system/timesyncd/` — конфиги.
- `system/bin/` — `malina-rw`, `malina-remote-syslog`.
- `system/udev/` — udev-правила (`60-usb-serial-latency.rules`: `latency_timer=1`
  для USB-serial — эхо-протокол МАП, см. `docs/map.md`).
- `system/network/` — дефолты `mode1.json`, `mode2.json`, `active`.
- `daemons/tsdb/` — сервис хранилища рядов телеметрии (`malina-tsdb`):
  append-only сегменты на `/malina/var/malina-ts`, двоичный API, без фоновых
  оптимизаций; юнит `tsdb.service` → `malina-tsdb.service` (`docs/telemetry-store.md`).
- `system/provision/` — снимок состояния: `fstab`, `cmdline.txt`, `config.txt`,
  `packages.txt` (apt-mark showmanual), `masked.txt`, `disabled.txt`.
- `image/stage-malina/05-map-uart/` — UART для МАП: `enable_uart=1` +
  `dtoverlay=miniuart-bt`, без serial-консоли (PL011 на GPIO14/15 — МАП, BT на
  mini-UART). Ранее дублировалось шагом 1b в архивном `deploy/provision.sh`.
  Описание — `docs/map.md`.
- `web/` — исходники сервиса (web+netd) и `pamcheck.c`.
- `deploy/manifest.tsv`, `deploy/deploy.sh`.

## Статус
Реализовано в `deploy/local/stage2.sh` + `deploy/manifest.tsv` (исторически —
`deploy/provision.sh`, ныне в `archive/`):
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

Пакеты проекта из репозиториев запекаются в образ (`image/stage-malina/00-packages`
+ `apt update/upgrade`); **кастомные артефакты (бинарники/юниты/ассеты) в образ не
запекаются** — устанавливаются из репозитория (`deploy/local/stage2.sh` /
`deploy/deploy.sh --first-run`) после загрузки. Тиражирование образов — вне рамок текущей задачи.

### Прочее
- `deploy/deploy.sh` требует `MALINA_PASS` (env) или `MALINA_PASS=…` в `.kilo/secrets`.

## Порядок восстановления (локально) и SSH-путь

**Основной путь — локальный:** Этап 1 (запись образа) + Этап 2
(`deploy/local/stage2.sh`) — см. `deploy/local-deploy.md`.

Ниже — **SSH-путь** (по желанию, для отладки) из гранулированных средств, не единым
«мегаскриптом». Исторический провижининг-скрипт перенесён в
`archive/…/deploy/provision.sh` (можно вернуть и запустить вручную либо
подглядывать как справку).

**0. Предусловия.** Образ загружен (`image/build.sh`), SSH включён
(`malina/malina-26`, root разрешён). Пароли на свежем образе — дефолтные.

**1. На ноутбуке — собрать артефакты под armhf** (бинарники в репо не коммитятся,
каталоги `*/dist/` в `.gitignore`):
```
make -C web arm                 # web/dist/malina-web-armv7, malina-netd-armv7
make -C daemons/map-reader arm  # daemons/map-reader/dist/map-reader-armv7
make -C daemons/kes-reader arm  # daemons/kes-reader/dist/kes-reader-armv7
make -C daemons/bms-reader arm  # daemons/bms-reader/dist/bms-reader-armv7
```

**2. Скопировать репозиторий (вместе с `dist/`) на устройство** в `/opt/malina-repo`:
```
rsync -a --delete --exclude .git ./ malina@<ip>:/opt/malina-repo/
```
(альтернатива — `git clone`, но тогда `dist/` нужно собрать/скопировать отдельно).

**3. SSH на устройство**, далее команды от root (например `sudo -i`):
```
ssh malina@<ip>
sudo -i
```

**4. Провижининг на устройстве (по желанию).** Ремоунтит `/` rw, создаёт раздел
`/malina` (p3) и ФС, применяет `deploy/manifest.tsv`, ставит бинарники, веб-ассеты,
PAM-хелпер, machine-id, симлинки, tmpfiles, включает юниты. Скрипт берётся из архива:
```
sudo mkdir -p /opt/malina-repo/deploy
sudo cp /opt/malina-repo/archive/*/deploy/provision.sh /opt/malina-repo/deploy/provision.sh
REPO=/opt/malina-repo /opt/malina-repo/deploy/provision.sh
```
(предпочтительно — вообще не использовать: артефакты ставить локально Этапом 2;
пакеты уже в образе).

> **Какие юниты включаются.** При первичном развёртывании (`deploy/deploy.sh
> --first-run`) включаются только базовые: `dhclient-eth0`, `malina-web`,
> `malina-net`, `malina-net-fallback.timer`, `malina-logrotate.timer`, `map-reader`,
> `kes-reader`, `bms-reader`, `malina-tsdb`, `malina-tsdb-raw`. Юниты режимов/сети
> (`malina-hostapd`, `malina-dnsmasq`, `malina-wpa`, `malina-net-wifi`,
> `malina-dhclient-wlan0`, `malina-net-monitor`, `malina-net-scan`,
> `malina-net-fallback.service`, `malina-logrotate.service`) включаются/выключаются
> **по режиму сети** (`malina-netd`) и здесь намеренно не трогаются.

**5. Пароли.** Сменить дефолтные на рабочие (как принято на устройстве):
```
passwd root ; passwd malina        # или задать согласованный пароль
```
(пароль SSH для `deploy/deploy.sh` — `MALINA_PASS` / `.kilo/secrets`, не коммитится).

**6. Идентификация роли: имя хоста и часовой пояс.** Хранятся в `/etc`
(`/etc/hostname`, `/etc/timezone`, симлинк `/etc/localtime`). Стартовые значения
кладёт провижининг (`system/provision/hostname`); далее правятся в UI —
«Система → Хост» (применяется через короткое rw-окно `malina-rw`).

**7. Перезагрузка.** Система поднимается в режиме 1 (AP+portal), поднимаются
`malina-web`, `map-reader`, `kes-reader`, сеть (`malina-netd`).

**8. Проверка после восстановления:**
```
systemctl is-active malina-web malina-netd map-reader kes-reader bms-reader rsyslog ssh cron
ip -br a                                  # режим AP/STA
curl -sI http://127.0.0.1/login | head -1 # web :80
ls -l /dev/shm/malina-map-state           # shm map-reader
ls -l /dev/shm/malina-kes-state           # shm kes-reader
ls -l /dev/shm/malina-bms-state           # shm bms-reader (список + телеметрия 107 Б)
findmnt /malina                           # rw-раздел
```
При необходимости — **первичная установка** артефактов из репо с ноутбука:
`deploy/deploy.sh --first-run <user@host>` (ставит бинарники/ассеты/manifest и
перезапускает сервисы); `--build` пересобирает arm-бинарники перед выкладкой.
**`--first-run` — только для первичного развёртывания**; на уже развёрнутом
устройстве обновления делают точечно (`deploy/deploy.sh` без флага; см.
`deploy/local-deploy.md`, «Обновления на развёрнутом устройстве»).

**9. Пакеты.** Набор из репозиториев, который нужен проекту, уже запечён в образ
(`image/stage-malina/00-packages`): `mc`, `rsyslog`, `iw`, `rfkill`, `hostapd`,
`dnsmasq`, `nftables`, `isc-dhcp-client`, `gcc`. Провижининг их не переустанавливает
(только при отсутствии).

## Логи
- Зеркалирование логов в `/malina/var/log` **отключено** (ОС стабильна, флэшку не
  изнашиваем; логи смотрим на `.253`). Ранее зеркало включалось на период отладки;
  конфиг `system/rsyslog/20-malina-copy.conf` и строка `/malina/var/log` в
  `system/tmpfiles/malina-rsyslog.conf` удалены, запись из `deploy/manifest.tsv`
  убрана.

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
  `/malina/etc/malina/remote-syslog`, `/malina/etc/malina/snmp-monitoring.json`
  (настройка периодической отправки SNMP-трапов «Мониторинг»).
  Имя хоста и часовой пояс хранятся **в `/etc`** (`/etc/hostname`, `/etc/timezone`,
  симлинк `/etc/localtime`), а не на `/malina`: применяются из UI через короткое
  rw-окно (`malina-rw run`: запись `/etc` → remount ro). В устойчивом состоянии
  записи в ro-корень нет.
- **Правило:** таймстампы/флаги/счётчики — в `/run` (переживать ребут не должны);
  на `/malina` — только явные настройки. Никакого состояния в репозитории.
