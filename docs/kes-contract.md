# Контракт kes-reader

Единый бинарный контракт КЭС (MPPT), публикуемый в POSIX shared memory
(`docs/kes-reader.md` §4). Значения **сырые** (масштабирование — на потребителе),
многобайтовые ячейки — little-endian. В отличие от `map-contract.md`, у КЭС **нет
сквозного id-списка полей**: контракт адресуется **фиксированными смещениями**
внутри трёх под-блоков слота.

Формат (сегмент `version = 1`): **kes-list** — запись `72 Б`; **слот** — payload
`3 × 512 = 1536 Б` из трёх независимых под-блоков: `"KESE"` (EEPROM: паспорт/номинал),
`"KESR"` (RAM: служебное + телеметрия + BMS/параллельные MPPT), `"KESS"` (40 настроек,
**условно подтверждённые** — см. `docs/kes-memory.md` §3). Источники истины по
ячейкам — `docs/kes-memory.md`; всё непроверенное — `docs/kes-memory-not-accepted.md`.

## Зависимости контракта (менять синхронно!)

Аналогично МАП: любое изменение раскладки слота/списка затрагивает ВСЕ места ниже;
`kes-reader` и `web` выкладывать **одним релизом** (иначе payload разойдётся с
декодером).

- `daemons/kes-reader`:
  - `shm.go` — `kesShmMagic`/`kesShmVersion`/`kesShmHeader`/`kesSlots`/
    `kesListItemSize`/`kesSlotData`, смещения `listGenOff`/`listBaudOff`/
    `listSerialOff`/`listPortOff`/`listLastSeenOff`, `listMeta`/`publishItem`/
    `listDoc`/`KesView`.
  - `kes.go` — `kesWindows` (окна чтения), `KesData`, `kesSettings`/`lenKesSettings`,
    `buildEepromPayload`/`buildRamPayload`/`buildSettingsPayload`, константы смещений
    `kese*Off`/`kesr*Off`/`kess*Off`, `memberKeseSize`/`memberKesrSize`/`memberKessSize`,
    `offKese`/`offKesr`/`offKess`.
  - `poll.go`/`state.go`/`write.go` — публикация слота/списка, `kesSlots`-аллокация.
- `web` (Go): `internal/web/kesstate.go` — `kesShmMagic`/`kesShmVersion`/
  `kesShmHeader`/`kesSlots`/`kesListItemLen`/`kesSlotHdr`/`kesSlotData`,
  `kesList*Off`, `KesInfo`/`KesList`, `loadKesList`/`decodeKesListItem`/`kesRawPayload`.
- **Версия сегмента пиннится у потребителя:** `kesShmVersion` в web (`kesstate.go`)
  обязан совпадать с `kesShmVersion` в `daemons/kes-reader/shm.go`. При расхождении
  `loadKesList`/`kesRawPayload` считают сегмент невалидным (пустой список, `raw` → 404).
- **Golden-тест контракта:** `daemons/kes-reader/kes_test.go` (`TestContractGolden`)
  фиксирует магии/смещения под-блоков — падает при дрейфе. Коды/окна — `kes_probe_test.go`.
- Документ: `docs/kes-reader.md` §4 (кратко), `docs/kes-memory.md` (адреса ячеек).

Пример: добавление поля в телеметрию требует правки `kes.go` (смещения `kesr*Off` +
`buildRamPayload`), синхронного отражения в декодере потребителя, обновления
`TestContractGolden` и этого документа.

## Сегмент shm и заголовки (полный формат)

Именованный сегмент POSIX (`/dev/shm/malina-kes-state`), один писатель-процесс:
```
[header 64B][list: slots×72B][slots: slots×slotSize]
```
`slots = 32`, `slotSize = 32 + 16384 = 16416`, общий размер = 64 + 32×72 + 32×16416
= **527 680 Б**. Механика — общий пакет `daemons/lib/shm` (`Header=64`, `SlotHeader=32`,
`MaxSlotData=16384`).

