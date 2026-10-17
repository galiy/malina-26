# BMS-Receiver: план реализации (предложение)

**Статус:** черновик-предложение. Реализация — **по отдельной отмашке** (поэтапно).

**Основа:** `docs/bms-reader-arch.md` (замороженная архитектура),
`docs/bms-reader.md` (контракт §1 + циклы §2/§3), `docs/bms/README.md` и
`docs/bms/<TYPE>.md` (типы).

## 1. Что уже готово (спека)

- Архитектура (компоненты, пул/backoff/баны, probe-цикл, ID-реестр, тайминги,
  конфиг/reload, tsdb, имена/пути).
- Контракт `bms` v1: раскладка 107 Б, tsdb-схема (`field_id` 1..58) и агрегация
  (`alarm`/`flags`/`balance_mask` → OR; `mode` → last).
- Типы: AntBMS-DSP (пассив, 19200, 500 мс), EnBMS RS485 (host, ADR 0..15, SN `0xA4`).

## 2. Чего не хватает для старта (пробелы)

1. **Плана реализации** (этот документ).
2. **Каркаса демона** и интеграции в сборку/деплой (`daemons/bms-reader`,
   `web`, `deploy/`).
3. **Кодека контракта**: 107-Б бинарный + `tscontract`-contract `(bms, v1)`.
4. **Транспорта**: единый интерфейс Serial/TCP (TCP — новый в `lib`).
5. **Реестра ID** (`bms-ident.json`) и пула источников/слотов.
6. **tsdb**: расширение `validKey` (10→50) + тесты + `telemetry-store.md`.
7. **Аккумулятора**: колонки `bms` и поддержка агрегации **OR** для битфилдов.
8. **Веб / web-API** — **вне рамок** текущей реализации (отложено; см. M7).
9. **Тестов/эмуляторов**: AntBMS (пассив) и EnBMS PACE (сервер) для e2e/smoke.
10. **Реализационные мелочи**: схемы JSON конфигов, команды control-сокета,
    unit-файл, размер shm-сегмента.

## 3. Этапы (milestones)

### M0. Каркас демона
- `daemons/bms-reader/` (модуль `daemons`): `main.go`, конфиг, control-сокет
  (`lib/control`), shm-сегмент (`lib/shm`: **список + до 16 телеметрийных слотов 107 Б**),
  systemd `malina-bms-reader.service`.
- **Приёмка:** собирается (`go build/vet/test ./daemons/...`), стартует, публикует
  пустой список в shm, корректно гасится по SIGTERM.

### M1. Контракт и tsdb
- 107-Б бинарный кодек (encode/decode, BE) — `lib` или `daemons/bms-reader/contract`.
- `tscontract` contract `(bms, v1)`: `field_id` 1..58, типы/ед.
- tsdb: `validKey` 10→50 (`store.go`), `format.go`, тесты, `telemetry-store.md`.
- **Приёмка:** unit — round-trip 107 Б; contract encode/decode; `validKey` ≥50.

### M2. Транспорт
- Единый интерфейс транспорта (Serial + TCP) поверх `lib/transport`; reuse
  `lib/ports` (ListPorts/BusyRdevs).
- TCP: framing-метка `m-tcp`/`m-rtu`/`native`.
- **Приёмка:** unit (mock TCP), открытие/закрытие/таймауты.

### M3. Оркестратор (пул + probe-цикл + ID)
- Пул источников (порт/слот), backoff-лестница, баны (NotMine 5 мин),
  последовательный probe-цикл (`Found/NotMine/NoAnswer`), рескан, мьютекс порта.
- Реестр ID (`bms-ident.json`): BASE↔ID, привязка `by-path`, суффикс `-NNNNN`,
  атомарная запись (temp+rename+fsync), reload по SIGHUP.
- **Приёмка:** unit — пул/баны/ID-матчинг/атомарность.

### M4. Тип AntBMS-DSP
- probe (пассив 500 мс, `AA 55 AA FF`+CRC), модуль (разбор 140 Б → 107 Б),
  мэппинг, `mode` по току, `alarm`, ID по ёмкости.
- **Приёмка:** эмулятор AntBMS (генератор кадров; для смоков — есть и живое железо) →
  round-trip; CRC-кейсы.

### M5. Тип EnBMS RS485
- probe (ADR 0..15, `0xA4` SN, `0x51`, `0x42`), модуль (`0x42`/`0x44` → 107 Б),
  канонический `mode`, `alarm`-битфилд, ID = SN.
- **Приёмка:** эмулятор PACE-сервера (для смоков; есть и живое железо) → round-trip;
  `NotMine`/`NoAnswer`-кейсы.

### M6. Публикация и агрегация
- puller: **107 Б → shm** (телеметрийный слот, читает web) **и** 107 Б → `tscontract` →
  `stat.Publish` (raw); heartbeat `lastSeen` → оркестратор; shm-список пишет оркестратор.
- Аккумулятор: колонки `bms`, агрегация **OR** (`flags`/`balance_mask`/`alarm`),
  `last` (`mode`); avg.
- **Приёмка:** e2e с эмуляторами → raw/avg в tsdb.

### M7. web-API `/api/bms/*` — **реализовано**; UI — по ТЗ `docs/bms-ui.md`

