# API: native (lists, raws, запись)

Документ для разработчиков клиентов. Описывает собственный HTTP-API `malina-web`:
авторизацию, списки устройств, сырые снимки (`raw`), запись в устройства и
статистику. Legacy-совместимость — [api-legacy.md](api-legacy.md).

- База: `http://<адрес-устройства>`.
- Все ответы, кроме `raw`, — JSON (`application/json; charset=utf-8`).
- `raw` — бинарные данные (`application/octet-stream`) по контрактам
  `map-reader`/`kes-reader`/`bms-reader`.
- Версионирование: контракт `raw` фиксирован magic/version в заголовке; списки
  версионируются полями, обратная совместимость не гарантируется при смене
  контракта.

## 1. Авторизация

Единый механизм: **PAM-пароль пользователя `malina`** (тот же, что для веб и SSH).
Ролей нет: любой валидный токен = полный доступ.

Поддерживаются два способа:

1. **Cookie-сессия** `malina_session` — для браузера. `HttpOnly`, `SameSite=Lax`,
   TTL 1 час, обновляется при каждом запросе.
2. **Bearer-токен** — для программных клиентов.

### 1.1. Получение токена

```http
POST /api/token
Content-Type: application/json

{"password": "ПАРОЛЬ"}
```

Успех:

```json
{
  "token": "64-символьная hex-строка",
  "expires": 1792358872
}
```

`expires` — unix-время, до которого токен будет жить **при простое**. Это не
жёсткий дедлайн: TTL скользящий (см. §1.3).

Ошибки:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Использование

```http
Authorization: Bearer <token>
```

```bash
TOKEN=$(curl -s -X POST http://10.10.100.254/api/token \
  -H 'Content-Type: application/json' \
  -d '{"password":"ПАРОЛЬ"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://10.10.100.254/api/mainlist'
```

### 1.3. Скользящий TTL и отзыв

- Токен живёт **1 час с последнего использования**, а не с выпуска.
- Любой авторизованный запрос обновляет `lastUsed`.
- Абсолютного максимума нет.
- Хранится только `sha256` токена в `/dev/shm/malina-auth`; при перезагрузке все
  токены исчезают, при перезапуске `malina-web` — сохраняются.
- Отзыв своего токена:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Смена пароля отзывает **все** сессии и токены.
- `GET /api/token` не поддерживается → `405`.

### 1.4. Ошибки авторизации

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

Для HTML-страниц вместо 401 используется редирект на `/login`.
Заголовок `WWW-Authenticate` выставляется на `/api/token`; часть старых
API-хендлеров (например, `/api/mainlist`) отвечает «голым» `401` без тела — клиент
должен опираться на статус.

## 2. Списки устройств (lists)

Общая идея: клиент получает список устройств, берёт пару **`id` + `gen`** и
запрашивает по ней `raw`. `id` — индекс слота; `gen` — «поколение аренды» слота.
Пара однозначна во времени: после переподключения устройства `gen` меняется, и
старый `raw` вернёт `404`.

### 2.1. `GET /api/mainlist`

Сводка по всем устройствам для главной страницы. Под авторизацией.

```json
{
  "updated": 1792355272,
  "maps": [
    {
      "id": 0,
      "gen": 1792353796794,
      "title": "Dominator · 00000 · /dev/ttyUSB2 · 57600",
      "mode": "MAP on. Pass-through + charge. Charge stage 1",
      "mode_num": 6,
      "soc": 82.20625,
      "grid": {
        "u": {"v": 218, "nd": false},
        "f": {"v": 50, "nd": false},
        "i": {"v": 25, "nd": false},
        "p": {"v": 5450, "nd": false},
        "t": {"v": 59, "nd": false}
      },
      "batt": {
        "u": {"v": 54.3, "nd": false},
        "f": {"v": 0, "nd": false},
        "i": {"v": -93.375, "nd": false},
        "p": {"v": -5070.2625, "nd": false},
        "t": {"v": 27, "nd": false}
      },
      "load": {
        "u": {"v": 215, "nd": false},
        "f": {"v": 50, "nd": false},
        "i": {"v": 1.7662, "nd": false},
        "p": {"v": 379.7375, "nd": false},
        "t": {"v": 68, "nd": false}
      },
      "online": true
    }
  ],
  "kes": [],
  "bms": [],
  "grid": []
}
```

