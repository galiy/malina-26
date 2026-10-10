# Веб-сервисы «Малина» (`malina-web`, `malina-netd`)

Две программы на Go:

- **`malina-web`** — веб-админка (UI + HTTP API), работает напрямую по HTTP
  (TLS не используется). Аутентификация — по паролю Linux-пользователя `malina`
  через PAM (хелпер `malina-pamcheck`).
- **`malina-netd`** — сетевая подсистема: применение режимов сети, hostapd/dnsmasq/
  wpa/dhclient, NAT, монитор чужого DHCP, фолбэк, watchdog точки доступа.

Пользовательская документация — в [../manuals/](../manuals/).

## Страницы UI

- `/` — **Информация о системе** (карточки: Хост+Сеть+Питание, Процессор+Память,
  Диски, Сеть, Сбойные юниты, Подключённые устройства, Процессы; ошибки внешних команд).
- `/system` — **Параметры системы**: имя хоста, логирование (свой syslog-хост или
  DHCP опция 7), смена пароля.
- `/network` — **Настройка сети**: два режима (Точка доступа / Wi-Fi клиент),
  валидация, сохранение с применением (перезапуск сети).
- `/services` — **Сервисы**: вкладки «МАЛИНА» (start/stop) и «Системные» (просмотр),
  комментарии-категории, кнопка «Перезагрузить малину».
- `/logs?file=…` — **Логи**: whitelist `/var/log`, автообновление/история.
- `/login` — вход по паролю.
- POST `/system/reboot` → страница ожидания перезагрузки (прогресс 2 мин + опрос каждые 5 c).

Меню: **Мониторинг** (МАП/КЭС — задел), **Статистика** (Графики/Агрегаты — задел),
**Система** (Информация/Параметры/Сеть/Сервисы), **Выход**.

## API

- `GET /api/system` — системные данные (JSON). Список процессов **не входит**.
- `GET /api/processes` — снимок процессов (по запросу; не автообновляется).
- `GET /api/services` — состояние управляемых служб; `POST /services/action` — start/stop.
- `GET /api/log?file=<name>&lines=100&offset=<n>` — хвост/история лога (whitelist).
- `POST /system/reboot` — перезагрузка; `POST /logout`; `GET /lang?set=..&next=..`.

## Подсистема `malina-netd`

- Режимы: `mode1` (AP+NAT+portal), `mode2` (STA+NAT, раздача на `eth0`).
- Команды: `status`, `apply`, `fallback-tick` (монитор/фолбэк по таймеру).
- Конфиги: `/malina/etc/malina/network/{active,mode1.json,mode2.json,fallback.json}`.
- Рабочие (tmpfs): `/run/malina-net/{hostapd.conf,dnsmasq.conf,mode,monitor.state,dnsmasq.leases}`.
- Управляется юнитами из `system/systemd/` (`malina-net`, `malina-hostapd`,
  `malina-dnsmasq`, `malina-wpa`, `malina-net-monitor`, `malina-net-fallback.timer` и др.).

## i18n

- Языки: Русский, Беларуская, 中文, English (порядок нативный); **дефолт — English**.
- Источник строк — русский; словари в `assets/i18n/*.json` (паритет ключей).
- Смена языка — селектор в шапке (`GET /lang`).

## Внешние команды

Все внешние команды на «Информации» выполняются через обёртку `run()` с лимитом
**1 c** (зависшие процессы убиваются). Сбои/таймауты собираются и показываются
карточкой «Ошибки внешних команд».

## Сборка

```
make host        # локально (web + netd для dev)
make arm         # ARMv7 static: dist/malina-web-armv7, dist/malina-netd-armv7
make netd-host   # только netd локально
make pamcheck    # C-хелпер
make fmt vet
```

## Dev-запуск (без PAM)

```
MALINA_WEB_DEV_PASSWORD=secret MALINA_WEB_LISTEN=:8080 go run ./cmd/malina-web
```

## Структура

- `cmd/malina-web`, `cmd/malina-netd` — точки входа.
- `internal/web` — сервер, роутинг, шаблоны/статика/i18n (внешние assets).
- `internal/auth` — PAM/dev аутентификатор; `internal/session` — сессии.
- `internal/i18n` — словари; `internal/netcfg` — конфиги режимов.
- `internal/sysinfo` — системные данные, процессы, обёртка `run()`.
- `internal/sysctl` — службы systemd (список/статус/start-stop) и др.
- `internal/logtail` — чтение логов (с конца, без полного чтения).
- `pamcheck/` — C-хелпер (dlopen libpam).
- `assets/{templates,static,i18n}` — UI (внешние, не встраиваются в бинарник).

## Деплой на устройство

- Бинарники: `/malina/usr/local/bin/malina-web`, `/malina/usr/local/sbin/malina-netd`.
- Статика: `/malina/usr/local/share/malina/web` (флаг `-assets`).
- Хелперы: `/malina/usr/local/libexec/malina-pamcheck`, `…/malina-remote-syslog`,
  `/malina/usr/local/sbin/malina-rw`.
- Юниты/PAM/конфиги — из `system/` по `deploy/manifest.tsv` (установка:
  `deploy/local/stage2.sh` локально; `deploy/install.sh` по SSH).
- Установка требует `MALINA_PASS` (env) или строки `MALINA_PASS=…` в `.kilo/secrets`.

## Флаги / переменные

- `-listen` (`MALINA_WEB_LISTEN`), по умолчанию `:80`.
- `-user` (`MALINA_WEB_USER`), по умолчанию `malina`.
- `-pam-helper` (`MALINA_WEB_PAM_HELPER`), `-assets` (`MALINA_WEB_ASSETS`).
- `MALINA_WEB_DEV_PASSWORD` — только для разработки.

## Безопасность

Секреты/приватные данные в репозиторий не попадают (`.kilo/` игнорируется); скан
перед коммитом обязателен (см. правила проекта).
