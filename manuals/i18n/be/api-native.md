# API: native (lists, raws, запіс)

Дакумент для распрацоўнікаў кліентаў. Апісвае ўласны HTTP-API `malina-web`:
аўтарызацыю, спісы прылад, сырыя здымкі (`raw`), запіс у прылады і статыстыку.
Legacy-сумяшчальнасць — [api-legacy.md](api-legacy.md).

- База: `http://<адрас-прылады>`.
- Усе адказы, акрамя `raw`, — JSON (`application/json; charset=utf-8`).
- `raw` — бінарныя даныя (`application/octet-stream`) па кантрактах
  `map-reader`/`kes-reader`/`bms-reader`.
- Версіяванне: кантракт `raw` зафіксаваны magic/version у загалоўку; спісы
  версіянуюцца палямі, зваротная сумяшчальнасць не гарантуецца пры змене
  кантракту.

## 1. Аўтарызацыя

Адзіны механізм: **PAM-пароль карыстальніка `malina`** (той самы, што для вэб і SSH).
Роляў няма: любы валідны токен = поўны доступ.

Падтрымліваюцца два спосабы:

1. **Cookie-сесія** `malina_session` — для браўзера. `HttpOnly`, `SameSite=Lax`,
   TTL 1 гадзіна, абнаўляецца пры кожным запыце.
2. **Bearer-токен** — для праграмных кліентаў.

### 1.1. Атрыманне токена

```http
POST /api/token
Content-Type: application/json

{"password": "ПАРОЛЬ"}
```

Поспех:

```json
{
  "token": "64-сімвальная hex-радок",
  "expires": 1792358872
}
```

`expires` — unix-час, да якога токен будзе жыць **пры бяздзейнасці**. Гэта не
жорсткі дэдлайн: TTL слізгальны (гл. §1.3).

Памылкі:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Выкарыстанне

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

### 1.3. Слізгальны TTL і адкліканне

- Токен жыве **1 гадзіну з апошняга выкарыстання**, а не з выпуску.
- Любы аўтарызаваны запыт абнаўляе `lastUsed`.
- Абсалютнага максімуму няма.
- Захоўваецца толькі `sha256` токена ў `/dev/shm/malina-auth`; пры перазагрузцы ўсе
  токены знікаюць, пры перазапуску `malina-web` — захоўваюцца.
- Адкліканне свайго токена:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Змена пароля адклікае **ўсе** сесіі і токены.
- `GET /api/token` не падтрымліваецца → `405`.

### 1.4. Памылкі аўтарызацыі

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

Для HTML-старонак замест 401 выкарыстоўваецца рэдырэкт на `/login`.
Загаловак `WWW-Authenticate` выстаўляецца на `/api/token`; частка старых
API-хэндлераў (напрыклад, `/api/mainlist`) адказвае «голым» `401` без цела — кліент
павінен абапірацца на статус.

## 2. Спісы прылад (lists)

Агульная ідэя: кліент атрымлівае спіс прылад, бярэ пару **`id` + `gen`** і
запытвае па ёй `raw`. `id` — індэкс слота; `gen` — «пакаленне арэнды» слота.
Пара адназначная ў часе: пасля перападключэння прылады `gen` мяняецца, і
стары `raw` верне `404`.

### 2.1. `GET /api/mainlist`

Зводка па ўсіх прыладах для галоўнай старонкі. Пад аўтарызацыяй.

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

| Поле | Тып | Сэнс |
|---|---|---|
| `updated` | int64 | unix-час зборкі адказу |
| `maps[]` | array | пліткі МАП |
| `kes[]` | array | пліткі КЭС |
| `bms[]` | array | пліткі BMS |
| `grid[]` | array | пліткі сеткавых інвертараў |

### 2.2. Плітка МАП (`maps[]`)

| Поле | Тып | Сэнс |
|---|---|---|
| `id` | int | індэкс слота |
| `gen` | uint64 | пакаленне слота (для `/api/map/raw`) |
| `title` | string | лакалізаваны подпіс |
| `mode` | string | тэкст рэжыму |
| `mode_num` | uint32 | кампазітны код 0..18 |
| `soc` | float/null | зарад АКБ, % |
| `grid`, `batt`, `load` | block | блокі па 5 значэнняў |
| `online` | bool | `lastSeen` не старэйшы за 2 хвіліны |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — значэнне ў адзінках СІ: У, А, Вт, Гц, °C, %.
- `nd: true` — даных няма, паказваць «—».
- Сервер аддае **сырыя** значэнні без акруглення; акругленне і размалёўка — на
  кліенце.