**Заголовок сегмента (64 Б):**
```
0  magic    u32   = 0x3153454B ("KES1")
4  version  u32   (текущая 1)
8  slotSize u32   (16416)
12 slots    u32   (32)
16..63      резерв (нули)
```

**Запись списка `kes-list` (72 Б), по одной на слот:**
```
0  state        u32  (0 свободен / 1 активен)
4  reserved     u32
8  seq          u64  (seqlock: нечёт=идёт запись)
16 gen          u64  (номер «аренды» слота = timestamp назначения, uint64)
24 baud         u32
28 serial       u16  (серийник КЭС, как у МАП)
30 reserved     u16
32 port         [32]byte (NUL-терминированная строка)
64 lastSeen     i64  (unix)
```
Поля `model`/`protocol`, присутствующие в `map-list`, у КЭС **убраны** (тип один —
`types`). Декодер — `listDoc` (демон, канонический) и `decodeKesListItem` (web).

**Заголовок слота (32 Б), далее payload:**
```
0  state    u32  (0 свободен / 1 активен; =1 только после первой публикации)
4  reserved u32
8  gen      u64
16 seq      u64  (seqlock)
24 len      u32  (длина payload)
28 crc      u32  (CRC32 IEEE payload)
32 ..      payload (1536 Б, см. ниже)
```

Доступ: список — `header + i*72`; слот — `header + 32×72 + i*16416`. Seqlock: писатель
держит `seq` нечётным на время записи и чётным по завершении; читатель берёт `seq` до и
после (плюс `state`, `len`, `crc`). Реализация — `shm.go`/`lib/shm`.

## Полезная нагрузка слота (payload): [KESE][KESR][KESS]

Три независимых под-блока по 512 Б; неиспользуемый остаток — нули (резерв). Суммарно
`1536 Б` (укладывается в `kesSlotData=16384`). Смещения под-блоков: `offKese=0`,
`offKesr=512`, `offKess=1024`.

### `"KESE"` — EEPROM (паспорт/номинал), 512 Б (занято 15 Б)

`адрес «как есть»`, magic `"KESE"`, `version = 1`.

| off | поле | тип | RAM ячейка | описание |
|---|---|---|---|---|
| 0 | magic | `[4]byte` | — | `"KESE"` |
| 4 | version | u8 | — | `= 1` |
| 6 | `Serial` | u16 LE | 0x2C9 | серийник (`0x2C9`(мл.) + `0x2CA`(ст.)), сырое десятичное |
| 8 | `Device` | u8 | 0x00 | `_Device = 5` (для КЭС) |
| 9 | `Pow` | u8 | 0x01 | `Pow` (строка `SUN PowN …`), значение как есть |
| 10 | `FW` | u8 | 0x02 | версия ПО: `major = v&0x0F`, `minor = v>>5` |
| 11 | `ModelPow` | u8 | 0x05 | мощность модели: `Вт = значение × 100` |
| 12 | `ModelVolt` | u8 | 0x9B | напряжение модели: `В = значение × 12` |
| 13 | `Pic` | u8 | 0x0C | `Pic`/`Модель`, фиксировано `= 1` |
| 14 | `Net` | u8 | 0x0D | `Net`, значение как есть |
| 15..511 | резерв | | | нули |

### `"KESR"` — RAM (служебное + телеметрия + BMS/MPPT), 512 Б (занято ~327 Б)

`адрес = RAM + 0x1F0` (указан в колонке «RAM»), magic `"KESR"`, `version = 1`.

