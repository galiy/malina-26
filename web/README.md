# Веб-сервисы «Малина» (`malina-web`, `malina-netd`)

Две программы на Go:

- **`malina-web`** — веб-админка (UI + HTTP API), работает напрямую по HTTP
  (TLS не используется). Аутентификация — по паролю Linux-пользователя `malina`
  через PAM (хелпер `malina-pamcheck`).
- **`malina-netd`** — сетевая подсистема: применение режимов сети, hostapd/dnsmasq/
  wpa/dhclient, NAT, монитор чужого DHCP, фолбэк, watchdog точки доступа.

Пользовательская документация — в [../manuals/](../manuals/).

## Страницы UI

- `/` — **главная**: живая сводка по всем устройствам (МАП + КЭС),
  `web/internal/web/mainlist.go` + `home.js` (см. `docs/mainpage.md`).
- `/info` — **Информация о системе** (карточки: Хост+Сеть+Питание, Процессор+Память,
  Диски, Сеть, Сбойные юниты, Подключённые устройства, Процессы; ошибки внешних команд).
- `/system` — **Параметры системы**: имя хоста, логирование (свой syslog-хост или
  DHCP опция 7), смена пароля.
- `/network` — **Настройка сети**: два режима (Точка доступа / Wi-Fi клиент),
  валидация, сохранение с применением (перезапуск сети).
- `/services` — **Сервисы**: вкладки «МАЛИНА» (start/stop) и «Системные» (просмотр),
  комментарии-категории, кнопка «Перезагрузить малину».
- `/logs?file=…` — **Логи**: whitelist `/var/log`, автообновление/история.
- `/stats/charts` — **Статистика → Графики**: закладки устройств (обе tsdb), наборы
  метрик, исторические ряды (сшив `avg`+`raw`, приоритет `raw`; глубина raw — по объёму),
  Chart.js локально, рендер — `web/assets/static/js/stats_charts.js` (`docs/ui-stat.md` §3).
- `/stats/tables` — **Статистика → Таблицы**: закладки устройств (обе tsdb), выбор
  периода, постраничная таблица по контракту; значения — в **человеческих единицах**
  (`web/internal/web/units.go`, эталон — панель 1 `map.js`), единица — второй строкой
  заголовка; реле — 3 колонки, КЭС `Mode` — символ, у BMS «Ячеек» вместо версии
  (`docs/ui-stat.md`; рендер — `web/assets/static/js/stats.js`).
- `/map`, `/kes` — **Мониторинг** устройства: закладки **Панель**, **Информация**
  (основные/системные), **Управление**. Live — polling `/api/{map,kes}/raw` (1 с).
  МАП-панель — `map.js`; КЭС-панель (по мотивам легаси-монитора КЭС: панели/АКБ/выход,
  температуры, энергия, реле, MPPT) — `kes.js`.
- `/login` — вход по паролю.
- POST `/system/reboot` → страница ожидания перезагрузки (прогресс 2 мин + опрос каждые 5 c).

Меню: **ДОМОЙ**, **МАП**, **КЭС**, **БМС**, **Сетевые**, **Статистика**
(Графики/Таблицы), **Мастер-узел**, **Система**
(Информация/Параметры/Сеть/Сервисы/E-Mail/События), **Выход**.

Подпункты устройств обновляются `nav.js` опросом
`/api/{map,kes,bms}/devices`: сразу при загрузке, первые 30 с — каждые 5 с,
далее — каждые 30 с, плюс при возврате фокуса/вкладки. Ссылки обязаны быть
корневыми абсолютными (`/bms`, `/map`, `/kes`), а не зависеть от текущего
пути. Частота, стоимость, кеш и регрессионные требования —
`docs/ui-navigation.md`.

## API

Авторизация API — **cookie-сессия `malina_session` ИЛИ `Authorization: Bearer <token>`**
(см. `docs/api-auth.md`). Сессии/токены — в `/dev/shm/malina-auth` (переживают
перезапуск web, idle-TTL 1 ч со скользящим продлением). Пути по типу устройства:
`/api/<type>/...` (`map` — МАП; будущие `kes` и др.).

- `POST /api/token` `{password}` → `{token, expires}`; `DELETE /api/token` — отзыв.
- `GET/POST /stats/settings` — «Статистика → Настройки»: резервное копирование raw/avg (`malina-arch`), тест target, восстановление/сброс архива.
- `GET /api/grid/status` — список сетевых инверторов: `id, gen, serial, addr, kind, status, last_seen, name, placement, logger_sn`; `logger_sn` — настроенный SN логгера (uint32, decimal), не ключ хранения; `0` — не задано (нет конфига или shm от старой конфигурации).
- `GET /api/map/devices` — список устройств типа «map» (JSON `[{type,id,gen,label}]`, для меню).
- `GET /api/map/raw?id=<id>&gen=<gen>` — бинарный контракт МАП (`application/octet-stream`);
  **`gen` обязателен** (id+gen должны совпасть, иначе 404). `gen` — timestamp аренды слота.
- `POST /api/map/write` — запись в МАП (тело — `docs/map-write-api.md`; проксирует
  в control-сокет `map-reader`; HTTP-статус по коду: 200/400/404/422/502/500).
- `GET /api/stats/devices` — список устройств обеих tsdb (JSON `{devices:[{type,id}]}`;
  из памяти tsdb, без чтения сегментов; порядок МАП(+BMS)→КЭС→BMS-сироты, `docs/ui-stat.md`).
