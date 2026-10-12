# BMS API (read_bms): контракт и реализованная логика

Статус: **реализовано в `malina-web`** (`/read_bms.php`). Источник контракта —
**реверс бэкапа** (`html/read_bms.php` + строки формата бинаря `/usr/sbin/bmslistener`).
Связано: `docs/legacy-api.md` (список legacy-эндпоинтов), `docs/legacy-endpoints.md`
(инвентаризация), `docs/legacy/batmon-api.md` (АКБ/BMS-эмуляция), `docs/legacy/master-node.md`.

## 1. Назначение

`read_bms.php` — legacy-ручка снимков **ANT BMS** (батарея, пассивный слушатель
`bmslistener`). На нашем узле **собственного демона `bmslistener` нет** (и
внешнего ANT BMS-модуля нет), но снимок МАП содержит данные АКБ (банки BMS,
напряжение/ток, SoC). Поэтому ручка считается **по требованию** из текущего
снимка МАП и отдаёт JSON в формате `bmslistener`.

## 2. Legacy-источник (`.68`)

`read_bms.php`:

```php
$shm = shmop_open(2018,"a",0,0);                    // сегмент 32 КБ
if ($shm===false) { echo '{"updated":0,"devices":[]}'; die(); }
$str_json = shmop_read($shm,0,32768);
$pos = strpos($str_json,"#EOF");
if ($pos===false || $pos<=0) { echo '{"updated":0,"devices":[]}'; die(); }
echo substr($str_json,0,$pos);
```

- **shm `2018`** = сегмент CHARGER/BMS (в `read_bms.php` — 32768 Б, в `emd.pl` — 1024 Б);
- контент — JSON `{"updated":<epoch>,"devices":[...]}` + терминатор `#EOF`;
- при недоступности сегмента/пустоте — `{"updated":0,"devices":[]}`.

Кто пишет shm 2018 — демон **`bmslistener`** (Zig-бинарь на `.68`, слушает
USB-serial адаптеры с ANT BMS live-кадрами 19200 8N1). Формат устройства
извлечён из строк бинаря (`strings`):

```json
{"updated":%ld,"devices":[<sep>[{"deviceName":"AntBms %d A/h (%s)","port":"%s",
 "timestamp":%ld,"time":"%s","cell_count":%d,"cells_v":[<%.3f,...>],
 "current_a":%.2f,"soc":%d,"capacity_ah":%.2f,"remaining_ah":%.2f,
 "temperatures_c":[<%.0f,...>],"charge_mos":%d,"discharge_mos":%d,"balancer":%d,
 "power_w":%.1f,"max_cell_idx":%d,"max_cell_v":%.3f,"min_cell_idx":%d,
 "min_cell_v":%.3f,"avg_cell_v":%.3f,"frames":%u}]}
```

## 3. Наш контракт (`/read_bms.php`, GET)

- Открытый (как `read_json.php`/`check_daemons.php`), **без авторизации**.
- **Stateless**: считает по требованию из текущего снимка МАП (первый доступный),
  без накопителей и состояния (в отличие от `read_json.php`).
- Доступность ручки для внешних потребителей — флаг `bms` в `check_daemons.php`
  (`0` — есть BMS-банки в снимке МАП, `1` — нет).
- `Content-Type: text/html; charset=UTF-8` (+ `Cache-Control: no-cache`), как в legacy.
- Устройство **одно** — АКБ, подключённая к выбранному МАП. Источник — снимок МАП:
  - **банки BMS** (`cells_v`/`cell_count`/`temperatures_c`) — ячейки
    `UakbCell_L(H)_<i>` (В, ×0,01, маска старшего бита) и `TakbCell_<i>`
    (℃, `raw−50`; `raw==255` → `127` «нет датчика»), число — `BMS_Num`,
    только при `LCD_BMSFunk ∈ {1,3}`;
  - **`soc` (%)** — приоритет аппаратному полю `SoC` (0..100), иначе OCV-оценка
    (см. `web/internal/web/ocv.go`, кривые/номинал/ток), как в `batmon`;
  - **`current_a`** — `IAcc_med_A_u16`, знак по режиму (`MODE` 4..9 = заряд → `+`);
  - **`capacity_ah`** — полная ёмкость АКБ из настроек МАП: `(LCD_CAcc*25) >> UACC`;
  - **`remaining_ah`** — `capacity_ah * soc / 100`;
  - **`power_w`** — `U_акб * current_a`;
  - **`max/min/avg_cell_v`** и их индексы — по банкам `cells_v` (0 при отсутствии банок).
- Поля, которых нет в снимке МАП: `charge_mos`, `discharge_mos`, `balancer`,
  `frames` — `0`; `deviceName` — `AntBms <capacity_ah> A/h (<модель МАП>)`;
  `port` — порт МАП; `timestamp`/`time` — из времени снимка (`LastSeen`).
- При отсутствии МАП/снимка — `{"updated":0,"devices":[]}`.

### 3.1 Пример

```
GET /read_bms.php
{"updated":1700000000,"devices":[{"deviceName":"AntBms 500 A/h (Титанатор)",
 "port":"/dev/ttyAMA0","timestamp":1700000000,"time":"18:53:20","cell_count":16,
 "cells_v":[3.200,3.400,"..."],"current_a":12.50,"soc":80,"capacity_ah":500.00,
 "remaining_ah":400.00,"temperatures_c":[25,27,"..."],"charge_mos":0,
 "discharge_mos":0,"balancer":0,"power_w":256.2,"max_cell_idx":3,"max_cell_v":3.410,
 "min_cell_idx":4,"min_cell_v":3.190,"avg_cell_v":3.300,"frames":0}]}
```

## 4. Отличия от `.68`

| Аспект | Legacy (`bmslistener`) | Наш `/read_bms.php` |
|---|---|---|
| Источник | shm 2018 (демон `bmslistener`) | снимок МАП (по требованию) |
| Устройства | по числу активных ANT BMS адаптеров | одно (АКБ выбранного МАП) |
| Состояние | демон пишет в shm непрерывно | нет, stateless-расчёт на каждый запрос |
| `updated` | время записи демона | `LastSeen` снимка МАП |
| `charge_mos`/`discharge_mos`/`balancer`/`frames` | реальные из кадра | `0` (в снимке МАП нет) |

## 5. Ссылки

- `docs/legacy-api.md` — список реализованных legacy-эндпоинтов.
- `docs/legacy/batmon-api.md` — эмуляция `read_json.php?device=bat`.
- `web/internal/web/legacy.go` (`renderBMS`), `web/internal/web/ocv.go` (оценка SoC).