Схема:

| Поле | Тип | Смысл |
|---|---|---|
| `updated` | int64 | unix-время сборки ответа |
| `maps[]` | array | плитки МАП |
| `kes[]` | array | плитки КЭС |
| `bms[]` | array | плитки BMS |
| `grid[]` | array | плитки сетевых инверторов |

### 2.2. Плитка МАП (`maps[]`)

| Поле | Тип | Смысл |
|---|---|---|
| `id` | int | индекс слота |
| `gen` | uint64 | поколение слота (для `/api/map/raw`) |
| `title` | string | локализованная подпись |
| `mode` | string | текст режима |
| `mode_num` | uint32 | композитный код 0..18 |
| `soc` | float/null | заряд АКБ, % |
| `grid`, `batt`, `load` | block | блоки по 5 значений |
| `online` | bool | `lastSeen` не старше 2 минут |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — значение в единицах СИ: В, А, Вт, Гц, °C, %.
- `nd: true` — данных нет, показывать «—».
- Сервер отдаёт **сырые** значения без округления; округление и раскраска — на
  клиенте.
- Для `batt` МАП **отрицательный** ток — заряд, **положительный** — разряд (см. пример выше: режим заряда, `i = −93.375`).

### 2.3. Плитка КЭС (`kes[]`)

```json
{
  "id": 0,
  "gen": 1792312122696,
  "title": "KES · 00000 · /dev/ttyUSB10 · 115200",
  "mode": "b-R",
  "soc": 96.69375,
  "panel": {
    "u": {"v": 103.88, "nd": false},
    "f": {"v": 0, "nd": false},
    "i": {"v": 5.7, "nd": false},
    "p": {"v": 592, "nd": false},
    "t": {"v": 14, "nd": false}
  },
  "batt": {
    "u": {"v": 57.11, "nd": false},
    "f": {"v": 0, "nd": false},
    "i": {"v": 10.16, "nd": false},
    "p": {"v": 580, "nd": false},
    "t": {"v": 2, "nd": false}
  },
  "online": true
}
```

`panel` — панели, `batt` — АКБ. `f` для КЭС всегда `0/nd`.

### 2.4. Плитка BMS (`bms[]`)

```json
{
  "id": 0,
  "gen": 1792312123347,
  "title": "AntBMS-DSP-000",
  "serial": "AntBMS-DSP-000",
  "mode": "Discharge",
  "soc": 25,
  "pack": {
    "u": {"v": 52.11, "nd": false},
    "f": {"v": 0, "nd": true},
    "i": {"v": 4.8, "nd": false},
    "p": {"v": 203, "nd": false},
    "t": {"v": 27, "nd": false}
  },
  "online": true,
  "cell_count": 16,
  "cells_mv": [3258, 3259, 3252, 3254],
  "cell_min": 3252,
  "cell_max": 3260,
  "cell_diff": 8,
  "balance_mask": 0,
  "cell_warn_mask": 0,
  "cell_prot_mask": 0,
  "bms_fault": false
}
```

- `pack.i` — ток со знаком: разряд `+`, заряд `−`.
- `cells_mv` — массив длиной `cell_count` (в примере сокращён).
- `cell_warn_mask`/`cell_prot_mask` — битовые маски предупреждений/защит по ячейкам.

### 2.5. Плитка инвертора (`grid[]`)

```json
{
  "id": 8,
  "gen": 1,
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "Гараж",
  "type": "deye",
  "online": true,
  "status": 1,
  "status_text": "OK",
  "inverter_status": 2,
  "inverter_status_text": "normal",
  "alarm": false,
  "pv_voltage": {"v": 63.2, "nd": false},
  "pv_current": {"v": 0, "nd": false},
  "pv_power": {"v": 0, "nd": false},
  "ac_active": {"v": 0, "nd": false},
  "ac_reactive": {"v": 80, "nd": false},
  "frequency": {"v": 49.98, "nd": false},
  "l1_voltage": {"v": 229.9, "nd": false},
  "l2_voltage": {"v": 0, "nd": false},
  "l3_voltage": {"v": 0, "nd": false},
  "current_sum": {"v": 0.4, "nd": false},
  "temp1": {"v": 0, "nd": true},
  "temp2": {"v": 22, "nd": false},
  "energy_today": {"v": 7.9, "nd": false},
  "energy_total": {"v": 5693.3, "nd": false},
  "last_seen": 1792355262
}
```

