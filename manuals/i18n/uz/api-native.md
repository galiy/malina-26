# API: native (lists, raws, ёзиш)

Мижозларни ишлаб чиқувчилар учун ҳужжат. `malina-web` нинг ўз HTTP-APIсини
тавсифлайди: авторизация, қурилмалар рўйхати, хом снимоклар (`raw`), қурилмаларга
ёзиш ва статистика. Legacy-мослик — [api-legacy.md](api-legacy.md).

- База: `http://<қурилма-манзили>`.
- `raw` дан ташқари барча жавоблар — JSON (`application/json; charset=utf-8`).
- `raw` — иккилик маълумотлар (`application/octet-stream`) `map-reader`/`kes-reader`/`bms-reader`
  контрактлари бўйича.
- Версиялаш: `raw` контракти сарлавҳада magic/version билан қатъийлаштирилган;
  рўйхатлар майдонлар билан версияланади, контракт ўзгарганда орқага мослик
  кафолатланмайди.

## 1. Авторизация

Ягона механизм: **`malina` фойдаланувчисининг PAM-пароли** (веб ва SSH учун бир хил).
Роллар йўқ: ҳар қандай валид токен = тўлиқ ижозат.

Икки усул қўллаб-қувватланади:

1. **Cookie-сессия** `malina_session` — браузер учун. `HttpOnly`, `SameSite=Lax`,
   TTL 1 соат, ҳар бир сўровда янгиланади.
2. **Bearer-токен** — дастурий мижозлар учун.

### 1.1. Токен олиш

```http
POST /api/token
Content-Type: application/json

{"password": "ПАРОЛ"}
```

Муваффақият:

```json
{
  "token": "64-рамзли hex-сатр",
  "expires": 1792358872
}
```

`expires` — токен **ишлатилмай турганда** яшайдиган unix-вақт. Бу қаттиқ муддат эмас: TTL
сирғалувчи (қ. §1.3).

Хатолар:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Ишлатиш

```http
Authorization: Bearer <token>
```

```bash
TOKEN=$(curl -s -X POST http://10.10.100.254/api/token \
  -H 'Content-Type: application/json' \
  -d '{"password":"ПАРОЛ"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://10.10.100.254/api/mainlist'
```

### 1.3. Сирғалувчи TTL ва бекор қилиш

- Токен чиқарилишдан эмас, **охирги ишлатилишдан 1 соат** яшайди.
- Ҳар қандай авторизацияланган сўров `lastUsed` ни янгилайди.
- Абсолют максимум йўқ.
- Токеннинг фақат `sha256` сақланади `/dev/shm/malina-auth` да; қайта юклашда
  барча токенлар йўқолади, `malina-web` қайта ишга туширилганда — сақланиб қолади.
- Ўз токенини бекор қилиш:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Парол алмаштириш **барча** сессия ва токенларни бекор қилади.
- `GET /api/token` қўллаб-қувватланмайди → `405`.

### 1.4. Авторизация хатолари

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

HTML-саҳифалар учун 401 ўрнига `/login` га редирект ишлатилади.
`WWW-Authenticate` сарлавҳаси `/api/token` га қўйилади; эски API-хендлерларнинг бир
қисми (масалан, `/api/mainlist`) танасиз «яланғоч» `401` билан жавоб беради — мижоз
статусга таяниши керак.

## 2. Қурилмалар рўйхати (lists)

Умумий ғоя: мижоз қурилмалар рўйхатини олади, **`id` + `gen`** жуфтлигини олиб
у бўйича `raw` сўрайди. `id` — слот индекси; `gen` — слотнинг «ижара авлоди».
Жуфтлик вақт бўйича ягона: қурилма қайта уланганида `gen` ўзгаради ва эски `raw`
`404` қайтаради.

### 2.1. `GET /api/mainlist`

Бош саҳифа учун барча қурилмалар бўйича жамланма. Авторизация остида.

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

| Майдон | Тур | Маъноси |
|---|---|---|
| `updated` | int64 | жавоб йиғилишининг unix-вақти |
| `maps[]` | array | МАП плиткалари |
| `kes[]` | array | КЭС плиткалари |
| `bms[]` | array | BMS плиткалари |
| `grid[]` | array | тармоқ инверторлари плиткалари |

### 2.2. МАП плиткаси (`maps[]`)

