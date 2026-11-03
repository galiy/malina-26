# Сводка: restore-menu

- Старт: 2026-11-03T09:38:22+03:00
- Конец: 2026-11-03T09:39:27+03:00
- Событий: 6041
- TX (host→МАП): 3785 Б
- RX (МАП→host): 3786 Б
- Длительность по событиям: 20.765 с

## CTRL-события

| Время MSK | Событие |
|---|---|
| 09:38:32.677 | SET_LATENCY: latency_timer=1 мс |
| 09:38:34.772 | RESET: wValue=0x0000 wIndex=0x0000 len=0 |
| 09:38:34.773 | SET_DATA: 8 бит, parity=none, stop=1, break=0 (wValue=0x0008) |
| 09:38:34.775 | SET_BAUD: ~19200 бод (divisor=156.25, wValue=0x809C) |
| 09:38:34.777 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:38:34.779 | MODEM_CTRL: wValue=0x0303 (DTR/RTS/break) |
| 09:38:34.780 | GET_MODEM_STATUS → 2 Б |
| 09:38:34.782 | SET_BAUD: ~19200 бод (divisor=156.25, wValue=0x809C) |
| 09:38:34.782 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:38:34.783 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:38:34.784 | GET_MODEM_STATUS → 2 Б |
| 09:38:34.786 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 09:38:34.786 | GET_MODEM_STATUS → 2 Б |
| 09:38:34.788 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 09:38:34.788 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:38:34.789 | SET_FLOW_CTRL: wValue=0x0000 wIndex=0x0000 |
| 09:38:34.790 | GET_MODEM_STATUS → 2 Б |
| 09:38:34.792 | MODEM_CTRL: wValue=0x0300 (DTR/RTS/break) |
| 09:38:34.792 | GET_MODEM_STATUS → 2 Б |
| 09:38:34.794 | MODEM_CTRL: wValue=0x0302 (DTR/RTS/break) |
| 09:38:53.442 | GET_MODEM_STATUS → 2 Б |

## Паузы ≥ 50 мс (18)

| После, с | Пауза, мс |
|---|---|
| 0.000 | 2095.0 |
| 2.999 | 300.9 |
| 3.402 | 301.3 |
| 3.802 | 301.0 |
| 4.190 | 300.7 |
| 4.590 | 300.5 |
| 4.991 | 315.7 |
| 5.405 | 300.9 |
| 5.788 | 300.5 |
| 6.191 | 301.2 |
| 6.591 | 301.0 |
| 6.990 | 300.7 |
| 7.373 | 301.4 |
| 7.777 | 301.1 |
| 8.177 | 301.0 |
| 8.574 | 105.8 |
| 9.374 | 121.5 |
| 9.513 | 11251.9 |

## Начало TX (первые 40 байт)

`72 7F 00 00 0F 0A 6F 03 8D 1E 00 C8 07 02 01 E1 01 00 00 0D 05 04 00 00 11 01 9D 00 00 02 10 8D 90 77 AA 79 5D 00 28 11`<br>`r.....o..........................w.y].(.`