| off | поле | тип | RAM ячейка | описание |
|---|---|---|---|---|
| 0 | magic | `[4]byte` | — | `"KESR"` |
| 4 | version | u8 | — | `= 1` |
| 6 | `LastSeen` | u64 LE | — | unix-ts снимка (служебное) |
| 14 | `RelayC` | u8 | 0x3E3 | состояние реле: бит0/1/2 = реле 1/2/3 |
| 15 | `RSErrSis` | u8 | 0x3B6 | ошибки контроллера |
| 16 | `I2CErr` | u8 | 0xD5A | ошибка протокола I2C (ведомый режим, к МАП) |
| 18 | `VcPV` | u16 LE | 0x2D9 | напряжение солнечных панелей (`12,34`, ×100) |
| 20 | `IcPV` | u16 LE | 0x2DF | ток солнечных панелей (`12,34`) |
| 22 | `VBat` | u16 LE | 0x2E5 | напряжение АКБ (`12,34`) |
| 24 | `PPV` | u16 LE | 0x2EB | мощность солнечных панелей, Вт |
| 26 | `POut` | u16 LE | 0x2ED | мощность на выходе контроллера, Вт |
| 28 | `PLoad` | u16 LE | 0x2F5 | мощность нагрузки (= `P_EXTS[1]`), Вт |
| 30 | `PCurr` | u16 LE | 0x2F9 | мощность заряда, Вт |
| 32 | `ICh` | u16 LE | 0x2FB | ток заряда АКБ (`12,34`) |
| 34 | `IOut` | u16 LE | 0x2FF | ток на выходе контроллера (`12,34`) |
| 36 | `TempInt` | u8 | 0x31D | внутренняя температура (`x−50`) |
| 37 | `TempBat` | u8 | 0x31E | температура АКБ (`x−50`) |
| 38 | `PwrW` | u16 LE | 0x39F | энергия за сутки, единицы Ватт |
| 40 | `PwrKW` | u16 LE | 0x3A1 | энергия за сутки, единицы килоВатт |
| 42 | `IExts0` | u16 LE | 0x406 | ток датчика №1 (`12,34`) |
| 44 | `IExts1` | u16 LE | 0x408 | ток датчика №2 (он же ток нагрузки) |
| 46 | `PExts0` | u16 LE | 0x40A | мощность от альтернативного источника, Вт |
| 48 | `PExts1` | u16 LE | 0x40C | мощность нагрузки (= `P_Load`), Вт |
| 50 | `Mode` | u8 | 0x2C2 | режим заряда (`i`/`v`/`B`/`b`/`S`), ASCII |
| 51 | `Sign` | u8 | 0x37A | знак тока датчика (`+`/`-`) |
| 52 | `MPP` | u8 | 0x379 | точка МРР (`R`/`L`/`0`) |
| 53 | `Windspeed` | u16 LE | 0x3F8 | частота вращения ВГ; `0xFFFF` → `65535` (н/д) |
| 55 | `UakbCell[32]` | u16×32 | 0xD80 | напряжение банок BMS (12В→4, 24В→8, 48В→16, 96В→32) |
| 119 | `TakbCell[32]` | u8×32 | 0xDC0 | температура банок (`x−50`, 255 = не подключена) |
| 151 | `QCell[32]` | u16×32 | 0xDE0 | ток разряда банок: `I=(Q/100)·(UakbCell/100)` |
| 215 | `ErrCells[32]` | u8×32 | 0xE00 | ошибки BMS |
| 247 | `IAkbMPPT[16]` | u16×16 | 0xE30 | ток заряда параллельных MPPT |
| 279 | `ModeMPPT[16]` | u8×16 | 0xE50 | мода: 0 цикл, 1 дозаряд, 2 буфер1, 3 буфер2 |
| 295 | `TakbMPPT[16]` | u8×16 | 0xE60 | температура параллельных MPPT (`x−50`, 0 = нет) |
| 311 | `ErrMPPT[16]` | u8×16 | 0xE70 | ошибки параллельных MPPT |
| 327..511 | резерв | | | нули |

Суммарная энергия за сутки = `PwrKW×1000 + PwrW`.
Поля `Mode`/`Sign`/`MPP`/`Windspeed` публикуются и отдаются в `read_json?device=mppt`
(legacy §1b).