| Майдон | Тур | Маъноси |
|---|---|---|
| `id` | int | слот индекси |
| `gen` | uint64 | слот авлоди (`/api/map/raw` учун) |
| `title` | string | локализацияланган ёзув |
| `mode` | string | режим матни |
| `mode_num` | uint32 | композит код 0..18 |
| `soc` | float/null | АКБ заряди, % |
| `grid`, `batt`, `load` | block | 5 та қийматдан иборат блоклар |
| `online` | bool | `lastSeen` 2 дақиқадан кечикмаган |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — СИ бирликларидаги қиймат: В, А, Вт, Гц, °C, %.
- `nd: true` — маълумот йўқ, «—» кўрсатилади.
- Сервер **хом** қийматларни яхлитлашсиз қайтаради; яхлитлаш ва бўяш — мижозда.
- МАП `batt` учун **манфий** ток — заряд, **мусбат** — разряд (юқоридаги мисолга қаранг:
  заряд режими, `i = −93.375`).

### 2.3. КЭС плиткаси (`kes[]`)

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

`panel` — панеллар, `batt` — АКБ. КЭС учун `f` доимо `0/nd`.

### 2.4. BMS плиткаси (`bms[]`)

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

- `pack.i` — ишорали ток: разряд `+`, заряд `−`.
- `cells_mv` — узунлиги `cell_count` бўлган массив (мисолда қисқартирилган).
- `cell_warn_mask`/`cell_prot_mask` — ячейкалар бўйича огоҳлантириш/ҳимоя бит
  маскалари.

### 2.5. Инвертор плиткаси (`grid[]`)

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

`status` — **боғланиш** ҳолати (`OK`/боғланиш йўқ), `inverter_status` — инвертор
иш ҳолати.

### 2.6. Бошқа рўйхатлар

| Эндпоинт | Жавоб |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement","logger_sn"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

`logger_sn` — созланган логгернинг серия рақами (uint32, decimal); сақлаш калити эмас; 0 — белгиланмаган (конфигурация йўқ ёки shm эски конфигурациядан).

`/api/map/devices` мисоли:

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

`/api/bms/devices` мисоли:

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

`state=1` — қурилма охирги 60 сония ичида боғланишда бўлган.

## 3. Хом снимоклар (raw)

`raw` — охирги муваффақиятли сўровнинг иккилик снимоки. Мижоз рўйхат бўйича
`id`+`gen` ни текшириши шарт; акс ҳолда бегона/эскирган слот олиниши мумкин.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Payload ўлчами — `64 + ceil(402/8) + 402×4 = 1723` байт (`nfields=402` да).
Қуйида — фрагмент (биринчи 80 байт).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Payload сарлавҳаси:

| Силжиш | Ўлчам | Майдон | Қиймат |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (жорий) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-терминацияланган сатр |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` Микроарт, `1` Modbus |
| 42 | 1 | `unit` | Modbus манзили |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | слот авлоди |
| 56 | 8 | `lastSeen` | unix-вақт |
| 64 | `ceil(n/8)` | `presence` | майдон мавжудлиги бит картаси |
| `64+presence` | `n×4` | `values[]` | ҳар майдон учун `uint32` LE |

- `values[i]` — `id=i` майдонининг **хом** қиймати. Масштаб — мижозда.
- `presence` — агар майдон ҳақиқатан ўқилган бўлса, бит `i` = 1; мавжуд бўлмаган
  қийматлар учун одатда `values` да `0xFFFFFFFF` бўлади, аммо фақат шунга таяниб
  бўлмайди.
- 402 та майдоннинг тўлиқ рўйхати (`id`, ном, манзил, ўлчам) — `docs/map-contract.md`.
- Асосий формулалар — §3.1.1.

#### 3.1.1. Асосий майдонлар масштаби

| Контракт майдони | id | Формула |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` В |
| `MODE` | 36 | композит режим (қ. legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` В |
| `INET` | 148 | А, катталик |
| `TFNET` | 150 | `6250/V` Гц |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` А; ишора: `PNET_Sign_P==0` → қайтиш (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` А; ишора `MODE_RAM` га боғлиқ |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` А; ишора — `H` нинг юқори бити |
| `PNET_8` | 387 | `int16(...) × 10` Вт (ишорали) |

#### 3.1.2. Python-декодер

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

Payload — **1536 байт**: ҳар бири 512 байтдан учта под-блок `[KESE][KESR][KESS]`.
Қуйида — фрагмент (биринчи 32 байт).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, паспорт):

| off | майдон | тур | маъно |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | серий рақами |
| 8 | `Device` | u8 | КЭС учун 5 |
| 9 | `Pow` | u8 | қувват/тур |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | Вт = қиймат×100 |
| 12 | `ModelVolt` | u8 | В = қиймат×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, телеметрия, `off` блок бошланишига нисбатан):

