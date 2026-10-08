# ПАК «Микроарт-Малина» — репозиторий проекта

Рефакторинг ПАК «Малина» (Raspberry Pi 3): перевод на современную ОС
(Debian 12 bookworm, armhf) с **immutable-корнем**, воспроизводимая сборка образа,
сетевая подсистема в двух режимах и веб-админка на Go.

## Документация

**Пользовательская** (для эксплуатации устройства) — в [docs/](docs/):
- [docs/README.md](docs/README.md) — быстрое начало, доступы по умолчанию;
- [docs/ui.md](docs/ui.md) — интерфейс: вход, доступы, SSH, точка доступа, меню и индекс страниц;
- [docs/pages/](docs/pages/) — страницы по отдельности: info, system, network, services, logs.

**Проектная** (разработка и сопровождение):
- [requirements.md](requirements.md) — требования и статус реализации;
- [deploy/restore.md](deploy/restore.md) — восстановление из образа + репо;
- [image/README.md](image/README.md) — сборка образа (pi-gen);
- [web/README.md](web/README.md) — веб-сервисы (`malina-web`, `malina-netd`).

## Структура репозитория

- `image/` — сборка образа (pi-gen, стейдж `stage-malina`).
- `system/` — артефакты системы: systemd-юниты, конфиги, seed-конфиги сети,
  snapshot провижининга (`fstab`, `cmdline.txt`, `config.txt`, списки пакетов/масок).
- `deploy/` — установка/провижининг на устройство (`manifest.tsv`, `install.sh`,
  `provision.sh`) и `restore.md`.
- `web/` — Go-сервисы (`cmd/malina-web`, `cmd/malina-netd`, `internal/*`),
  статика/шаблоны/i18n, PAM-хелпер.
- `tests/` — автотесты (UI smoke на Playwright).
- `docs/` — пользовательская документация.

## Что реализовано (кратко)

- **ОС/образ**: Raspberry Pi OS Lite (bookworm), root `ro`, tmpfs
  (`/tmp`,`/var/log`,`/var/tmp`,`/var/spool`,`/var/lib/*`), rw-раздел `/malina`
  (перенос `/root`,`/home`,cron,wpa; фиксированный `machine-id`).
- **watchdog**: systemd `RuntimeWatchdogSec=15s` (`bcm2835_wdt`).
- **Сеть**: режим 1 «Точка доступа» (AP+NAT+portal) и режим 2 «Wi-Fi клиент»
  (STA+NAT, раздача на `eth0`); защита от чужого DHCP, монитор, фолбэк, watchdog AP.
- **Веб-админка**: вход по PAM, страницы «Информация», «Параметры», «Настройка сети»,
  «Сервисы» (вкладки, start/stop), «Логи», страница ожидания перезагрузки; i18n
  ru/be/zh/en (по умолчанию en); тема оформления, локальные шрифты.
- **Наблюдаемость**: rsyslog + пересылка на удалённый syslog (DHCP опция 7 или
  ручной хост).

## Сборка и установка (кратко)

```
# образ
image/build.sh

# веб-сервисы (ARMv7) и установка на устройство
make -C web arm
MALINA_PASS=<пароль> deploy/install.sh <user@host>       # + --build для сборки на месте

# провижининг раздела /malina, артефактов, сети
deploy/provision.sh
```

## Доступы по умолчанию

- Веб/SSH: пользователь `malina`, пароль `malina26`; root (SSH) — `malina26`.
- Точка доступа Wi-Fi: SSID `Malina`, пароль `malina123`.

Подробности — в [docs/README.md](docs/README.md).

## Примечания

- Приватные данные (`.kilo/`, `hardware.md`, `AGENTS.md`) не коммитятся.
- Деплой образа/тираж — вне рамок; восстановление на устройстве — по
  [deploy/restore.md](deploy/restore.md).