- `GET /api/stats/extremes?type=&id=` — крайние точки времени устройства по обеим tsdb
  (`{raw:{found,min,max}, avg:{...}}`; из памяти tsdb).
- `GET /api/stats/series?type=&id=&from=&to=&fields=` — ряды графиков (одним запросом;
  клиент передаёт `fields` — только поля активных наборов + поля для `calcSeries`;
  пусто `fields` → все поля). Ответ `{series:{"<key>":[[t,v],…]}}`; серверное
  прореживание `bucket` (ближайшая к границе окна; ~≤1000 точек для широких окон, узкие — без лимита):
  backend считает `bucket_ms = (to − from)/1000`; порог `min` (raw 1.75 с; avg
  `PeriodMin × 1.5`) — если `bucket_ms < min`, `bucket` не передаётся (tsdb отдаёт все
  строки); источник — сшив `avg`+`raw` (avg всегда, raw по фактической глубине, приоритет
  `raw`); JSON стримится (`docs/ui-stat.md` §3.3/§3.6).
- `GET /api/stats/table?type=&id=&from=&to=&dir=from|to&limit=&detail=&rawmin=` — страница
  таблицы (≤200 строк; обе tsdb, приоритет raw в его зоне; `detail=1` — raw+avg,
  иначе только avg). Ответ: `columns:[{id,name,type,unit}]` (id строковый; битфилды
  реле развёрнуты в `Relay1/2/3`) и `rows:[{t,v,f}]` со значениями **в человеческих
  единицах** (строки — символы, напр. КЭС `Mode`); `docs/ui-stat.md`.
- `GET /api/kes/devices` — список устройств типа «kes» (JSON `[{type,id,gen,label}]`).
- `GET /api/kes/raw?id=<id>&gen=<gen>` — бинарный payload слота КЭС
  (`application/octet-stream`; `gen` обязателен, id+gen должны совпасть, иначе 404).
- `POST /api/kes/write` — запись в КЭС (тело — как у control-сокета `kes-reader`,
  `docs/kes-reader.md` §4.2.3; проксирует в сокет, HTTP-статус по коду:
  200/400/404/422/502/500; аудит `kes_write`).
- `GET /api/mainlist` — сводка по всем устройствам (МАП + КЭС) для главной
  страницы `/`: готовые к отрисовке плитки (вся агрегация/расчёты на сервере,
  клиент не тянет бинарные контракты). Формат и формулы — `docs/mainpage.md`;
  реализация — `web/internal/web/mainlist.go`.
- `POST /write_eeprom.php` — **legacy-запись** (`data=[[addr,val],…]`, десятичные;
  `addr==0` — команда, иначе — байт; авторизация **Basic+PAM**; исполняется в цикле
  legacy-модуля, сразу перечитывает и обновляет `read_json`/`read_memory`; ответ `Ok`/`error`).
- `GET /read_json.php?device=map`, `GET /read_memory.php`, `GET /check_daemons.php` —
  legacy-чтение (**без авторизации**).

Полное описание legacy API — `docs/legacy/legacy-api.md`.
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

- Языки: Русский, Беларуская, 中文, English, ქართული, Қазақша, O'zbekcha, Тоҷикӣ (порядок нативный); **дефолт — English**.
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
- Юниты/PAM/конфиги — из `system/` по `deploy/manifest.tsv` (**первичное**
  развёртывание: `deploy/local/stage2.sh` локально; `deploy/deploy.sh --first-run` по SSH).
- **Обновления** уже развёрнутого устройства — точечно (бинарник + юнит + ассеты +
  restart), `deploy.sh --first-run` для этого не используется (см. `deploy/local-deploy.md`).
- Установка требует `MALINA_PASS` (env) или строки `MALINA_PASS=…` в `.kilo/secrets`.

## Флаги / переменные

- `-listen` (`MALINA_WEB_LISTEN`), по умолчанию `:8080` (на устройстве юнит
  задаёт `-listen :80`).
- `-user` (`MALINA_WEB_USER`), по умолчанию `malina`.
- `-pam-helper` (`MALINA_WEB_PAM_HELPER`), `-assets` (`MALINA_WEB_ASSETS`).
- `-auth-shm` (`MALINA_WEB_AUTH_SHM`), по умолчанию `/dev/shm/malina-auth`.
- `-session-idle` — скользящий TTL сессий/токенов (по умолчанию `1h`).
- `-control-sock` (`MALINA_WEB_CONTROL_SOCK`), по умолчанию `/run/malina-map/control.sock`.
- `-audit` (`MALINA_WEB_AUDIT=0` — выключить), `-audit-tag` (`MALINA_WEB_AUDIT_TAG`,
  по умолчанию `malina-web-audit`) — аудит-журнал действий: JSON-события в syslog
  (`/dev/log`), на log-хосте — отдельный файл по тегу (см. `docs/api-auth.md` §8).
- `MALINA_WEB_DEV_PASSWORD` — только для разработки.

Сервис обслуживает **корень** своего хоста; базового пути нет. Публикация наружу —
отдельный hostname на TLS-прокси (см. `docs/`/`deploy/`), проксируется в корень
устройства.

## Безопасность

Секреты/приватные данные в репозиторий не попадают (`.kilo/` игнорируется); скан
перед коммитом обязателен (см. правила проекта).