| off | майдон | тур | маъно |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix-вақт |
| 14 | `RelayC` | u8 | реле битлари 1/2/3 |
| 15 | `RSErrSis` | u8 | контроллер хатоси |
| 16 | `I2CErr` | u8 | I²C хатоси |
| 18 | `VcPV` | u16 LE | панеллар кучланиши ×100 |
| 20 | `IcPV` | u16 LE | панеллар токи ×100 |
| 22 | `VBat` | u16 LE | АКБ кучланиши ×100 |
| 24 | `PPV` | u16 LE | панеллар қуввати, Вт |
| 26 | `POut` | u16 LE | чиқиш қуввати, Вт |
| 28 | `PLoad` | u16 LE | юкланиш қуввати, Вт |
| 30 | `PCurr` | u16 LE | заряд қуввати, Вт |
| 32 | `ICh` | u16 LE | заряд токи ×100 |
| 34 | `IOut` | u16 LE | чиқиш токи ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | суткалик энергия, Вт |
| 40 | `PwrKW` | u16 LE | суткалик энергия, кВт |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | ташқи датчиклар |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — маълумот йўқ |
| 55 | `UakbCell[32]` | u16×32 | банка кучланишлари |
| 119 | `TakbCell[32]` | u8×32 | банка ҳароратлари (`x−50`) |
| 151 | `QCell[32]` | u16×32 | банкалар разряд токи |
| 215 | `ErrCells[32]` | u8×32 | BMS хатоси |
| 247 | `IAkbMPPT[16]` | u16×16 | параллел MPPT токи |
| 279 | `ModeMPPT[16]` | u8×16 | MPPT режими |
| 295 | `TakbMPPT[16]` | u8×16 | MPPT ҳарорати |
| 311 | `ErrMPPT[16]` | u8×16 | MPPT хатоси |

`KESS` (созламалар, 40×u16 LE, `off = 5 + 2×i`, RFC ҳолати) — тўлиқ рўйхат
`docs/kes-contract.md` §`KESS` да.

Суткалик умумий энергия: `PwrKW×1000 + PwrW`.

#### 3.2.1. Python-декодер

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

