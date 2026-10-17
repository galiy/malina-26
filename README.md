# ПАК «Микроарт-Малина» — репозиторий проекта

Рефакторинг ПАК «Малина» (Raspberry Pi 3): перевод на современную ОС
(Debian 12 bookworm, armhf) с **immutable-корнем**, воспроизводимая сборка образа,
сетевая подсистема в двух режимах и веб-админка на Go.

## Документация

**Пользовательская** (для эксплуатации устройства) — в [manuals/](manuals/):
- [manuals/README.md](manuals/README.md) — быстрое начало, доступы по умолчанию;
- [manuals/ui.md](manuals/ui.md) — интерфейс: вход, доступы, SSH, точка доступа, меню и индекс страниц;
- [manuals/pages/](manuals/pages/) — страницы по отдельности: info, system, network, services, logs.

**Проектная** (разработка и сопровождение):
- [requirements.md](requirements.md) — требования и статус реализации;
- [deploy/restore.md](deploy/restore.md) — восстановление из образа + репо;
- [image/README.md](image/README.md) — сборка образа (pi-gen);
- [web/README.md](web/README.md) — веб-сервисы (`malina-web`, `malina-netd`);
- [docs/](docs/) — техническая документация (МАП, карта памяти и т.п.).

## Структура репозитория

- `image/` — сборка образа (pi-gen, стейдж `stage-malina`).
- `system/` — артефакты системы: systemd-юниты, конфиги, seed-конфиги сети,
  snapshot провижининга (`fstab`, `cmdline.txt`, `config.txt`, списки пакетов/масок).
- `deploy/` — установка/провижининг: `manifest.tsv`, единый `deploy.sh`
  (обновление / `--first-run` — первичное), локальный деплой
  `local/deploy-local.sh` (перезаливка + p3 + раскладка + проверка),
  методика переноса ПАК по сети `redeploy-plan.md`
  (Вариант B, инструкции для агента), `local-deploy.md`, `restore.md`.
  Устаревшие скрипты — в `archive/` (вне git).
- `web/` — Go-сервисы (`cmd/malina-web`, `cmd/malina-netd`, `internal/*`),
  статика/шаблоны/i18n, PAM-хелпер.
- `tests/` — автотесты: UI smoke (Playwright, `run-smoke.js`) и API e2e
  (`run-api-e2e.js`: авторизация cookie/Bearer, токены, запись, смена пароля).
- `manuals/` — пользовательская документация.
- `docs/` — техническая документация (МАП, карта памяти и т.п.).

## Что реализовано (кратко)

- **ОС/образ**: Raspberry Pi OS Lite (bookworm), root `ro`, tmpfs
  (`/tmp`,`/var/log`,`/var/tmp`,`/var/spool`,`/var/lib/*`), rw-раздел `/malina`
  (перенос `/root`,`/home`,cron,wpa; фиксированный `machine-id`).
- **watchdog**: systemd `RuntimeWatchdogSec=15s` (`bcm2835_wdt`).
- **Сеть**: режим 1 «Точка доступа» (AP+NAT+portal) и режим 2 «Wi-Fi клиент»
  (STA+NAT, раздача на `eth0`); защита от чужого DHCP, монитор, фолбэк, watchdog AP.
- **Веб-админка**: вход по PAM, страницы «Информация», «Параметры», «Настройка сети»,
  «Сервисы» (вкладки, start/stop), «Логи», страница ожидания перезагрузки; i18n
  ru/be/zh/en/ka/kk/uz/tg (по умолчанию en); тема оформления, локальные шрифты.
- **Аудит действий**: JSON-события web (вход, токены, смена пароля/настроек/сети,
  запись в МАП, службы) в syslog с отдельным тегом → на log-хосте отдельный файл;
  секреты маскируются (см. `docs/api-auth.md`).
- **Наблюдаемость**: rsyslog + пересылка на удалённый syslog (DHCP опция 7 или
  ручной хост).
- **Raw-хранилище телеметрии**: второй инстанс `malina-tsdb` на tmpfs
  `/run/malina-ts-raw` (200 МБ, retention 48 ч, row-append) — детальное сырьё для
  графиков (см. `docs/telemetry-store.md` §14).
- **Хранилище рядов телеметрии**: демон `daemons/tsdb` (`malina-tsdb`) —
  append-only сегменты на `/malina` (ключ: тип+id+время, версия контракта,
  двоичный API, без фоновых оптимизаций), доступ локально по Unix-сокету (без
  сети; см. `docs/telemetry-store.md`).

## Сборка и установка (кратко)

```
# образ
image/build.sh

# веб-сервисы (ARMv7)
make -C web arm

# ОБНОВЛЕНИЕ работающего устройства (по умолчанию):
MALINA_PASS=<пароль> deploy/deploy.sh --build <user@host>

# ПЕРВИЧНОЕ развёртывание — только один раз, на голое устройство/образ:
MALINA_PASS=<пароль> deploy/deploy.sh --build --first-run <user@host>

# локальный деплой на SD (перезаливка образа + p3 + раскладка + проверка):
deploy/local/deploy-local.sh /dev/sdX
# перенос ПАК по сети (методика для агента, без скрипта) — deploy/redeploy-plan.md
```

## Доступы по умолчанию

- Веб/SSH: пользователь `malina`, пароль `malina-26`; root (SSH) — `malina-26`.
- Точка доступа Wi-Fi: SSID `Malina`, пароль `malina-26`.

Подробности — в [manuals/README.md](manuals/README.md).

## Примечания

- Приватные данные (`.kilo/`, `hardware.md`, `AGENTS.md`) не коммитятся.
- Деплой образа/тираж — вне рамок; восстановление на устройстве — по
  [deploy/restore.md](deploy/restore.md).
