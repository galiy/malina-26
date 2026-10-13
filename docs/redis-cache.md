# Redis — in-memory кэш телеметрии

Лёгкий in-memory кэш для статистики рядов телеметрии устройств (МАП/КЭС).
Отдельный процесс **Redis 7** (пакет `redis-server` из основных репозиториев
Debian 12 bookworm, BSD-3, armhf), без персиста. Конфиг и запуск — наши.

## Требования и как закрыты

| Требование | Решение |
|---|---|
| Не in-app кэш | Отдельный демон `redis-server` |
| Индекс по 2 полям: deviceID + время | Sorted Set на ряд: `ts:{dev}:{metric}`, `score` = Unix ms (время), `member` = значение |
| Быстрый доступ для графиков | `ZRANGEBYSCORE key t0 t1 WITHSCORES` — O(log N + M) |
| Авто-чистка старых строк | `ZREMRANGEBYSCORE key -inf (now-retention)` по таймеру (цикл обслуживания) + `EXPIRE` на ключ |
| Ограничение памяти | `maxmemory 200mb` + `maxmemory-policy allkeys-lru` |
| Доступ только с устройства, но можно открыть по сети | `bind 127.0.0.1 -::1` по умолчанию; открытие — см. ниже |
| Без записи на диск | `save ""`, `appendonly no`, рабочий каталог `dir /run/redis` (tmpfs) |

## Компоненты в репозитории

- `system/redis/malina.conf` → `/etc/malina/redis.conf` — наш конфиг (bind,
  лимит памяти, без персиста).
- `system/systemd/redis-server.service.d/malina.conf` →
  `/etc/systemd/system/redis-server.service.d/malina.conf` — дроп-ин, запускает
  `redis-server` с нашим конфигом вместо дистрибутивного.
- `system/tmpfiles/malina.conf` — строка `d /run/redis 0755 redis redis -`.
- Пакет `redis-server` входит в образ (`image/stage-malina/00-packages`).
- `deploy/manifest.tsv` — обе записи конфига; `deploy/local/stage2.sh` и
  `deploy/install.sh` включают юнит `redis-server` при первичном деплое.

> Пакетный `/etc/redis/redis.conf` не трогаем — дроп-ин переопределяет `ExecStart`.
> Если в конфиг добавить `requirepass`/ACL, файл `/etc/malina/redis.conf` следует
> сделать `0640 root:redis` (в манифесте сейчас `0644` — секретов в нём нет).

## Ключевые параметры `/etc/malina/redis.conf`

- `bind 127.0.0.1 -::1`, `protected-mode yes`, `port 6379` — только с устройства.
- `save ""`, `appendonly no`, `dir /run/redis`, `pidfile /run/redis/redis-server.pid`
  — никакой записи на носитель.
- `maxmemory 200mb`, `maxmemory-policy allkeys-lru` — жёсткий лимит, вытеснение LRU.
- `daemonize no`, `supervised systemd`, `logfile ""` — логи в journald.

## Доступ по сети (по умолчанию выключен)

Redis слушает только loopback. Чтобы открыть доступ по сети, **и**:

1. В `/etc/malina/redis.conf` заменить `bind 127.0.0.1 -::1` на нужный адрес
   (например `bind 0.0.0.0`), при необходимости — `protected-mode`.
2. Обязательно закрыть доступ аутентификацией: `requirepass <пароль>` или ACL
   (`user <name> ... >pass`). Пароль хранить в `.kilo/` (не коммитить).
3. Ограничить на уровне `nftables` (только нужный интерфейс/подсеть).

Изменения конфига требуют `systemctl restart redis-server` и rw-окна для `/etc`
(`malina-rw`). Redis-адрес и пароль в UI/коде — вне рамок этого изменения.

## Модель данных (потребителю)

- Ряд = `ts:{deviceType}:{deviceID}:{metric}` (ZSET), `score` — время в мс.
- Запись: `ZADD ts:map:1234:unet <ms> <value>`; при желании `EXPIRE` на ключ.
- График: `ZRANGEBYSCORE ts:map:1234:unet <t0> <t1> [WITHSCORES] [LIMIT ...]`.
- Чистка: периодически `ZREMRANGEBYSCORE key -inf <now-retentionMs>`.
- `allkeys-lru` — страховка сверх retention: при 200 МБ вытесняются самые старые
  ключи.

## Эксплуатация

```
systemctl status redis-server
redis-cli -h 127.0.0.1 info memory | head
redis-cli -h 127.0.0.1 dbsize
```

Redis-клиента в проекте пока нет: доступ к кэшу (запись/чтение рядов) —
отдельная задача.