Payload — айнан **107 байт**, кўпбайтли майдонлар — **big-endian**.
Қуйида — фрагмент (биринчи 80 байт; қолган 27 — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | ўлчам | майдон | тур | бирлик |
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
| 86 | 2 | `mode` | u16 | кичик байт — enum |
| 88 | 19 | `alarm` | u8×19 | битфилд, тур бўйича жойлашув |

Каноник `mode`: `0` кутиш, `1` разряд, `2` заряд, `3` ушлаб туриш,
`4` тўлиқ заряд, `5` ўчириш.

`alarm` — 152 бит; жойлашув BMS турига боғлиқ (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` аҳамиятли элементлар сонини белгилайди; массивларнинг
ёлғиз қуйруқлари эътиборга олинмайди. Ҳосилалар (min/max/avg) ни мижоз ўзи ҳисоблайди.

#### 3.3.1. Python-декодер

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

Инверторнинг JSON-ҳужжати: майдонлар аллақачон масштабланган ва i18n калитлари
билан имзоланган.

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
  L1/L2/L3 (U/I), ҳароратлар 1/2, ҳолат, авария сўзлари 0..5.
- Қийматлар — сатрлар, масштаб билан; `nd` — маълумот йўқ.
- `alarm_words` — hex-сўзлар, `alarms` — изоҳланган кодлар.

## 4. Қурилмаларга ёзиш

Ёзиш ўша токен/cookie бўйича мавжуд. Қуйида — фақат мижозга керак контрактлар;
ички деталлар — `docs/map-write-api.md` ва `docs/kes-reader.md` да.

### 4.1. `POST /api/map/write`

Авторизация: cookie ёки Bearer. Тана — 64 КиБ гача.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **фақат битта тур**: ёки `reg`, ёки `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` hex да, `0x`сиз;
  `addr ≤ 3FF` — EEPROM, `≥400` — RAM; 256 байтгача (Modbus — 250 гача).
- `cmd`: `{"op":"cmd","code":"07"}` — айнан битта буйруқ. Кодлар: `1` OFF, `2` ON,
  `3` EEPROM WR (хизмат), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (хавфсиз текширув), `8` StatReset, `9` DischOff.
- Максимум 64 операция.
- `no_frame: true` — хизмат буйруқлари билан ўрамаслик (мижозга одатда керак эмас).

Жавоб:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP-статуслар: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Тана — `kes-reader` control-сокети каби (`docs/kes-reader.md` §4.2.3), web
`cmd:"write"` ни қўшади ва проксилайди.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — фақат `op=reg`; `relay` операцияси йўқ.
- `addr`/`data` — hex, `0x`сиз; `addr` 0x0000..0xFFFF; `data` — 1..256 байт.
- Максимум 64 операция.
- КЭС релеси — `0x0E3D` ячейкасидаги байт-маска (битлар 0/1/2); мижоз RMW ни ўзи
  қилади.
- Жавоб — `kes-reader` JSON; HTTP-статус код бўйича: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Статистика

Барча эндпоинтлар авторизация остида. Вақт — **unix-миллисония**.

| Эндпоинт | Параметрлар | Жавоб |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | худди шу, «охирги N дақиқа» ойнаси |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (статистика рўйхатида).
- `id` — қурилманинг сатр идентификатори (серий рақам/логик ID).
- `source`: `raw` ёки `avg` (`series` учун).
- `follow=1` — «ОНЛАЙН» режими, манба доимо `raw`, `depth` — дақиқалар (1..10,
  сукунат бўйича 5); `after` — фақат янги нуқталарни қўшимча юклаш.
- `table`: `dir=from|to`, `limit` 1..2000 (сукунат бўйича 200), `detail=1` —
  детал сатрларни қўшиш, `rawmin` — raw нинг минимал вақти.

`series` мисоли:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

`series` калитлари — телеметрия контрактининг **рақамли `field_id`** лари сатр
кўринишида (`"1"` = `UNET`, `"2"` = `INET`), `map-contract` манзил/ID ячейкалари ёки
майдон номлари эмас. Номлар ва бирликлар — `columns` жадвалида. Турлар бўйича
`field_id` нинг тўлиқ рўйхати — `docs/stat.md`. `fields` параметри худди ўша рақамли
id ларни вергул орқали қабул қилади; бўш `fields` — барча майдонлар.

`table` мисоли:

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

- `columns[]` — `statsCol` объектлари; `id` — `field_id` (телеметрия контрактининг
  тартиб рақами), `type` — қиймат тури коди, `name` — майдон номи.
- `rows[]` — `statsRow` объектлари: `t` — ms, `v` — контракт версияси, `f` — `field_id`
  бўйича қийматлар (калитлар сатр).

## 6. Хатолар

| Код | Тана | Қачон |
|---|---|---|
| `400` | `{"error":"bad_request"}` | бузилган JSON/параметрлар |
| `401` | `{"error":"unauthorized"}` | токен йўқ/муддати ўтган |
| `404` | `{"error":"..."}` / бўш | қурилма/слот йўқ, `id+gen` мос келмади |
| `405` | — | нотўғри метод |
| `422` | `{"ok":false,"code":"..."}` | қурилма хатоси |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | қурилма жавоб бермади |
| `500` | `{"ok":false,"code":"internal"}` | ички хато |

## 7. Мижознинг тавсия этилган цикли

1. `POST /api/token` → токен.
2. `GET /api/<type>/devices` (ёки `/api/mainlist`) → `id/gen` рўйхати.
3. `GET /api/<type>/raw?id&gen` → иккилик снимок. `grid` учун raw-эндпоинт йўқ:
   снимок `GET /api/grid/device?serial=` орқали олинади.
4. Контракт бўйича декодлаш; `404` да — рўйхатни қайта ўқиш.
5. Тарих учун — `/api/stats/devices` ва `/api/stats/series|table`.
6. Ёзиш учун — `POST /api/map/write` ёки `/api/kes/write` ўша `id/gen` билан.

## 8. Ҳавола

- [api-legacy.md](api-legacy.md) — legacy-мослик.
- `docs/api-auth.md` — авторизация ва токенлар сақлагичи.
- `docs/map-contract.md` — МАП тўлиқ контракти (402 майдон).
- `docs/kes-contract.md` — КЭС контракти (KESE/KESR/KESS).
- `docs/bms-reader.md` — BMS контракти (107 байт).
- `docs/grid-reader.md` — инверторлар.
- `docs/map-write-api.md` — МАПга ёзиш.
- `docs/kes-reader.md` — КЭСга ёзиш.
