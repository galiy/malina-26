# Авторизация API (зафиксированное решение)

Статус: **реализовано** (web: `internal/authstore` + middleware `cookie || Bearer`,
эндпоинты `/api/token`, `/api/map/{devices,raw,write}`, legacy `/write_eeprom.php`
с Basic+PAM; см. `web/README.md`).
TLS — **внешний слой**, нам не принадлежит и не учитывается.

## 1. Матрица эндпоинтов

Путь группируется по **типу устройства**: `/api/<type>/...` (`map` — МАП;
в будущем `kes` — КЭС и т.д.). Сейчас тип один — `map`.

| Эндпоинт | Тип | Авторизация |
|---|---|---|
| `/read_json.php`, `/read_memory.php`, `/check_daemons.php` | legacy чтение | **без авторизации** (как сейчас) |
| `/read_json.php?device=mppt`, `/read_sec.php` | legacy чтение КЭС | **без авторизации** (как map-чтение) |
| `/read_json.php?device=bat` | legacy чтение (эмуляция batmon) | **без авторизации** (как map-чтение; см. `docs/legacy/batmon-api.md`) |
| `/write_eeprom.php`, `/write_sec.php` | legacy запись | **HTTP Basic** (пароль → PAM, кэш 5 мин) |
| `/api/map/devices` | чтение (тип map) | cookie-сессия **или** `Bearer` |
| `/api/map/raw` | чтение (тип map) | cookie-сессия **или** `Bearer` |
| `/api/map/write` | запись (тип map) | cookie-сессия **или** `Bearer` |
| веб-страницы (`/`, `/map`, `/system`, …) | UI | cookie-сессия |

Единый **скоуп** (без ролей): любой валидный токен = полный доступ.

Переименование **жёсткое** (алиасы не оставляем): `/api/devices` →
`/api/map/devices`; web-клиент (`nav.js`, `map.js`) правится в том же изменении.

Legacy-запись `write_eeprom` **реализована**: `POST data=[[addr,val],…]`
(десятичные), `addr==0` — команда, иначе — запись байта; исполняется в основном
цикле legacy-модуля (см. `web/README.md`), ответ — текст `Ok`/`error`.
**Rate-limit/lockout не делаем** (согласовано).

Контракт legacy-API (чтение и запись) — `docs/legacy-api.md`.

## 2. Веб-сессия и токены — общий механизм

- Один и тот же хранилищный слой для cookie-сессий и API-токенов.
- Аутентификация одна: **PAM по паролю** (пользователь `malina`, единственный).
- `POST /api/token` `{password}` → PAM → `{token, expires}`; в дальнейшем
  `Authorization: Bearer <token>`.
- Cookie `malina_session`: `HttpOnly`, `SameSite=Lax`; `MaxAge=1h`,
  **обновляется на каждом ответе** (чтобы активный клиент не терял вход).
- `DELETE /api/token` (с `Bearer`) — отзыв своего токена; `/logout` — удаление
  cookie-записи.

## 3. Скользящий TTL (idle 1 час)

- Срок жизни — **1 час с момента последнего использования**, не с выпуска.
- При каждом успешном использовании (любой авторизованный запрос) обновляем
  `lastUsed = now`; валидность: `now − lastUsed < 1h`.
- Активный web-клиент (пулит `/api/map/raw` ~1/с) и API-клиент **никогда** не
  перелогиниваются; простаивающие — истекают и перелогиниваются.
- Абсолютного «hard max» нет.

## 4. Хранилище — сегмент в tmpfs

- POSIX shm: **`/dev/shm/malina-auth`** (mode 0600, root). Живёт **перезапуск
  сервиса**, умирает при **ребуте** (tmpfs) — приемлемо.
- Раскладка: `[заголовок][слот 0]…[слот N−1]`; **N = 4096** (~256 КБ).
- Слот: `state, tokHash[32] (sha256 токена), created, lastUsed` (user не храним —
  он один). Хранится **хеш**, не сам токен.