- Для `batt` МАП **адмоўны** ток — зарад, **станоўчы** — разрад (гл. прыклад вышэй: рэжым зараду, `i = −93.375`).

### 2.3. Плітка КЭС (`kes[]`)

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

`panel` — панэлі, `batt` — АКБ. `f` для КЭС заўсёды `0/nd`.

### 2.4. Плітка BMS (`bms[]`)

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

- `pack.i` — ток са знакам: разрад `+`, зарад `−`.
- `cells_mv` — масіў даўжынёй `cell_count` (у прыкладзе скарочаны).
- `cell_warn_mask`/`cell_prot_mask` — бітавыя маскі папярэджанняў/абарон па ячэйках.

### 2.5. Плітка інвертара (`grid[]`)

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

`status` — статус **сувязі** (`OK`/няма сувязі), `inverter_status` — стан працы
інвертара.

### 2.6. Іншыя спісы

| Эндпоінт | Адказ |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement","logger_sn"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

`logger_sn` — настроены серыйны нумар логера (uint32, decimal); не ключ захоўвання; 0 — не зададзена (няма канфігурацыі або shm ад старой канфігурацыі).

Прыклад `/api/map/devices`:

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

Прыклад `/api/bms/devices`:

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

`state=1` — прылада была на сувязі не пазней за 60 секунд таму.

## 3. Сырыя здымкі (raw)

`raw` — бінарны здымак апошняга паспяховага апытання. Кліент абавязаны правяраць
`id`+`gen` па спісе; інакш можна атрымаць чужы/састарэлы слот.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Памер payload — `64 + ceil(402/8) + 402×4 = 1723` байта (пры `nfields=402`).
Ніжэй — фрагмент (першыя 80 байт).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Загаловак payload:

| Зрушэнне | Памер | Поле | Значэнне |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (бягучая) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-тэрмінаваны радок |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` Мікраарт, `1` Modbus |
| 42 | 1 | `unit` | адрас Modbus |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | пакаленне слота |
| 56 | 8 | `lastSeen` | unix-час |
| 64 | `ceil(n/8)` | `presence` | бітавая карта наяўнасці поля |
| `64+presence` | `n×4` | `values[]` | `uint32` LE на кожнае поле |

- `values[i]` — **сырое** значэнне поля `id=i`. Масштаб — на кліенце.
- `presence` — біт `i` = 1, калі поле рэальна прачытана; для адсутных
  значэнняў звычайна `0xFFFFFFFF` у `values`, але абапірацца толькі на гэта нельга.
- Поўны спіс 402 палёў (`id`, імя, адрас, памер) — `docs/map-contract.md`.
- Ключавыя формулы — §3.1.1.

#### 3.1.1. Масштабы ключавых палёў

| Поле кантракту | id | Формула |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` У |
| `MODE` | 36 | кампазітны рэжым (гл. legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` У |
| `INET` | 148 | А, магнітуда |
| `TFNET` | 150 | `6250/V` Гц |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` А; знак: `PNET_Sign_P==0` → аддача (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` А; знак залежыць ад `MODE_RAM` |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` А; знак — старшы біт `H` |
| `PNET_8` | 387 | `int16(...) × 10` Вт (знакавы) |

#### 3.1.2. Дэкодэр на Python

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

Payload — **1536 байт**: тры пад-блокі па 512 байт `[KESE][KESR][KESS]`.
Ніжэй — фрагмент (першыя 32 байты).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, пашпарт):

| off | поле | тып | сэнс |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | серыйны нумар |
| 8 | `Device` | u8 | 5 для КЭС |
| 9 | `Pow` | u8 | магутнасць/тып |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | Вт = значэнне×100 |
| 12 | `ModelVolt` | u8 | У = значэнне×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, тэлеметрыя, `off` адносна пачатку блока):

