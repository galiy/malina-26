# Сводка: flash

- Старт: 2026-10-29T18:57:19+03:00
- Конец: 2026-10-29T19:04:46+03:00
- Событий: 821324
- TX (host→МАП): 410652 Б
- RX (МАП→host): 410661 Б
- Длительность по событиям: 414.335 с

## CTRL-события

| Время MSK | Событие |
|---|---|
| 18:57:34.085 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.085 | RESET: wValue=0x0000 wIndex=0x0000 len=0 |
| 18:57:34.086 | SET_DATA: 8 бит, parity=none, stop=1, break=0 (wValue=0x0008) |
| 18:57:34.088 | SET_BAUD: ~19200 бод (divisor=156.25, wValue=0x809C) |
| 18:57:34.090 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 18:57:34.092 | MODEM_CTRL: wValue=0x0303 (DTR/RTS/break) |
| 18:57:34.093 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.095 | SET_BAUD: ~631579 бод (divisor=4.75, wValue=0x8004, wIndex=0x0001); номинал 625000 (вендор: _RSBAUD_Rasb_Load=0x64) |
| 18:57:34.095 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 18:57:34.096 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 18:57:34.097 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.099 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 18:57:34.099 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.101 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 18:57:34.101 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 18:57:34.102 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 18:57:34.103 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.105 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 18:57:34.105 | GET_MODEM_STATUS → 2 Б |
| 18:57:34.107 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 19:04:28.420 | GET_MODEM_STATUS → 2 Б |

## Паузы ≥ 50 мс (3)

| После, с | Пауза, мс |
|---|---|
| 0.023 | 97.0 |
| 0.120 | 270.0 |
| 0.402 | 2959.0 |

## Начало TX (первые 40 байт)

`6C 72 75 6E 55 0E 22 98 53 EA AC 7F D8 03 4C 73 72 02 A2 9B 29 8B F3 D7 C6 7D 85 60 05 B6 AF 04 27 28 33 C3 CE 14 A9 82`<br>`lrunU.".S.....Lsr...)....}.`....'(3.....`