`status` — статус **связи** (`OK`/нет связи), `inverter_status` — состояние работы
инвертора.

### 2.6. Прочие списки

| Эндпоинт | Ответ |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

Пример `/api/map/devices`:

```json
[
  {
    "type": "map",
    "id": 0,
    "gen": 1792353796794,
    "serial": "00000",
    "label": "Dominator · 00000 · /dev/ttyUSB2 · 57600"
  }
]
```

Пример `/api/bms/devices`:

```json
[
  {
    "type": "bms",
    "id": 0,
    "gen": 1792312123347,
    "serial": "AntBMS-DSP-000",
    "base": "AntBMS-DSP-000",
    "bms_type": "AntBMS-DSP",
    "bms_type_label": "ANT BMS",
    "source": "serial:/dev/ttyUSB0",
    "framing": 0,
    "last_seen": 1792355272
  }
]
```

`/api/settings/status`:

```json
{
  "type": "map",
  "updated": 1792355300,
  "rows": [
    {
      "id": 0,
      "gen": 1792353796794,
      "port": "/dev/ttyUSB2",
      "model": "D",
      "protocol": "microart",
      "unit": 0,
      "baud": 57600,
      "serial": "00000",
      "serial_letter": "c",
      "state": 1,
      "last_seen": 1792355299
    }
  ]
}
```

`state=1` — устройство было на связи не позже 60 секунд назад.

## 3. Сырые снимки (raw)

`raw` — бинарный снимок последнего успешного опроса. Клиент обязан проверять
`id`+`gen` по списку; иначе можно получить чужой/устаревший слот.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Размер payload — `64 + ceil(402/8) + 402×4 = 1723` байта (при `nfields=402`).
Ниже — фрагмент (первые 80 байт).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Заголовок payload:

| Смещение | Размер | Поле | Значение |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (текущая) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-терминированная строка |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` Микроарт, `1` Modbus |
| 42 | 1 | `unit` | адрес Modbus |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | поколение слота |
| 56 | 8 | `lastSeen` | unix-время |
| 64 | `ceil(n/8)` | `presence` | битовая карта наличия поля |
| `64+presence` | `n×4` | `values[]` | `uint32` LE на каждое поле |

- `values[i]` — **сырое** значение поля `id=i`. Масштаб — на клиенте.
- `presence` — бит `i` = 1, если поле реально прочитано; для отсутствующих
  значений обычно `0xFFFFFFFF` в `values`, но полагаться только на это нельзя.
- Полный список 402 полей (`id`, имя, адрес, размер) — `docs/map-contract.md`.
- Ключевые формулы — §3.1.1.

#### 3.1.1. Масштабы ключевых полей

| Поле контракта | id | Формула |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` В |
| `MODE` | 36 | композитный режим (см. legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` В |
| `INET` | 148 | А, магнитуда |
| `TFNET` | 150 | `6250/V` Гц |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` А; знак: `PNET_Sign_P==0` → отдача (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` А; знак зависит от `MODE_RAM` |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` А; знак — старший бит `H` |
| `PNET_8` | 387 | `int16(...) × 10` Вт (знаковый) |

#### 3.1.2. Декодер на Python

```python
import struct, urllib.request

def decode_map(payload: bytes):
    magic, ver, n = struct.unpack_from("<IHH", payload, 0)
    assert magic == 0x4441504D and ver == 5
    port = payload[8:40].split(b"\0", 1)[0].decode()
    model, proto, unit = payload[40], payload[41], payload[42]
    gen, last = struct.unpack_from("<QQ", payload, 48)
    plen = (n + 7) // 8
    presence = payload[64:64 + plen]
    vals = struct.unpack_from(f"<{n}I", payload, 64 + plen)
    return {"port": port, "model": chr(model), "protocol": proto,
            "unit": unit, "gen": gen, "last": last,
            "presence": presence, "values": vals}

data = urllib.request.urlopen(
    "http://10.10.100.254/api/map/raw?id=0&gen=1792353796794",
    # Authorization: Bearer ...
).read()
m = decode_map(data)
print(m["port"], m["gen"], m["values"][4])  # UACC (id=4)
```

### 3.2. `GET /api/kes/raw?id=&gen=`

```http
GET /api/kes/raw?id=0&gen=1792312122696
Content-Type: application/octet-stream
```

Payload — **1536 байт**: три под-блока по 512 байт `[KESE][KESR][KESS]`.
Ниже — фрагмент (первые 32 байта).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, паспорт):

| off | поле | тип | смысл |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | серийник |
| 8 | `Device` | u8 | 5 для КЭС |
| 9 | `Pow` | u8 | мощность/тип |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | Вт = значение×100 |
| 12 | `ModelVolt` | u8 | В = значение×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, телеметрия, `off` относительно начала блока):

