# BMS-Receiver: типы BMS — проекты-источники и типы

Папка описывает **типы BMS** для демона `bms-reader`: проекты-источники (референсные
реализации), контракты типов, мэппинг на общий контракт, сценарии probe/опроса,
скорости и правила идентификатора.

- **Общий контракт** — `docs/bms-reader.md` §1; **замороженная архитектура** —
  `docs/bms-reader-arch.md`.
- **Типы:** `docs/bms/<TYPE>.md`.

## Проекты-источники (референс)

### sunReceiver — `bmslistener` (ANT BMS, пассивный слушатель)

- Проект: `../sunReceiver` (ПАК «Малина»), исходник
  `daemons/bmslistener/bmslistener.c`, доки `docs/antbms.md`,
  `docs/antbms/antbms-protocol-status-frame.md`,
  `docs/antbms/antbms-live-frame-decoded.md`, `docs/modules/bms-listener.md`.
- Что делает: **пассивно** читает USB-serial адаптеры (PL2303), на которых ANT BMS
  вещает 140-байтные live-кадры `AA 55 AA FF` на **19200 8N1**; публикует коллекцию
  активных адаптеров в System V shm (ключ 2018) в JSON-формате экосистемы
  (`{"updated":…,"devices":[…]} #EOF`). Адаптеры подключены **только на чтение**.
- Проба: открыть порт, ждать первый валидный кадр (до **7 с**), CRC обязательна;
  провал → backoff **300 с**; бюджет проб **21 с**/скан; скан портов раз в **30 с**;
  молчит **30 с** → забыть.
- Поля кадра и их offsets — в `docs/antbms/antbms-protocol-status-frame.md` и
  `antbms-live-frame-decoded.md` (полный разбор ниже в `antbms-dsp.md`).
- **Серийника BMS в кадре нет** → `bmslistener` именует устройство
  `AntBms <ёмкость_Ah> A/h (<порт>)`.

### sunReceiver — `enBms` (EnBMS, BLE и RS485)

- Проект: `../sunReceiver`, код `cmd/sunreceiver/enBms_rs485.go`, `enBms.go`,
  `enBms_poller.go`; док `docs/modules/enbms.md`.
- Что делает: опрашивает BMS Enjie EMU110x по BLE (бинарный кадр) либо по **RS485**
  (ASCII PACE, YD/T 1363). В нашем типе используется **только RS485**.
- RS485: транспорт — COM-порт или прозрачный TCP-шлюз; CID2 `0x42` TeleMeter
  (телеметрия), `0x44` TeleState (защиты/режим/ключи/баланс), `0x51` BasicInfo
  (модель ASCII); **ADR 0-based**; 19200 8N1.
- **Серийник есть:** CID2 `0xA4` GetBatSN (host-RS485, 19200, без INFO) → 30 Б ASCII;
  заводское BLE-имя `BP00` не уникально (ключ хранения в sunReceiver — MAC/имя).

### energybms (реверс протокола EnBMS)

- Проект: `/home/sasha/src/energybms` — `protocol-485.md`, `protocol-BLE.md`,
  `DEVICE_SNAPSHOT.md`, `REVERSE_ENGINEERING.md`, редактор `energybms-editor`.
- Используется как первоисточник раскладок PACE и битовых карт `Ext_Bit`.

### Временные инструменты (на этом ПК)

- `sunReceiver/docs/antbms/scripts/antbms_poll.py` — активный клиент ANT: шлёт
  keep-alive `5A 5A 00 00 01 01` каждые 250 мс и читает 140-байтные кадры.
- `antbms_init_monitor.py` — мониторинг линии RXD дисплея (9600 8N1) — сам heartbeat
  дисплея в BMS.
- `/home/sasha/src/energybms/tools/query_bms.py` — BLE-клиент EnBMS.

## Общие конвенции (типы v1)

- **Скорости COM:** только **19200** и **9600** (перебор — на уровне типа).
- **Идентификатор:** AntBMS-DSP — серийника нет (BASE по ёмкости); EnBMS RS485 —
  серийник есть (BASE = SN). Суффикс `-NNNNN` и реестр — по `docs/bms-reader-arch.md` §9.
- **Общий контракт:** бинарный, фиксированной длины, макс. 16 ячеек —
  `docs/bms-reader.md` §1.

## Индекс типов

| Тип | Документ | Режим | Протокол | Скорость |
|---|---|---|---|---|
| AntBMS-DSP | [`antbms-dsp.md`](antbms-dsp.md) | пассивный | ANT live-кадр 140 Б | 19200 (фикс.) |
| EnBMS RS485 | [`enbms-rs485.md`](enbms-rs485.md) | активный | PACE ASCII (0x42/0x44/0x51) | 19200 8N1 |
