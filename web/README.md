# malina-web (Фаза 0)

Каркас веб-админки на Go: один статический бинарник (UI + API + оркестрация —
позже), без nginx/TLS. Аутентификация — по паролю Linux-пользователя `malina`
через PAM (хелпер `malina-pamcheck`).

## MVP (сейчас)
- Страница входа (только пароль; пользователь фиксирован — `malina`, проверка через Linux PAM).
- Сессии: серверные токены + cookie `malina_session` (HttpOnly, SameSite=Lax).
- Меню: **Информация о системе** (градусники CPU/память/swap/диски/temp, сеть, питание; автообновление 1 c), **Система**, **Выход**.
- Переключатель языка: пункты нативно — Русский, Беларуская, 中文, English (именно в этом порядке).
  Источник строк — русский; дефолтный язык интерфейса — английский.
- **Логи**: кнопки на странице информации → общая страница `/logs?file=…` (последние 100 строк, tail 1 c).
- **Система**: смена имени хоста, настройка логирования (свой syslog-хост; пусто → DHCP опция 7), смена пароля (с подтверждением).

## API
- `GET /api/system` — системные данные (JSON).
- `GET /api/log?file=<name>&lines=100` — хвост лога (whitelist файлов `/var/log`).

## Страницы
- `/` — Информация о системе.
- `/system` — Система (hostname/logging/password).
- `/logs?file=…` — просмотр логов.

## Сборка
```
make host      # локально (dev)
make arm       # ARMv7 static для Pi (dist/malina-web-armv7)
make pamcheck  # C-хелпер (на устройстве: нужен gcc; libpam-dev НЕ нужен)
make fmt vet
```

## Dev-запуск (без PAM)
```
MALINA_WEB_DEV_PASSWORD=secret MALINA_WEB_LISTEN=:8080 go run ./cmd/malina-web
```

## Деплой на устройство
- Бинарник: `/malina/usr/local/bin/malina-web` (root `ro` — держим в `/malina`).
- Хелпер PAM: `/malina/usr/local/libexec/malina-pamcheck` (компилируется на .60).
- Хелпер syslog: `/malina/usr/local/libexec/malina-remote-syslog` (`deploy/malina-remote-syslog`) — общий для веба и хука.
- Хелпер rw: `/malina/usr/local/sbin/malina-rw` (`deploy/malina-rw`) — сериализованное окно `remount rw/ro`
  (`malina-rw on|off|run <cmd>`); используется сменой пароля, а в будущем — enable/disable сервисов, apt и т.п.
- DHCP-хук: `deploy/dhcp-hook-99-malina-syslog` → `/etc/dhcp/dhclient-enter-hooks.d/99-malina-syslog`
  (учёт кастомного хоста из `/malina/etc/malina/remote-syslog`, иначе DHCP опция 7).
- Юнит hostname: `deploy/malina-hostname.service` (применяет `/malina/etc/hostname` при загрузке).
- PAM-сервис: `deploy/pam.d-malina-web` → `/etc/pam.d/malina-web`.
- Юнит: `deploy/malina-web.service` → `/etc/systemd/system/`.
- Смена пароля пишет `/etc/shadow` (root `ro`) — сервис делает краткий `remount rw`/`ro`.

### Структура на rw-разделе `/malina` (структура от `/`)
```
/malina/
  etc/malina/                          # конфиги сервиса (web.json и пр.)
  usr/local/bin/                       # бинарники (malina-web)
  usr/local/libexec/                   # вспомогательные (malina-pamcheck)
  usr/local/share/malina/web/          # СТАТИКА (templates/ static/ i18n/) — внешняя
    templates/                         # html-шаблоны
    static/                            # css/js/img (вкл. titanator.jpg)
    i18n/                              # переводы en/ru/zh
  var/lib/malina/                      # состояние/данные сервиса (запись тут, т.к. root ro)
```
Статика **не встраивается в бинарник**: сервис читает `templates`/`i18n` и отдаёт
`static` из каталога `-assets` (по умолчанию `/malina/usr/local/share/malina/web`).
Это позволяет править вёрстку/переводы/картинки без пересборки. Правки шаблонов/i18n
применяются после `systemctl restart malina-web` (статика отдаётся сразу).
Статические части (PAM-сервис, systemd-юнит) остаются на `ro`-корне.

### Команды деплоя
```
make arm
gcc -O2 -o dist/malina-pamcheck pamcheck/pamcheck.c -ldl   # на устройстве
# на .60:
#   install -m0755 malina-web-armv7 /malina/usr/local/bin/malina-web
#   install -m0755 malina-pamcheck  /malina/usr/local/libexec/malina-pamcheck
#   install -m0644 pam.d-malina-web /etc/pam.d/malina-web
#   install -m0644 malina-web.service /etc/systemd/system/malina-web.service
#   systemctl daemon-reload && systemctl enable --now malina-web
```


## Флаги / переменные
- `-listen` (`MALINA_WEB_LISTEN`), по умолчанию `:8080`.
- `-user` (`MALINA_WEB_USER`), по умолчанию `malina`.
- `-pam-helper` (`MALINA_WEB_PAM_HELPER`).
- `MALINA_WEB_DEV_PASSWORD` — только для разработки.

## Структура
- `cmd/malina-web` — точка входа.
- `internal/web` — сервер, роутинг, шаблоны/статика/i18n (`assets/`, embed).
- `internal/auth` — PAM/dev аутентификатор.
- `internal/session` — in-memory сессии.
- `internal/i18n` — словари переводов.
- `pamcheck/` — C-хелпер (dlopen libpam).
- `deploy/` — systemd/PAM.

## Безопасность (перед коммитом)
Секреты/приватные данные в репозиторий не попадают (`.kilo/` игнорируется);
скан обязателен (см. правила проекта).