| off | поле | тип | смысл |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix-время |
| 14 | `RelayC` | u8 | биты реле 1/2/3 |
| 15 | `RSErrSis` | u8 | ошибки контроллера |
| 16 | `I2CErr` | u8 | ошибка I²C |
| 18 | `VcPV` | u16 LE | напряжение панелей ×100 |
| 20 | `IcPV` | u16 LE | ток панелей ×100 |
| 22 | `VBat` | u16 LE | напряжение АКБ ×100 |
| 24 | `PPV` | u16 LE | мощность панелей, Вт |
| 26 | `POut` | u16 LE | мощность выхода, Вт |
| 28 | `PLoad` | u16 LE | мощность нагрузки, Вт |
| 30 | `PCurr` | u16 LE | мощность заряда, Вт |
| 32 | `ICh` | u16 LE | ток заряда ×100 |
| 34 | `IOut` | u16 LE | ток выхода ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | энергия за сутки, Вт |
| 40 | `PwrKW` | u16 LE | энергия за сутки, кВт |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | внешние датчики |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — нет данных |
| 55 | `UakbCell[32]` | u16×32 | напряжения банок |
| 119 | `TakbCell[32]` | u8×32 | температуры банок (`x−50`) |
| 151 | `QCell[32]` | u16×32 | ток разряда банок |
| 215 | `ErrCells[32]` | u8×32 | ошибки BMS |
| 247 | `IAkbMPPT[16]` | u16×16 | ток параллельных MPPT |
| 279 | `ModeMPPT[16]` | u8×16 | режим MPPT |
| 295 | `TakbMPPT[16]` | u8×16 | температура MPPT |
| 311 | `ErrMPPT[16]` | u8×16 | ошибки MPPT |

`KESS` (настройки, 40×u16 LE, `off = 5 + 2×i`, статус RFC) — полный список в
`docs/kes-contract.md` §`KESS`.

Суммарная энергия за сутки: `PwrKW×1000 + PwrW`.

#### 3.2.1. Декодер на Python

```python
import struct

def decode_kes(payload: bytes):
    assert payload[0:4] == b"KESE" and payload[512:516] == b"KESR"
    serial = struct.unpack_from("<H", payload, 6)[0]
    fw = payload[10]
    relay = payload[512 + 14]
    vcpv = struct.unpack_from("<H", payload, 512 + 18)[0] / 100.0
    vbat = struct.unpack_from("<H", payload, 512 + 22)[0] / 100.0
    ppv = struct.unpack_from("<H", payload, 512 + 24)[0]
    t_int = payload[512 + 36] - 50
    pwr_w = struct.unpack_from("<H", payload, 512 + 38)[0]
    pwr_kw = struct.unpack_from("<H", payload, 512 + 40)[0]
    return {"serial": serial, "fw": (fw & 0x0F, fw >> 5), "relay": relay,
            "vcpv": vcpv, "vbat": vbat, "ppv": ppv, "t_int": t_int,
            "energy_wh": pwr_kw * 1000 + pwr_w}
```

### 3.3. `GET /api/bms/raw?id=&gen=`

```http
GET /api/bms/raw?id=0&gen=1792312123347
Content-Type: application/octet-stream
```