| off | поле | тып | сэнс |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix-час |
| 14 | `RelayC` | u8 | біты рэле 1/2/3 |
| 15 | `RSErrSis` | u8 | памылкі кантралёра |
| 16 | `I2CErr` | u8 | памылка I²C |
| 18 | `VcPV` | u16 LE | напружанне панэляў ×100 |
| 20 | `IcPV` | u16 LE | ток панэляў ×100 |
| 22 | `VBat` | u16 LE | напружанне АКБ ×100 |
| 24 | `PPV` | u16 LE | магутнасць панэляў, Вт |
| 26 | `POut` | u16 LE | магутнасць выхаду, Вт |
| 28 | `PLoad` | u16 LE | магутнасць нагрузкі, Вт |
| 30 | `PCurr` | u16 LE | магутнасць зараду, Вт |
| 32 | `ICh` | u16 LE | ток зараду ×100 |
| 34 | `IOut` | u16 LE | ток выхаду ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | энергія за суткі, Вт |
| 40 | `PwrKW` | u16 LE | энергія за суткі, кВт |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | знешнія датчыкі |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — няма даных |
| 55 | `UakbCell[32]` | u16×32 | напружанні банок |
| 119 | `TakbCell[32]` | u8×32 | тэмпературы банок (`x−50`) |
| 151 | `QCell[32]` | u16×32 | ток разраду банок |
| 215 | `ErrCells[32]` | u8×32 | памылкі BMS |
| 247 | `IAkbMPPT[16]` | u16×16 | ток паралельных MPPT |
| 279 | `ModeMPPT[16]` | u8×16 | рэжым MPPT |
| 295 | `TakbMPPT[16]` | u8×16 | тэмпература MPPT |
| 311 | `ErrMPPT[16]` | u8×16 | памылкі MPPT |

`KESS` (наладкі, 40×u16 LE, `off = 5 + 2×i`, статус RFC) — поўны спіс у
`docs/kes-contract.md` §`KESS`.

Сумарная энергія за суткі: `PwrKW×1000 + PwrW`.

#### 3.2.1. Дэкодэр на Python

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

