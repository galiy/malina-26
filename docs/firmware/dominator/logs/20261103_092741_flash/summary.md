# Сводка: flash

- Старт: 2026-11-03T09:27:41+03:00
- Конец: 2026-11-03T09:32:25+03:00
- Событий: 177813
- TX (host→МАП): 88893 Б
- RX (МАП→host): 88903 Б
- Длительность по событиям: 191.748 с

## CTRL-события

| Время MSK | Событие |
|---|---|
| 09:29:01.301 | SET_LATENCY: latency_timer=1 мс |
| 09:29:13.639 | RESET: wValue=0x0000 wIndex=0x0000 len=0 |
| 09:29:13.639 | SET_DATA: 8 бит, parity=none, stop=1, break=0 (wValue=0x0008) |
| 09:29:13.641 | SET_BAUD: ~57279 бод (divisor=52.375, wValue=0xC034) |
| 09:29:13.643 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:29:13.645 | MODEM_CTRL: wValue=0x0303 (DTR/RTS/break) |
| 09:29:13.646 | GET_MODEM_STATUS → 2 Б |
| 09:29:13.648 | SET_BAUD: ~19200 бод (divisor=156.25, wValue=0x809C) |
| 09:29:13.648 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:29:13.649 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:29:13.650 | GET_MODEM_STATUS → 2 Б |
| 09:29:13.652 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 09:29:13.652 | GET_MODEM_STATUS → 2 Б |
| 09:29:13.654 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 09:29:13.654 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:29:13.655 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:29:13.656 | GET_MODEM_STATUS → 2 Б |
| 09:29:13.658 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 09:29:13.658 | GET_MODEM_STATUS → 2 Б |
| 09:29:13.660 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 09:32:13.048 | GET_MODEM_STATUS → 2 Б |

## Паузы ≥ 50 мс (4)

| После, с | Пауза, мс |
|---|---|
| 0.000 | 12337.8 |
| 12.406 | 254.9 |
| 12.679 | 1401.9 |
| 191.584 | 163.3 |

## Начало TX (первые 40 байт)

`6C 72 75 6E 55 00 04 8B 24 F5 EA BB CE 90 2A AD E4 5F 28 17 18 43 9C 90 58 A5 17 82 86 88 BD 7E 52 0E E3 F7 03 77 FC 6D`<br>`lrunU...$.....*.._(..C..X......~R....w.m`