Payload — ровно **107 байт**, многобайтовые поля — **big-endian**.
Ниже — фрагмент (первые 80 байт; остальные 27 — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | размер | поле | тип | единица |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 заряд MOS, b1 разряд MOS, b2 балансировка |
| 4 | 32 | `cells_mv[16]` | u16×16 | мВ |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 А, разряд `+` |
| 56 | 4 | `power_w` | i32 | Вт |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 А·ч |
| 68 | 4 | `remaining_mah` | u32 | 0.001 А·ч |
| 72 | 4 | `rated_mah` | u32 | 0.001 А·ч |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 В |
| 80 | 2 | `port_cv` | u16 | 0.01 В |
| 82 | 4 | `balance_mask` | u32 | маска |
| 86 | 2 | `mode` | u16 | младший байт — enum |
| 88 | 19 | `alarm` | u8×19 | битфилд, раскладка по типу |

Канонический `mode`: `0` ожидание, `1` разряд, `2` заряд, `3` поддержание,
`4` полный заряд, `5` выключение.

`alarm` — 152 бита; раскладка зависит от типа BMS (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` задают число значащих элементов; хвосты массивов
игнорируются. Производные (min/max/avg) клиент считает сам.

#### 3.3.1. Декодер на Python

```python
import struct

def decode_bms(p: bytes):
    assert len(p) == 107
    cell_count, temp_count, flags = struct.unpack_from(">BBH", p, 0)
    cells = list(struct.unpack_from(">16H", p, 4))[:cell_count]
    temps = list(struct.unpack_from(">8h", p, 36))[:temp_count]
    cur = struct.unpack_from(">i", p, 52)[0] / 100.0
    power = struct.unpack_from(">i", p, 56)[0]
    soc = struct.unpack_from(">H", p, 60)[0] / 10.0
    soh = struct.unpack_from(">H", p, 62)[0] / 10.0
    cap = struct.unpack_from(">I", p, 64)[0] / 1000.0
    remain = struct.unpack_from(">I", p, 68)[0] / 1000.0
    cycles = struct.unpack_from(">H", p, 76)[0]
    total = struct.unpack_from(">H", p, 78)[0] / 100.0
    port = struct.unpack_from(">H", p, 80)[0] / 100.0
    mode = struct.unpack_from(">H", p, 86)[0] & 0xFF
    return {"cells_mv": cells, "temps_c": [t / 10 for t in temps],
            "current_a": cur, "power_w": power, "soc": soc, "soh": soh,
            "cap_ah": cap, "remaining_ah": remain, "cycles": cycles,
            "total_v": total, "port_v": port, "mode": mode,
            "flags": flags, "alarm": p[88:107]}
```

### 3.4. `GET /api/grid/device?serial=`

JSON-документ инвертора: поля уже масштабированы и подписаны ключами i18n.

```json
{
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "Гараж",
  "type": "deye",
  "kind": 0,
  "gen": 1,
  "status": 1,
  "status_text": "OK",
  "inverter_status": 2,
  "inverter_status_text": "normal",
  "last_seen": 1792355302,
  "energy_today": "7.9",
  "energy_total": "5693.3",
  "energy_today_nd": false,
  "energy_total_nd": false,
  "fields": [
    {"id": 1, "name_key": "stats.f.grid.pv1_voltage",
     "desc_key": "stats.d.grid.pv1_voltage", "value": "63.2",
     "unit_key": "home.grid.v", "nd": false}
  ],
  "alarm_words": [],
  "alarms": []
}
```

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, частота,
  L1/L2/L3 (U/I), температуры 1/2, состояние, аварийные слова 0..5.
- Значения — строки, уже с масштабом; `nd` — нет данных.
- `alarm_words` — hex-слова, `alarms` — расшифрованные коды.

## 4. Запись в устройства

Запись доступна по тому же токену/cookie. Ниже — только контракты, нужные клиенту;
внутренние детали — в `docs/map-write-api.md` и `docs/kes-reader.md`.

### 4.1. `POST /api/map/write`

Авторизация: cookie или Bearer. Тело — до 64 КиБ.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **только один тип**: либо `reg`, либо `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` в hex без
  `0x`; `addr ≤ 3FF` — EEPROM, `≥400` — RAM; до 256 байт (Modbus — до 250).
- `cmd`: `{"op":"cmd","code":"07"}` — ровно одна команда. Коды: `1` OFF, `2` ON,
  `3` EEPROM WR (служебная), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (безопасная проверка), `8` StatReset, `9` DischOff.
- Максимум 64 операции.
- `no_frame: true` — не обрамлять служебными командами (клиенту обычно не нужно).

Ответ:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP-статусы: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Тело — как у control-сокета `kes-reader` (`docs/kes-reader.md` §4.2.3), web
добавляет `cmd:"write"` и проксирует.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — только `op=reg`; операции `relay` нет.
- `addr`/`data` — hex без `0x`; `addr` 0x0000..0xFFFF; `data` — 1..256 байт.
- Максимум 64 операции.
- Реле КЭС — байт-маска в ячейке `0x0E3D` (биты 0/1/2); клиент делает RMW сам.
- Ответ — JSON `kes-reader`; HTTP-статус по коду: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Статистика

Все эндпоинты под авторизацией. Время — **unix-миллисекунды**.

| Эндпоинт | Параметры | Ответ |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | то же, окно «последние N минут» |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (в списке статистики).
- `id` — строковый идентификатор устройства (серийник/логический ID).
- `source`: `raw` или `avg` (для `series`).
- `follow=1` — режим «ОНЛАЙН», источник всегда `raw`, `depth` — минуты (1..10,
  по умолчанию 5); `after` — догрузка только новых точек.
- `table`: `dir=from|to`, `limit` 1..2000 (по умолчанию 200), `detail=1` —
  включить детальные строки, `rawmin` — минимальное время raw.

Пример `series`:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

Ключи `series` — **числовые `field_id` контракта телеметрии** в виде строк
(`"1"` = `UNET`, `"2"` = `INET`), а не адреса/ID ячеек `map-contract` и не имена
полей. Имена и единицы — в `columns` таблицы. Полный список `field_id` по типам —
`docs/stat.md`. Параметр `fields` принимает те же числовые id через запятую;
пустой `fields` — все поля.

Пример `table`:

```json
{
  "status": 0,
  "columns": [
    {"id": "1", "name": "UNET", "type": 4, "unit": "В",
     "raw": false, "desc": "stats.f.map.UNET"},
    {"id": "2", "name": "INET", "type": 4, "unit": "А",
     "raw": false, "desc": "stats.f.map.INET"}
  ],
  "rows": [
    {"t": 1792353001000, "v": 1, "f": {"1": 218.0, "2": 25.0}}
  ]
}
```

- `columns[]` — объекты `statsCol`; `id` — `field_id` (порядковый id контракта
  телеметрии), `type` — код типа значения, `name` — имя поля.
- `rows[]` — объекты `statsRow`: `t` — ms, `v` — версия контракта, `f` — значения
  по `field_id` (ключи строковые).

## 6. Ошибки

| Код | Тело | Когда |
|---|---|---|
| `400` | `{"error":"bad_request"}` | кривой JSON/параметры |
| `401` | `{"error":"unauthorized"}` | нет/просрочен токен |
| `404` | `{"error":"..."}` / пусто | нет устройства/слота, `id+gen` не совпал |
| `405` | — | неверный метод |
| `422` | `{"ok":false,"code":"..."}` | ошибка устройства |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | устройство не ответило |
| `500` | `{"ok":false,"code":"internal"}` | внутренняя ошибка |

## 7. Рекомендуемый цикл клиента

1. `POST /api/token` → токен.
2. `GET /api/<type>/devices` (или `/api/mainlist`) → список `id/gen`.
3. `GET /api/<type>/raw?id&gen` → бинарный снимок. Для `grid` raw-эндпоинта нет:
   снимок берётся через `GET /api/grid/device?serial=`.
4. Декодировать по контракту; при `404` — перечитать список.
5. Для истории — `/api/stats/devices` и `/api/stats/series|table`.
6. Для записи — `POST /api/map/write` или `/api/kes/write` с тем же `id/gen`.

## 8. Ссылки

- [api-legacy.md](api-legacy.md) — legacy-совместимость.
- `docs/api-auth.md` — авторизация и хранилище токенов.
- `docs/map-contract.md` — полный контракт МАП (402 поля).
- `docs/kes-contract.md` — контракт КЭС (KESE/KESR/KESS).
- `docs/bms-reader.md` — контракт BMS (107 байт).
- `docs/grid-reader.md` — инверторы.
- `docs/map-write-api.md` — запись в МАП.
- `docs/kes-reader.md` — запись в КЭС.