### `"KESS"` — настройки (условно подтверждённые), 512 Б (занято 85 Б)

`[magic "KESS"][version][40]×uint16 LE`, magic `"KESS"`, `version = 1`. Порядок — как
в `kesSettings` (демон) = `docs/kes-memory.md` §3. **Статус: RFC** — источник
`Карта Памяти_WG.csv`, в два источника истины не входит, **требуется проверка на железе**.

| i | off | параметр | имя | RAM | Б |
|---|---|---|---|---|---|
| 0 | 5 | Скорость бегущей строки | `Roll_H_Speed` | 0x210 | 1 |
| 1 | 7 | Яркость подсветки | `BL_Bright` | 0x211 | 1 |
| 2 | 9 | Время подсветки | `BL_Time` | 0x212 | 2 |
| 3 | 11 | Время индикации | `Ind_Time` | 0x214 | 2 |
| 4 | 13 | Страница 1 | `Page_I[0]` | 0x216 | 1 |
| 5 | 15 | Страница 2 | `Page_I[1]` | 0x217 | 1 |
| 6 | 17 | Страница 3 | `Page_I[2]` | 0x218 | 1 |
| 7 | 19 | Страница 4 | `Page_I[3]` | 0x219 | 1 |
| 8 | 21 | Страница 5 | `Page_I[4]` | 0x21A | 1 |
| 9 | 23 | Поиск ТММ | `MPPWM_mode` | 0x21C | 1 |
| 10 | 25 | Тип стабилизации | `Stab_Mode` | 0x21D | 1 |
| 11 | 27 | Период сканирования | `T_Scan` | 0x21E | 2 |
| 12 | 29 | Параллельное включение | `N_Par` | 0x220 | 2 |
| 13 | 31 | Количество последовательное | `PV_Q_S` | 0x222 | 2 |
| 14 | 33 | Количество параллельное | `PV_Q_P` | 0x224 | 2 |
| 15 | 35 | Ток КЗ | `PV_I_Shrt` | 0x226 | 2 |
| 16 | 37 | Напряжение ХХ | `PV_V_Open` | 0x228 | 2 |
| 17 | 39 | Напряжение МРР | `PV_V_MPP` | 0x22A | 2 |
| 18 | 41 | Ток МРР | `PV_I_MPP` | 0x22C | 2 |
| 19 | 43 | Темп. коэфф. напряжения | `PV_V_K_Temp` | 0x22E | 2 |
| 20 | 45 | Темп. коэфф. тока | `PV_I_K_Temp` | 0x230 | 2 |
| 21 | 47 | Напряжение АКБ вкл | `V_Bat_R_On` | 0x232 | 2 |
| 22 | 49 | Напряжение АКБ выкл | `V_Bat_R_Off` | 0x234 | 2 |
| 23 | 51 | Избыток солнца 1 | `OVR_P[0]` | 0x236 | 2 |
| 24 | 53 | Избыток солнца 2 | `OVR_P[1]` | 0x238 | 2 |
| 25 | 55 | Избыток солнца 3 | `OVR_P[2]` | 0x23A | 2 |
| 26 | 57 | Режим экономии | `Econom_Mode` | 0x23C | 1 |
| 27 | 59 | Реле №1 | `Relay_1` | 0x23D | 1 |
| 28 | 61 | Реле №2 | `Relay_2` | 0x23E | 1 |
| 29 | 63 | Реле №3 | `Relay_3` | 0x23F | 1 |
| 30 | 65 | Выбор направления | `I2C_Dist` | 0x240 | 1 |
| 31 | 67 | Выбор адреса | `I2C_Own_Addr` | 0x241 | 2 |
| 32 | 69 | Тип | `Bat_Type` | 0x243 | 1 |
| 33 | 71 | Емкость | `Bat_Cap` | 0x244 | 2 |
| 34 | 73 | Напряжение циклич. заряда | `Bat_Volt_Cyc` | 0x246 | 2 |
| 35 | 75 | Напряжение буферн. заряда | `Bat_Volt_Buf` | 0x248 | 2 |
| 36 | 77 | Напряжение разряда | `Bat_Volt_Dis` | 0x24A | 2 |
| 37 | 79 | Макс. ток заряда | `Bat_I_Ch_max` | 0x24C | 2 |
| 38 | 81 | Ток буферного заряда | `Bat_I_Buff` | 0x24E | 2 |
| 39 | 83 | Темп. коэффициент | `Bat_K_Temp` | 0x250 | 2 |
| — | 85..511 | резерв | | | нули |

