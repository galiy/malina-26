# Общая библиотека демонов (`daemons/lib`)

Статус: **реализовано** — общий код `map-reader` ↔ `kes-reader` (и `kes-emu`) вынесен
в `daemons/lib`. Цель — один транспорт и один кадровый слой протокола Микроарт, общая
механика сканирования/публикации/управления; различия устройств (регистры, контракты,
команды) остаются в демонах.

## 0. Модульная схема (решено)

**Единый Go-модуль** `github.com/galiy/malina-26/daemons` (`daemons/go.mod`):

```
daemons/
├─ go.mod                      # module .../daemons (go 1.22, без внешних зависимостей)
├─ lib/
│  ├─ transport/               # ✅ serial: Port, Open, SetBaud, ReadOne, ReadUntilNL, WriteAll, Flush, RS485
│  ├─ microart/                # ✅ кадры Микроарт: Escape, Build{Read,Write}Request, Parse{Reply,WriteReply}, Read, Write
│  ├─ shm/                     # ✅ механика POSIX-shm: Segment (header+seqlock), Slot, Item (формат полей — у демона)
│  ├─ control/                 # ✅ каркас unix-сокет управления (Serve + JSON-фрейминг); диспетчер — у демона
│  ├─ scanner/                 # ✅ Runner: цикл сканирования, backoff (обычный/чужой), active, занятость портов (через /proc); проба — колбэк
│  ├─ ports/                   # ✅ device-agnostic: ListPorts, RdevOf, BusyRdevs (glob/дедуп по rdev, занятость через /proc)
│  ├─ device/                  # ✅ коды _Device (3/5/7/9) + Name()
│  └─ tscontract/              # ✅ контракты телеметрии: схемы/кодеки map/kes/mbms (общие с daemons/tsdb и продюсером malina-web)
├─ map-reader/                 # package main (без своего go.mod)
├─ kes-reader/                 # package main — каркас (сканирование/проба/опрос/shm/control/kes-list)
└─ kes-emu/                    # ✅ package main — «живой» эмулятор КЭС (3 PTY → /dev/ttyUSB9..11), читает/пишет/команды
```

Сборка не меняется: `cd daemons/map-reader && go build .` (модуль находится в
`daemons/go.mod`); `make host/arm` работает как раньше; деплой (`dist/map-reader-armv7`)
не затронут.

> **Почему `ports`, а не `scanner`:** оркестрация пробы (порядок скоростей, **Modbus**,
> логика «чужое устройство», backoff) — **MAP-специфика** и остаётся в `map-reader`
> (`detect.go`, `modbus.go`). В библиотеку вынесены только device-agnostic примитивы
> работы с портами. Общий «runner сканера» с колбэками введём, когда появится второй
> реальный потребитель (`kes-reader`), чтобы интерфейс не выдумывать заранее.

## 1. Раскрой (символы → пакет)

| Пакет | Откуда | Символы |
|---|---|---|
| `lib/transport` | `map-reader/serial.go` | `Port`, `Open`, `Close`, `SetBaud` (+`setBaudOther`), `Flush`, `WriteAll`, `ReadOne`, `ReadUntilNL`, `ErrClosed` (RS485-режим — внутренний, выключен константой) |
| `lib/microart` | `map-reader/microart.go` (кадры) | `Escape`, `BuildReadRequest`, `BuildWriteRequest`, `ParseReply`, `ParseWriteReply`, `Read`, `Write`, `ErrNoDevice`, `ErrBadReply`, `DeviceError` |
| `lib/shm` | `map-reader/shm.go` | `Segment` (`Open/Close/Slot/Item/Data/Slots`), `Slot` (`PrepareGen/Release/Publish`), `Item` (`Clear/Release/Publish`); раскладку/поля задаёт демон через `Layout` |
| `lib/control` | `map-reader/control.go` | `Serve(sock, stop, handler)`, `WriteJSON`; лимиты соединений/дедлайны внутри |
| `lib/ports` | `map-reader/{scan.go}` | `ListPorts`, `RdevOf`, `BusyRdevs` (glob, дедуп по rdev, занятость через `/proc`) |
| `lib/device` | — | коды `_Device` (`TypeMap=3`, `TypeKes=5`, `TypeCharger=7/9`), `Name(code)` |
| `lib/tscontract` | `daemons/tsdb/contract.go` | контракты телеметрии: `FieldType`/`Field`/`Value`, `Contract`, `Registry` (`Get`/`Register`/`Project`), `AppendValue`/`DecodeValue`, `NewDefaultRegistry()` (`map`/`kes`/`mbms`) |

