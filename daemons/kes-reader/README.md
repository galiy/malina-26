# kes-reader — демон опроса КЭС (MPPT, Микроарт)

Отдельный Go-демон ПАК «Малина» (каркас). Ищет подключённые **КЭС/MPPT** на
последовательных портах, захватывает найденный порт и опрашивает устройство.
Список активных КЭС публикует в **разделяемую память** (для `malina-web`),
команды принимает **синхронно** через Unix-сокет.

Демон не поднимает HTTP и не ходит по сети: обмен — только локальный
(shm + unix-сокет). Общий код — в `daemons/lib` (см. `docs/daemons-shared.md`).

Родственные документы: `docs/kes-reader.md` (контракт), `docs/kes-memory.md`
(карта регистров/настроек), `docs/legacy/kes-legacy-malina2.md` (разбор legacy КЭС).

## Как работает

1. Главный поток (`lib/scanner.Runner`) перебирает свободные последовательные
   порты; проба (`probeKesPort`) идёт по скоростям `115200 → 19200 → 57600`
   (legacy-диапазон КЭС, дефолт 19200) и читает 1 байт `_Device (0x0000)`; общий
   **бюджет пробы на порт — 2 с** (`probeDeadline`, как у `map-reader`):
   - `5` → **КЭС** — найден;
   - `3` → **МАП** — чужое MicroArt-устройство: порт **сразу отпускаем** и ставим
     длинный backoff (`backoffForeign` = 10 мин), Modbus/лишний трафик не шлём.
2. Найденный порт захватывается (fd остаётся открытым), КЭС попадает в список,
   запускается горутина опроса `pollPort`.
3. Опрос — **оптимальная разбивка регионов**, минимум страниц MicroArt (≤240 Б) без слияния
   удалённых кластеров (`адрес устройства = RAM + 0x1F0`, ID-окно «как есть»); 6 страниц ≈ 840 Б,
   полное покрытие контрактных адресов (тест `TestWindowsCover`). Образ индексируется по
   **RAM-адресу**. Слот — **три контракта фиксированного размера
   с резервом по 512 Б**: **EEPROM** (`"KESE"`, параметры устройства, адрес «как есть») +
   **RAM** (`"KESR"`, телеметрия + BMS/параллельные MPPT) + **настройки**
   (`"KESS"`, 40 значений RAM-копий, условно подтверждённые); payload слота = 1536 Б.
   Запись `kes-list` публикуется в **отдельной секции того же сегмента**
   `[header][list: 32×72B][slots: 32×slotSize]` (seqlock; отдельного shm-файла нет).

## Признаки КЭС

- `_Device (0x00) == 5` — КЭС/MPPT (у МАП — `3`, зарядник — `7/9`).
- Идентификация — только MicroArt, **один КЭС на порт**.
- Серийник — `Serial` (RAM `0x2C9`, 16 бит LE), как у МАП (`b[28:30]`).

## Обмены

- **Shared memory:** `/dev/shm/malina-kes-state`. Сегмент
  `[header 64B][list: 32×72B][slots: 32×slotSize]` (механика — `lib/shm`).
  Запись `kes-list` (72 Б): `[16:24] Gen`, `[24:28] Baud`, `[28:30] Serial`,
  `[32:64] Port`, `[64:72] LastSeen`. (Поля `model`/`protocol`/`state` в контракте
  не хранятся — убраны.)
- **Управление:** Unix-сокет `/run/malina-kes/control.sock` (каркас — `lib/control`).
Команды: `ping`, `list`, `write` (запись **ячеек** `op=reg`; реле — ячейкой `0x0E3D`,
   без команд к `op=cmd`/`op=relay` — см. «Статус»).
   Запись реле идёт в командную ячейку `0x0E3D` — по спеке она **не подтверждена на
   железе** (см. `docs/kes-reader.md` §7.7).

## Статус

Демон повторяет архитектуру `map-reader` (сборка/тесты/структура) и **готов к тестированию
на железе** (тесты на реальном КЭС не проводились — нет устройства).

- ✅ сканирование/проба (`_Device==5`, быстрый отказ от МАП/чужого MicroArt) — `lib/scanner`;
- ✅ опрос окон (окно = всё адресное пространство КЭС), публикация слота и `kes-list`
  в shm (seqlock) — `lib/shm`;
- ✅ контроль-сокет (`ping/list/write`) — `lib/control`;
- ✅ **запись**: ячейки (`op=reg`: RAM/EEPROM, pre-команда `EEPromWR=3`, адрес «как есть»),
   ретраи/тайминги; реле — ячейкой `0x0E3D` (write-only, RMW — на стороне клиента); команд
   КЭС (`op=cmd`) и отдельной операции реле (`op=relay`) — нет;
- ℹ️ `Serial==0` подставляется как `idx+1` в `kes-list`/слоте (legacy, §1b);
- ⏳ ячейка команды реле `0x0E3D` — подтвердить на железе (`docs/kes-reader.md` §7.7);
- ⏳ контракт слота/shm и интеграция с `malina-web` — `docs/kes-reader.md`.

## Проверка без железа

- `go build ./...`, `go vet ./...`, `go test ./...` (unit: разбор образа/контракта,
  валидация записи, последовательность посылок).
- На железе (когда будет): e2e против `kes-emu` (`daemons/kes-emu`) на `.60`.

## Сборка

    make host        # dist/kes-reader
    make arm         # dist/kes-reader-armv7 (GOARM=7, CGO_ENABLED=0)

## Установка и деплой

- Бинарник кладётся в **`/malina/usr/local/sbin/kes-reader`**, юнит **`kes-reader.service`** —
  в `/etc/systemd/system/` (см. `deploy/manifest.tsv`). CPU-приоритет — как у `map-reader`
  (`Nice=-5`, `CPUWeight=1000`, только CFS).
- Локально на SD — `deploy/local/stage2.sh /dev/sdX` (см. `deploy/local-deploy.md`) —
  ставит бинарник и включает юнит; проверка — `deploy/local/stage4.sh`.
- По SSH на развёрнутом устройстве (точечно, демон): собрать `make -C daemons/kes-reader arm`,
  затем rw-окно → `install -m0755 dist/kes-reader-armv7 /malina/usr/local/sbin/kes-reader`,
  `install -m0644 kes-reader.service /etc/systemd/system/kes-reader.service` →
  `systemctl daemon-reload && systemctl enable --now kes-reader` → `remount,ro`.
- Проверка: `systemctl is-active kes-reader`, `ls -l /dev/shm/malina-kes-state`,
  `journalctl -u kes-reader`, `list` по `/run/malina-kes/control.sock` (пусто без подключённых КЭС).