`off` = `5 + 2×i` (uint16 LE). Значения — сырые (`raw`); формулы масштаба/диапазоны —
`docs/kes-memory.md` §3.

## Go-структура контракта (`kes.go`)

```go
// KesData — поля контракта. Публикация идёт НЕ по порядку полей, а по явным
// offset-константам (kese*Off/kesr*Off) в buildEepromPayload/buildRamPayload;
// порядок ниже — как в kes.go, справочно. Поля, помеченные «не публикуется»,
// в payload не попадают.
type KesData struct {
	// KESE — паспорт/номинал
	Device, Pow, FW, ModelPow, ModelVolt, Pic, Net byte
	Serial uint16

	// KESR — телеметрия
	VcPV, IcPV, VBat, PPV, POut, PLoad, PCurr, ICh, IOut uint16
	TempInt, TempBat byte
	PwrW, PwrKW uint16
	IExts0, IExts1, PExts0, PExts1 uint16
	RelayC, RSErrSis, I2CErr byte
	SignC0, SignC1 byte // не публикуется (RAM 0x404/0x405)
	Mode, Sign, MPP byte
	Windspeed uint16

	// KESR — BMS и параллельные MPPT
	UakbCell [32]uint16
	TakbCell [32]byte
	QCell    [32]uint16
	ErrCells [32]byte
	IAkbMPPT [16]uint16
	ModeMPPT [16]byte
	TakbMPPT [16]byte
	ErrMPPT  [16]byte

	// KESS — 40 настроек (сырые), порядок = kesSettings
	Settings [40]uint16
}
```
Сборка payload — `buildEepromPayload` (KESE) + `buildRamPayload` (KESR) +
`buildSettingsPayload` (KESS) → конкатенация `[KESE][KESR][KESS]`.

## Список (`kes-list`) — бинарная коллекция

`ListItem` (72 Б): `state u32`, `reserved u32`, `seq u64` @8, `gen u64` @16,
`baud u32` @24, `serial u16` @28, `reserved u16` @30, `port[32]` @32, `lastSeen i64` @64.

Go-типы: `daemons/kes-reader/shm.go` (`listMeta`, `KesView`, `listDoc`);
web `internal/web/kesstate.go` (`KesInfo`, `KesList`, `loadKesList`,
`decodeKesListItem`). Читатель: `loadKesList` → элемент `(ID, Gen)` → слот `ID` →
`kesRawPayload` (бинарный payload 1536 Б) для `/api/kes/raw`.

## Потребители

- **Native REST (web):** `/api/kes/{devices,raw,write}` — `web/internal/web/kesapi.go`
  (`handleAPIKesDevices`/`handleAPIKesRaw`), читает `kes-list` и payload через
  `kesstate.go`. Авторизация — cookie-сессия **или** `Bearer` (`docs/api-auth.md`).
- **Legacy-чтение:** `read_json.php?device=mppt`, `read_sec.php` — через web-декодер
  legacy-полей (окна `kesWindows`).
- **Мастер-Узел / агрегатор:** `check_daemons.php` (флаг присутствия КЭС), см.
  `docs/legacy/master-node.md`.
