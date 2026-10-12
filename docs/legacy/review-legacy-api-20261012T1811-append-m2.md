# M2 — артефакт-подтверждение (выгрузка `.68`, 2026-10-12, чтение по SSH)

Подтверждение для пункта M2 ревью `review-legacy-api-20261012T1811.md`: веб-скрипты
эталонной станции `.68` (armv7 4.14.50, доступ по SSH — чтение, без модификаций).

## 1. Локальных потребителей `read_sec` на `.68` нет

`grep -rn "read_sec" /settings/html` — **ни одного вхождения** ни в одном JS/PHP
(сам файл `read_sec.php` исключён). Все локальные чтения памяти — через
`read_memory.php`, и всегда с `offset=0&count=N`:

- `js/home.js:80`        → `read_memory.php?offset=0&count=400`
- `js/batmon_set.js:41`  → `read_memory.php?offset=0&count=400`
- `js/mac_set.js:21`     → `read_memory.php?offset=0&count=512`
- `js/mac_set.js:564`    → `read_memory.php?offset=0&count=512`
- `js/master_node.js`    → вхождений `read_sec`/`read_memory` нет (панель «Мастер-Узел»
  использует `read_json.php?device=bat`/`mppt` и `check_daemons`)

Это не отменяет необходимости паритета: `read_sec`/`write_sec` — **публичный HTTP API**,
доступный **внешним** потребителям с других хостов, поэтому совместимый формат ответа
обязан сохраняться независимо от локальных скриптов. Реализация — в
`web/internal/web/keslegacyapi.go` (`handleReadSec`): `offset=0` → массив, `offset>0` →
объект `{addr:byte}` (см. M2 в ревью-доке).

## 2. Формат `read_sec.php` при `offset>0` — объект (подтверждён исходником)

`/settings/html/read_sec.php` (725 Б, 2018-03-30):

```php
$offset = intval($_REQUEST['offset']);
$count  = intval($_REQUEST['count']);
if ($offset<0 || $count<1 || ($offset+$count)>2048) die('error');
$shm=shmop_open(1995,"a",0,0); ...
$str=shmop_read($shm,$id*2048+$offset,$count);
for ($i=$offset;$i<$offset+$count;$i++) $arr[$i]=ord($str[$i]);
echo json_encode($arr);
```

- при `offset=0` → `json_encode` даёт **массив** `[b0,b1,…]`;
- при `offset>0` ключи `$arr[$i]` начинаются с `$offset` → `json_encode` отдаёт
  **объект** `{"713":0,"714":0}` — как и сказано в
  `docs/legacy/kes-legacy-malina2.md` §20.

Границы: legacy лимит `offset+count ≤ 2048` (наш — ≤ `kesVirtualSize 0xE80`).

## 3. `read_memory.php` — тоже объект при `offset>0`

Та же логика массива ключей `$arr[$i]` от `$offset`, но потребители всегда с
`offset=0`. Использование `read_sec.php` нашим слоем отличается от `read_memory.php`.