> **Не общее (остаётся в демоне):** Modbus (`modbus.go`), оркестрация пробы и порядок
> скоростей (`detect.go`), MAP-контракт/payload (`cells.go`, `mapdata.go`, `contract.go`,
> `maplist.go`), валидация записи (`write.go`), timesync.

## 2. Что остаётся в демонах

- **`map-reader`:** `cells.go`, `mapdata.go`, `contract.go` (`MapData`/`ContractMeta`),
  `maplist.go`, `parseCells`/`mapInfo`, `write.go` (валидация/`mapError`/`element`),
  `modbus.go` (МАП-специфика Modbus), `timesync.go`, MAP-команды `control`,
  `probePort` (порядок скоростей/`foreign`-логика), `sim.go` (временный → удалить).
- **`kes-reader`** (план): свой контракт/payload, `kes`-ячейки/настройки, свой
  парсер/probe (`_Device==5`), запись ячеек (`op=reg`; реле — ячейкой `0x0E3D`, без
  команд и отдельной операции реле), JSON-формат; использует
  `lib/{transport,microart,scanner,shm,control}`.
- **`kes-emu`** (план): `lib/transport` + `lib/microart` со стороны устройства.

## 3. Границы (сделать generic-точками при выносе)

- **probe:** `type Prober interface { Probe(*transport.Port, *Config) (Result, error) }`,
  где `Result = Found{Groups, Proto, Baud} | Foreign | Absent`; флаги/бэкoff — ядро.
- **shm:** единый формат `[header][list: N×item][slots: N×slot]`, payload/`ListItem` —
  параметризованы типом устройства.
- **control:** общий каркас сокета, `dispatch(method, req) resp` — свой у демона.

## 4. Порядок выноса (strangler) и статус

1. ✅ **`transport` + `microart`** — вынесено; `map-reader` на тонких обёртках.
2. ✅ **`shm`** — механика сегмента/seqlock вынесена (`lib/shm`); байтовый контракт и
   map-поля (`listMeta`, `publishItem`, `decodeListItem`) — остались в `map-reader`.
3. ✅ **`ports` + `scanner`** — device-agnostic примитивы портов и общий `scanner.Runner`
   (цикл/backoff/active с колбэками) вынесены и используются `map-reader` и `kes-reader`.
4. ✅ **`control`** — каркас сокета/JSON вынесен (`lib/control.Serve`); диспетчер команд
   (`ping/list/write`, `dispatchWrite`) — в `map-reader`.
5. ✅ **`device`** — коды `_Device` и `Name()`; `parseCells` использует `device.TypeMap`.

**Проверка:** `go build ./...`, `go vet ./...`, `go test ./...` — зелёные; host+arm
сборка `map-reader` и `kes-reader` ок; `gofmt` чисто. Рефакторинг поведения не меняет;
упрощение критерия МАП (`_Device==3`, без `_VerPO`/`_DevOpt`) — **отдельное
санкционированное изменение** (см. `requirements.md` §9.1), не часть этого переноса.

**`kes-reader`** (второй потребитель, каркас): использует `lib/{transport,microart,
scanner,shm,control,ports,device}`; архитектура повторяет `map-reader` (сканирование →
проба `_Device==5` → опрос окон → публикация в shm/`kes-list` → control-сокет, запись
ячеек (`op=reg`; реле — ячейкой `0x0E3D`)). Готов к тестированию на железе (не тестировался —
нет устройства).

## 5. Открытые вопросы

- Полный generic-«runner сканера» (колбэк/интерфейс `Probe`) — вводим вместе с
  `kes-reader`, чтобы интерфейс подошёл двум реальным потребителям.
- Формат записи списка при мультиустройствах: общий заголовок + типизированные «хвосты»
  или по одному типу на демон (`lib/shm` даёт только seqlock-каркас — формат у демона).
- `kes-emu` шарит `transport`+`microart`: проверить, что сторона устройства не тянет
  лишние зависимости (сейчас `lib` без внешних модулей).