- Операции: `Issue`, `Check+Touch`, `Revoke`, `PurgeExpired`, `PurgeAll`.
- Конкурентность: мьютекс в процессе + **seqlock** (как в `map-reader`).
- При переполнении — вытеснение oldest-idle; периодический reaper удаляет
  простаивающие >1 ч.

## 5. Basic + PAM: кэш успешных проверок (5 минут)

> Реализовано (`internal/auth.BasicCache`), используется legacy `write_eeprom.php`.

- Legacy `write_eeprom` — Basic: пароль → PAM (**пароль в открытом виде, не хеш**).
- Чтобы не дёргать PAM каждый запрос: **in-memory LRU-кэш успешных проверок**
  `sha256(user:pass) → validUntil` (TTL **5 мин**), размер 32.
- Кэшируются **только успешные** проверки; неверные всегда идут в PAM.
- Кэш инвалидируется при смене пароля и теряется при рестарте (это ок).
- Rate-limit/lockout **не делаем** (см. §1).

## 6. Смена пароля

- При успешной смене пароля (`/system/password`):
  - `PurgeAll` сегмента `/dev/shm/malina-auth` (все сессии и токены),
  - очистка PAM-кэша (5-мин).
- Клиенты перелогиниваются.

## 7. Ошибки

- Нет/невалидный/протухший токен → **401** с
  `WWW-Authenticate: Bearer realm="malina", error="invalid_token"`.
- Для HTML-страниц (cookie) → редирект на `/login`; для API → 401 JSON.
- (403 зарезервирован на будущее, если появятся роли.)

## 8. Безопасность/прочее

- TLS — снаружи; канал до TLS-сервера считается доверенным.
- CSRF для cookie-записей из браузера: `SameSite=Lax` + (при необходимости)
  нестандартный заголовок/CSRF-токен; `Bearer` от CSRF свободен.
- Rate-limit на `/login` и `/api/token` — **не делаем** (согласовано).
- UI управления токенами **не делаем**.

### Аудит-журнал (реализовано)

- Модуль `internal/audit`: событие = один JSON-объект (`ts`, `event`, поля),
  пишется в syslog через `/dev/log` с отдельным тегом
  (`-audit-tag`, по умолчанию `malina-web-audit`; `-audit` / `MALINA_WEB_AUDIT=0` — выкл.).
- На внешнем log-хосте (`.253`) используется **существующая универсальная** раскладка
  rsyslog (разбор по `%PROGRAMNAME%`): наши события попадают в **отдельный файл**
  `…/malina-<ip>/<category>/malina-web-audit.log`. Доп. настроек ни на log-хосте, ни
  в системном логировании устройства **не требуется** (используется общий форвард «всего»
  syslog и её же шаблон). Формат строки — syslog-префикс + JSON-тело.
- События: `login` (ok/fail), `logout`, `token_issue` (ok/fail), `token_revoke`,
  `password_change`, `session_expire` (с `idle_since` = `lastUsed`), `system_settings`
  (host/logging/maptime), `network_settings` (все параметры m1/m2), `service_action`,
  `reboot`, `map_write`, `eeprom_write`.
- Секреты не логируются: значения «секретных» ключей (pass/secret/token/cookie/
  authorization) заменяются на `***`; токен — только 8-hex префикс `sha256`; клиентский
  IP — из `X-Forwarded-For`.
- Legacy-чтение остаётся открытым.

## 9. Порядок внедрения

1. Хранилище `/dev/shm/malina-auth` (слоты, seqlock) + инициализация.
2. Middleware `cookie || Bearer` + скользящий `Touch`.
3. `POST/DELETE /api/token`, `/logout`.
4. **`/api/map/devices`** (переименование с `/api/devices`) и **`/api/map/raw`**
   под новый middleware; **переработка web-клиента** (`nav.js`, `map.js`, роуты).
5. `/api/map/write` — по готовности сервиса записи (контракт — `docs/map-write-api.md`).
6. Legacy `/write_eeprom.php` (Basic+PAM-кэш; запись в цикле legacy-модуля).
7. Инвалидация при смене пароля.
8. Тесты (401/скользящий TTL/переживание рестарта web).

Rate-limit не делаем.