Payload — роўна **107 байт**, шматбайтавыя палі — **big-endian**.
Ніжэй — фрагмент (першыя 80 байт; астатнія 27 — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | памер | поле | тып | адзінка |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 зарад MOS, b1 разрад MOS, b2 балансіроўка |
| 4 | 32 | `cells_mv[16]` | u16×16 | мВ |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 А, разрад `+` |
| 56 | 4 | `power_w` | i32 | Вт |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 А·г |
| 68 | 4 | `remaining_mah` | u32 | 0.001 А·г |
| 72 | 4 | `rated_mah` | u32 | 0.001 А·г |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 У |
| 80 | 2 | `port_cv` | u16 | 0.01 У |
| 82 | 4 | `balance_mask` | u32 | маска |
| 86 | 2 | `mode` | u16 | малодшы байт — enum |
| 88 | 19 | `alarm` | u8×19 | бітфілд, раскладка па тыпе |

Кананічны `mode`: `0` чаканне, `1` разрад, `2` зарад, `3` падтрыманне,
`4` поўны зарад, `5` выключэнне.

`alarm` — 152 біты; раскладка залежыць ад тыпу BMS (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` задаюць лік значных элементаў; хвасты масіваў
ігнаруюцца. Вытворныя (min/max/avg) кліент лічыць сам.

#### 3.3.1. Дэкодэр на Python

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

JSON-дакумент інвертара: палі ўжо маштабаваны і падпісаны ключамі i18n.

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

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, частата,
  L1/L2/L3 (U/I), тэмпературы 1/2, стан, аварыйныя словы 0..5.
- Значэнні — радкі, ужо з масштабам; `nd` — няма даных.
- `alarm_words` — hex-словы, `alarms` — расшыфраваныя коды.

## 4. Запіс у прылады

Запіс даступны па тым самым токене/cookie. Ніжэй — толькі кантракты, патрэбныя кліенту;
унутраныя дэталі — у `docs/map-write-api.md` і `docs/kes-reader.md`.

### 4.1. `POST /api/map/write`

Аўтарызацыя: cookie або Bearer. Цела — да 64 КіБ.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **толькі адзін тып**: альбо `reg`, альбо `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` у hex без
  `0x`; `addr ≤ 3FF` — EEPROM, `≥400` — RAM; да 256 байт (Modbus — да 250).
- `cmd`: `{"op":"cmd","code":"07"}` — роўна адна каманда. Коды: `1` OFF, `2` ON,
  `3` EEPROM WR (службовая), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (бяспечная праверка), `8` StatReset, `9` DischOff.
- Максімум 64 аперацыі.
- `no_frame: true` — не абрамляць службовымі камандамі (кліенту звычайна не трэба).

Адказ:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP-статусы: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Цела — як у control-сокета `kes-reader` (`docs/kes-reader.md` §4.2.3), web
дадае `cmd:"write"` і праксіруе.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — толькі `op=reg`; аперацыі `relay` няма.
- `addr`/`data` — hex без `0x`; `addr` 0x0000..0xFFFF; `data` — 1..256 байт.
- Максімум 64 аперацыі.
- Рэле КЭС — байт-маска ў ячэйцы `0x0E3D` (біты 0/1/2); кліент робіць RMW сам.
- Адказ — JSON `kes-reader`; HTTP-статус па коду: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Статыстыка

Усе эндпоінты пад аўтарызацыяй. Час — **unix-мілісекунды**.

| Эндпоінт | Параметры | Адказ |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | тое ж, акно «апошнія N хвілін» |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (у спісе статыстыкі).
- `id` — радковы ідэнтыфікатар прылады (серыйны нумар/лагічны ID).
- `source`: `raw` або `avg` (для `series`).
- `follow=1` — рэжым «ОНЛАЙН», крыніца заўсёды `raw`, `depth` — хвіліны (1..10,
  па змаўчанні 5); `after` — дагрузка толькі новых кропак.
- `table`: `dir=from|to`, `limit` 1..2000 (па змаўчанні 200), `detail=1` —
  уключыць дэталёвыя радкі, `rawmin` — мінімальны час raw.

Прыклад `series`:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

Ключы `series` — **лікавыя `field_id` кантракту тэлеметрыі** ў выглядзе радкоў
(`"1"` = `UNET`, `"2"` = `INET`), а не адрасы/ID ячэек `map-contract` і не імёны
палёў. Імёны і адзінкі — у `columns` табліцы. Поўны спіс `field_id` па тыпах —
`docs/stat.md`. Параметр `fields` прымае тыя ж лікавыя id праз коску;
пусты `fields` — усе палі.

Прыклад `table`:

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

- `columns[]` — аб'екты `statsCol`; `id` — `field_id` (парадкавы id кантракту
  тэлеметрыі), `type` — код тыпу значэння, `name` — імя поля.
- `rows[]` — аб'екты `statsRow`: `t` — ms, `v` — версія кантракту, `f` — значэнні
  па `field_id` (ключы радковыя).

## 6. Памылкі

| Код | Цела | Калі |
|---|---|---|
| `400` | `{"error":"bad_request"}` | крывы JSON/параметры |
| `401` | `{"error":"unauthorized"}` | няма/пратэрмінаваны токен |
| `404` | `{"error":"..."}` / пуста | няма прылады/слота, `id+gen` не супаў |
| `405` | — | няправільны метад |
| `422` | `{"ok":false,"code":"..."}` | памылка прылады |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | прылада не адказала |
| `500` | `{"ok":false,"code":"internal"}` | унутраная памылка |

## 7. Рэкамендаваны цыкл кліента

1. `POST /api/token` → токен.
2. `GET /api/<type>/devices` (або `/api/mainlist`) → спіс `id/gen`.
3. `GET /api/<type>/raw?id&gen` → бінарны здымак. Для `grid` raw-эндпоінта няма:
   здымак бярэцца праз `GET /api/grid/device?serial=`.
4. Дэкадаваць па кантракце; пры `404` — перачытаць спіс.
5. Для гісторыі — `/api/stats/devices` і `/api/stats/series|table`.
6. Для запісу — `POST /api/map/write` або `/api/kes/write` з тым самым `id/gen`.

## 8. Спасылкі

- [api-legacy.md](api-legacy.md) — legacy-сумяшчальнасць.
- `docs/api-auth.md` — аўтарызацыя і сховішча токенаў.
- `docs/map-contract.md` — поўны кантракт МАП (402 палі).
- `docs/kes-contract.md` — кантракт КЭС (KESE/KESR/KESS).
- `docs/bms-reader.md` — кантракт BMS (107 байт).
- `docs/grid-reader.md` — інвертары.
- `docs/map-write-api.md` — запіс у МАП.
- `docs/kes-reader.md` — запіс у КЭС.
