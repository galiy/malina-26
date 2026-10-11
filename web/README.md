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

Авторизация API — **cookie-сессия `malina_session` ИЛИ `Authorization: Bearer <token>`**
(см. `docs/api-auth.md`). Сессии/токены — в `/dev/shm/malina-auth` (переживают
перезапуск web, idle-TTL 1 ч со скользящим продлением). Пути по типу устройства:
`/api/<type>/...` (`map` — МАП; будущие `kes` и др.).

- `POST /api/token` `{password}` → `{token, expires}`; `DELETE /api/token` — отзыв.
- `GET /api/map/devices` — список устройств типа «map» (JSON `[{type,id,gen,label}]`, для меню).
- `GET /api/map/raw?id=<id>&gen=<gen>` — бинарный контракт МАП (`application/octet-stream`);
  **`gen` обязателен** (id+gen должны совпасть, иначе 404). `gen` — timestamp аренды слота.
- `POST /api/map/write` — запись в МАП (тело — `docs/map-write-api.md`; проксирует
  в control-сокет `map-reader`; HTTP-статус по коду: 200/400/404/422/502/500).
- `POST /write_eeprom.php` — **legacy-запись** (`data=[[addr,val],…]`, десятичные;
  `addr==0` — команда, иначе — байт; авторизация **Basic+PAM**; исполняется в цикле
  legacy-модуля, сразу перечитывает и обновляет `read_json`/`read_memory`; ответ `Ok`/`error`).
- `GET /read_json.php?device=map`, `GET /read_memory.php`, `GET /check_daemons.php` —
  legacy-чтение (**без авторизации**).

Полное описание legacy API — `docs/legacy-api.md`.
- `GET /api/system` — системные данные (JSON). Список процессов **не входит**.
- `GET /api/processes` — снимок процессов (по запросу; не автообновляется).
- `GET /api/services` — состояние управляемых служб; `POST /services/action` — start/stop.
- `GET /api/log?file=<name>&lines=100&offset=<n>` — хвост/история лога (whitelist).
- `POST /system/reboot` — перезагрузка; `POST /logout`; `GET /lang?set=..&next=..`.
- `POST /system/map-time-sync` (`enabled=1|0`) — вкл/выкл синхронизацию времени МАП
  (флаг-файл `/malina/etc/malina/map-time-sync.off`; саму синхронизацию делает `map-reader`).

### Вкладка «Управление» (`/map`)

Запись в МАП из браузера: команды (ON/OFF, Charge, реле, Reset) и группы параметров
(таблица: имя+«?», текущее значение, редактор, единица, Сохранить/Отменить; сверху
«Сохранить всё»/«Отменить всё»). Пишет через `POST /api/map/write`; текущие значения
берёт из штатного raw-пула страницы. Данные параметров — `static/control-data.js`,
генерируется `web/tools/gen_control.py` из `~/src/mapsettings/catalog.json` (+легаси-набор);
логика — `static/control.js`. Требования — `docs/map-write-ui.md`.

## Подсистема `malina-netd`

- Режимы: `mode1` (AP+NAT+portal), `mode2` (STA+NAT, раздача на `eth0`).
  Опция `mode1.wifi_off_on_eth` («Выключить Wi-Fi»): при `carrier(eth0)=1` гасит
  AP/радио (`hostapd`+`dnsmasq`+`rfkill block wifi`), при пропаже линка —
  поднимает. Сторожевой цикл — `malina-netd wifi-watch` (юнит
  `malina-net-wifi.service`, стартует/останавливается из `applyMode1`).
  При погашенном Wi‑Fi watchdog и idle‑перезапуск hostapd пропускаются (иначе
  hostapd падал бы в цикл из-за `Restart=on-failure` при выключенном радио).
  В `mode2` `wlan0` уходит в чужую сеть как STA: AP и наш DHCP (`dnsmasq`)
  **останавливаются**, с `wlan0` снимается адрес AP (10.10.100.254), чистятся
  `dnsmasq.conf`/`hostapd.conf` — чтобы не раздавать `10.10.100.x` в аплинк.
  Раздача (если включена) — только на `eth0`, с защитой от чужого DHCP.
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
- `internal/auth` — PAM/dev аутентификатор; `internal/authstore` — сессии/токены
  в `/dev/shm/malina-auth` (сессии, токены, скользящий TTL).
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

- `-listen` (`MALINA_WEB_LISTEN`), по умолчанию `:8080` (на устройстве юнит
  задаёт `-listen :80`).
- `-user` (`MALINA_WEB_USER`), по умолчанию `malina`.
- `-pam-helper` (`MALINA_WEB_PAM_HELPER`), `-assets` (`MALINA_WEB_ASSETS`).
- `-auth-shm` (`MALINA_WEB_AUTH_SHM`), по умолчанию `/dev/shm/malina-auth`.
- `-session-idle` — скользящий TTL сессий/токенов (по умолчанию `1h`).
- `-control-sock` (`MALINA_WEB_CONTROL_SOCK`), по умолчанию `/run/malina-map/control.sock`.
- `-base` (`MALINA_WEB_BASE`) — базовый путь монтирования (напр. `/malina`), **задавать,
  если TLS-слой кладёт сайт в подпапку**. Пусто — корень. Все ссылки/редиректы/`fetch`
  строятся с этим префиксом; входящие с префиксом срезаются (если прокси не срезает),
  без префикса — принимаются. Авто-вычисления базы нет — только фиксированное значение.
- `MALINA_WEB_DEV_PASSWORD` — только для разработки.

## Безопасность

Секреты/приватные данные в репозиторий не попадают (`.kilo/` игнорируется); скан
перед коммитом обязателен (см. правила проекта).
