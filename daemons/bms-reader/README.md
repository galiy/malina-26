# bms-reader — сбор телеметрии BMS

Демон ПАК «Малина»: собирает телеметрию BMS разных типов и публикует её в shm
(бинарный контракт 107 Б для web) и в raw-tsdb (через `tscontract`, `device_type=bms`).

- Архитектура (заморожена): `docs/bms-reader-arch.md`.
- Контракт/циклы: `docs/bms-reader.md` (§1 — контракт, §2 — probe, §3 — пулер).
- Типы: `docs/bms/README.md`, `docs/bms/<TYPE>.md`.
- План: `docs/bms-reader-plan.md`.

## Состав

| Файл | Назначение |
|---|---|
| `main.go` | запуск, обработка сигналов (SIGHUP→reload) |
| `config.go` | `bms-gateways.json` / `bms-ident.json` (атомарная запись) |
| `shm.go` | раскладка сегмента: список (Items) + телеметрия 107 Б (Slots) |
| `control.go` | control-сокет: `ping`/`list`/`reload` |
| `transport.go` | Transport: Serial (`lib/transport`) и TCP |
| `module.go` | интерфейс типа BMS (`Probe`/`Poll`/`Speeds`) |
| `antbms.go` | тип AntBMS-DSP (пассив, 19200, кадр 140 Б) |
| `enbms.go`, `pace.go` | тип EnBMS RS485 (PACE ASCII, ADR 0..15, SN `0xA4`) |
| `orchestrator.go` | пул источников, backoff/баны, рескан, пулеры |
| `registry.go` | реестр ID (`BASE-NNNNN`, привязка) |
| `publish.go` | shm 107 Б + raw (`tscontract`/`stat`) |

## Запуск

```
make -C daemons/bms-reader arm      # ARMv7 (устройство)
make -C daemons/bms-reader host     # локально
go test ./daemons/bms-reader/...    # unit + e2e (TCP-эмулятор EnBMS)
```

Systemd: `bms-reader.service` → `malina-bms-reader.service`; `ExecReload` — SIGHUP
(перечитать `bms-gateways.json`).

## Пути (устройство)

- shm: `/dev/shm/malina-bms-state`
- control: `/run/malina-bms/control.sock`
- конфиги: `/malina/etc/malina/bms-gateways.json`, `bms-ident.json`
- raw tsdb: `/run/malina-ts-raw/tsdb.sock`

Ограничения: только **чтение** BMS (RO); TCP-шлюзы — только по явной настройке.