> **API реализован** (`/api/bms/{devices,raw}`, `mainlist.bms`). **UI** (меню МАП/КЭС/БМС,
> страница устройства `/bms`, раздел «БМС» на главной; удаление «Монитора») — по ТЗ
> `docs/bms-ui.md`, **реализация по отмашке**.

- **`web/internal/web/bmsstate.go`** — чтение shm-сегмента BMS (`bms-list` + слот 107 Б),
  раскладка совпадает с `daemons/bms-reader/shm.go`.
- **`web/internal/web/bmsapi.go`**:
  - `GET /api/bms/devices` — JSON-список BMS (`type:"bms"`, `id`, `gen`, `serial`, `base`,
    `bms_type`, `source`, `framing`, `last_seen`);
  - `GET /api/bms/raw?id&gen` — **бинарный payload слота** (контракт 107 Б), как
    `/api/kes/raw`;
  - **`write` нет** — BMS строго **RO**.
- Маршруты зарегистрированы в `web/internal/web/server.go`; авторизация — как у
  `/api/kes/*` (cookie||Bearer); задокументировано в `docs/api-auth.md`.
- **Тесты:** `web/internal/web/bmsapi_test.go` (devices/raw/401/404/400 на временном
  shm-сегменте).
- **UI-страница** (настройка шлюзов, отображение) — **вне рамок** (как и решено).

### M8. Деплой и восстановимость
Демон **новый**, поэтому включается во **все** общие точки деплоя (сейчас в них его нет):

- **Сборка:** Makefile-цель `arm` в `daemons/bms-reader`; `make -C daemons/bms-reader arm`;
  `BIN_BMS=…/dist/bms-reader-armv7`; копирование в stage и `scp` — рядом с
  map/kes/tsdb (`deploy/deploy.sh`).
- **Установка:** `install bms-reader → /malina/usr/local/sbin/bms-reader`.
- **Unit:** `daemons/bms-reader/bms-reader.service` →
  `/etc/systemd/system/malina-bms-reader.service`.
- **Списки сервисов `deploy/deploy.sh`:** добавить `malina-bms-reader` в `enable`,
  `restart`, `is-active` (там же, где `map-reader`/`kes-reader`/`malina-tsdb`).
- **`deploy/manifest.tsv`:** строки для бинаря, юнита и конфигов (`bms-reader.json`,
  `bms-ident.json`) — первичное развёртывание (`--first-run`).
- **`deploy/local/deploy-local.sh` + `deploy/local-deploy.md` + `deploy/restore.md`:** офлайн-карта
  и восстановление (демон/юнит/конфиги/каталоги rw).
- **Веб-ассеты:** страница/i18n `bms` уезжают в `web/assets` (`assets.tar`) — списки
  сервисов не меняют.
- **Приёмка:** `deploy/deploy.sh --first-run` (манифест) и обычный `--build` → деплой на
  `.60`, `systemctl is-active malina-bms-reader`.

### M9. Приёмка
- unit + e2e + UI smoke; **live** на реальных BMS (AntBMS-DSP, EnBMS host-RS485) —
  с согласованием сетевого доступа; проверка знака тока Ant и SN EnBMS.

## 4. Реализационные решения (подтверждено)

- **Go-модуль:** демон в `daemons/bms-reader` (в модуле `daemons`); общий код — в
  `daemons/lib` (новое: TCP-транспорт, контракт-кодек).
- **Конфиги (подтверждено):** `bms-reader.json` — `[{host, port, mode:"modbus"|"none"}]`;
  `bms-ident.json` — `{counter, entries:[{base,id,binding}]}`.
- **control-сокет (подтверждено):** команды `reload`, `list`.
- **shm:** сегмент: **список + до 16 телеметрийных слотов (107 Б)** — по образцу `map`/`kes`.
- **unit:** `malina-bms-reader.service`, `ExecReload=/bin/kill -HUP $MAINPID`.
- **tsdb (подтверждено):** `validKey` → **50** (полный масштаб: `format.go`, тесты, мета/UI,
  `docs/telemetry-store.md`).
- **Retention (подтверждено):** вытеснение по объёму — **в общем порядке, независимо от
  типа устройства** (без спец-логики для `bms`).
- **Web/API:** **вне рамок** текущей реализации (M7 отложен).

## 5. Риски/зависимости

- **Сеть:** прод-TCP-шлюзы — пока нет; **тестовый выдан** (PACE, read-only; адрес — в `.kilo/`).
- **Железо:** 2 живые BMS (AntBMS-DSP, EnBMS host-RS485) — подключаются к `.60` по запросу.
- **tsdb:** смена `validKey` — правка контракта хранилища (ревизия зависимостей).
- **Аккумулятор:** добавить `bmsRules` и поддержку OR-агрегации (сейчас только `map`/`kes`).
- **Проверки на живом:** знак `current_da` у Ant; чтение SN `0xA4` и probe/ADR у EnBMS.

### Тестовое окружение (приватно)

Адреса/хосты — в **`.kilo/bms-test-env.md`** (не коммитится): тестовый TCP-шлюз (PACE,
read-only) и 2 живые BMS на устройстве `.60` (по потребности).

## 6. Порядок

M0 → M1 → M2 → M3 → (M4, M5 параллельно) → M6 → M8 → M9; **M7 (web) — вне рамок**.

Каждый этап — отдельный коммит; после M3 возможен частичный e2e на эмуляторах.
