# API: native (lists, raws, навиштан)

Ҳуҷҷат барои таҳиягарони клиентҳо. HTTP-API-и худии `malina-web`-ро тавсиф мекунад:
авторизатсия, рӯйхати дастгоҳҳо, аксҳои хом (`raw`), навиштан ба дастгоҳҳо ва
омор. Мутобиқати legacy — [api-legacy.md](api-legacy.md).

- База: `http://<суроғаи-дастгоҳ>`.
- Ҳамаи посухҳо, ба ҷуз `raw`, — JSON (`application/json; charset=utf-8`).
- `raw` — маълумоти дуӣ (`application/octet-stream`) аз рӯи шартномаҳои
  `map-reader`/`kes-reader`/`bms-reader`.
- Версиябандӣ: шартномаи `raw` тавассути magic/version дар сарлавҳа муқаррар
  шудааст; рӯйхатҳо тавассути майдонҳо версиябандӣ мешаванд, мутобиқати баръакс
  ҳангоми тағйири шартнома кафолат дода намешавад.

## 1. Авторизатсия

Механизми ягона: **PAM-пароли корбари `malina`** (ҳамон, ки барои веб ва SSH).
Нақшҳо нестанд: ҳар як токени эътиборнок = дастрасии пурра.

Ду роҳ дастгирӣ мешавад:

1. **Сессия-и cookie** `malina_session` — барои браузер. `HttpOnly`, `SameSite=Lax`,
   TTL 1 соат, ҳангоми ҳар дархост нав мешавад.
2. **Bearer-токен** — барои клиентҳои барномавӣ.

### 1.1. Гирифтани токен

```http
POST /api/token
Content-Type: application/json

{"password": "ПАРОЛ"}
```

Муваффақият:

```json
{
  "token": "сатри 64-аломатии hex",
  "expires": 1792358872
}
```

`expires` — вақти unix, то он токен **ҳангоми бекорӣ** зинда хоҳад монд. Ин
муҳлати қатъӣ нест: TTL лағжанда аст (ниг. §1.3).

Хатогиҳо:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Истифода

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

### 1.3. TTL лағжанда ва бозхонд

- Токен **1 соат аз истифодаи охирин** зинда мемонад, на аз баровардан.
- Ҳар дархости авторизатсияшуда `lastUsed`-ро нав мекунад.
- Максимуми мутлақ вуҷуд надорад.
- Танҳо `sha256`-и токен дар `/dev/shm/malina-auth` нигоҳ дошта мешавад; ҳангоми
  барқарорсозӣ ҳамаи токенҳо нест мешаванд, ҳангоми бозоғози `malina-web` — нигоҳ
  дошта мешаванд.
- Бозхонди токени худ:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Тағйири парол ҳамаи сессияҳо ва токенҳоро бозмегирад.
- `GET /api/token` дастгирӣ намешавад → `405`.

### 1.4. Хатогиҳои авторизатсия

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

Барои саҳифаҳои HTML ба ҷои 401 редирект ба `/login` истифода мешавад.
Сарлавҳаи `WWW-Authenticate` дар `/api/token` гузошта мешавад; қисме аз
handler-ҳои кӯҳнаи API (масалан, `/api/mainlist`) бо `401`-и «лухт» бе бадан
ҷавоб медиҳад — клиент бояд ба статус такя кунад.

## 2. Рӯйхати дастгоҳҳо (lists)

Ғояи умумӣ: клиент рӯйхати дастгоҳҳоро мегирад, ҷуфти **`id` + `gen`**-ро мегирад
ва аз рӯи он `raw`-ро дархост мекунад. `id` — индекси слот; `gen` — «насл ба иҷораи»
слот. Ҷуфт дар вақт ягона аст: пас аз дубора пайвастшавии дастгоҳ `gen` тағйир
меёбад ва `raw`-и кӯҳна `404` бармегардонад.

### 2.1. `GET /api/mainlist`

Хулоса оид ба ҳамаи дастгоҳҳо барои саҳифаи асосӣ. Зери авторизатсия.

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

| Майдон | Навъ | Маъно |
|---|---|---|
| `updated` | int64 | вақти unix-и ҷамъбасти посух |
| `maps[]` | array | плиткаҳои МАП |
| `kes[]` | array | плиткаҳои КЭС |
| `bms[]` | array | плиткаҳои BMS |
| `grid[]` | array | плиткаҳои инверторҳои шабакавӣ |

### 2.2. Плиткаи МАП (`maps[]`)

| Майдон | Навъ | Маъно |
|---|---|---|
| `id` | int | индекси слот |
| `gen` | uint64 | насли слот (барои `/api/map/raw`) |
| `title` | string | имзои локалишуда |
| `mode` | string | матни режим |
| `mode_num` | uint32 | рамзи композитӣ 0..18 |
| `soc` | float/null | пуршавии АКБ, % |
| `grid`, `batt`, `load` | block | блокҳо аз рӯи 5 арзиш |
| `online` | bool | `lastSeen` на дертар аз 2 дақиқа |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — арзиш дар воҳидҳои СИ: В, А, Вт, Гц, °C, %.
- `nd: true` — маълумот нест, нишон додан «—».
- Сервер арзишҳои **хом**-ро бе гирдкунӣ медиҳад; гирдкунӣ ва рангубор — дар
  клиент.
- Барои `batt`-и МАП ҷараёни **манфӣ** — пуркунӣ, **мусбат** — холӣкунӣ (ниг.
  мисоли дар боло: режими пуркунӣ, `i = −93.375`).

### 2.3. Плиткаи КЭС (`kes[]`)

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

`panel` — панелҳо, `batt` — АКБ. `f` барои КЭС ҳамеша `0/nd`.

### 2.4. Плиткаи BMS (`bms[]`)

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

- `pack.i` — ҷараён бо аломат: холӣкунӣ `+`, пуркунӣ `−`.
- `cells_mv` — массив дарозиаш `cell_count` (дар мисол ихтисор шудааст).
- `cell_warn_mask`/`cell_prot_mask` — ниқобҳои битии огоҳиҳо/муҳофизатҳо аз рӯи
  ҳуҷайраҳо.

### 2.5. Плиткаи инвертор (`grid[]`)

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

`status` — статуси **пайваст** (`OK`/бе пайваст), `inverter_status` — ҳолати кори
инвертор.

### 2.6. Рӯйхатҳои дигар

| Эндпоинт | Посух |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

Мисоли `/api/map/devices`:

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

Мисоли `/api/bms/devices`:

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

`state=1` — дастгоҳ на дертар аз 60 сония қабл дар пайваст буд.

## 3. Аксҳои хом (raw)

`raw` — акси дуии охирин пурсиши муваффақ. Клиент вазифадор аст `id`+`gen`-ро аз
рӯи рӯйхат тафтиш кунад; вагарна метавонад слоти бегона/кӯҳна гирад.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Андозаи payload — `64 + ceil(402/8) + 402×4 = 1723` байт (ҳангоми `nfields=402`).
Дар поён — порча (80 байти аввал).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Сарлавҳаи payload:

| Ҷойивазкунӣ | Андоза | Майдон | Арзиш |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (ҷорӣ) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | сатри NUL-терминалшуда |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` Микроарт, `1` Modbus |
| 42 | 1 | `unit` | суроғаи Modbus |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | насли слот |
| 56 | 8 | `lastSeen` | вақти unix |
| 64 | `ceil(n/8)` | `presence` | харитаи битии мавҷуд будани майдон |
| `64+presence` | `n×4` | `values[]` | `uint32` LE барои ҳар майдон |

- `values[i]` — арзиши **хом**-и майдони `id=i`. Масштаб — дар клиент.
- `presence` — бити `i` = 1, агар майдон воқеан хонда шуда бошад; барои арзишҳои
  мавҷуднабуда одатан `0xFFFFFFFF` дар `values`, аммо танҳо ба ин такя кардан
  мумкин нест.
- Рӯйхати пурраи 402 майдон (`id`, ном, суроға, андоза) — `docs/map-contract.md`.
- Формулаҳои калидӣ — §3.1.1.

#### 3.1.1. Масштабҳои майдонҳои калидӣ

| Майдони шартнома | id | Формула |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` В |
| `MODE` | 36 | режими композитӣ (ниг. legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` В |
| `INET` | 148 | А, магнитуда |
| `TFNET` | 150 | `6250/V` Гц |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` А; аломат: `PNET_Sign_P==0` → додан (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` А; аломат аз `MODE_RAM` вобаста аст |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` А; аломат — бити калони `H` |
| `PNET_8` | 387 | `int16(...) × 10` Вт (аломатдор) |

#### 3.1.2. Декодер дар Python

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

Payload — **1536 байт**: се зер-блок по 512 байт `[KESE][KESR][KESS]`.
Дар поён — порча (32 байти аввал).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, шиноснома):

| off | майдон | навъ | маъно |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | рақами истеҳсолӣ |
| 8 | `Device` | u8 | 5 барои КЭС |
| 9 | `Pow` | u8 | қудрат/навъ |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | Вт = арзиш×100 |
| 12 | `ModelVolt` | u8 | В = арзиш×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, телеметрия, `off` нисбат ба оғози блок):

| off | майдон | навъ | маъно |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | вақти unix |
| 14 | `RelayC` | u8 | битҳои реле 1/2/3 |
| 15 | `RSErrSis` | u8 | хатогиҳои контроллер |
| 16 | `I2CErr` | u8 | хатогии I²C |
| 18 | `VcPV` | u16 LE | шиддати панелҳо ×100 |
| 20 | `IcPV` | u16 LE | ҷараёни панелҳо ×100 |
| 22 | `VBat` | u16 LE | шиддати АКБ ×100 |
| 24 | `PPV` | u16 LE | қудрати панелҳо, Вт |
| 26 | `POut` | u16 LE | қудрати баромад, Вт |
| 28 | `PLoad` | u16 LE | қудрати бор, Вт |
| 30 | `PCurr` | u16 LE | қудрати пуркунӣ, Вт |
| 32 | `ICh` | u16 LE | ҷараёни пуркунӣ ×100 |
| 34 | `IOut` | u16 LE | ҷараёни баромад ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | энергия дар шабонарӯз, Вт |
| 40 | `PwrKW` | u16 LE | энергия дар шабонарӯз, кВт |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | сенсорҳои беруна |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — маълумот нест |
| 55 | `UakbCell[32]` | u16×32 | шиддатҳои банкаҳо |
| 119 | `TakbCell[32]` | u8×32 | ҳароратҳои банкаҳо (`x−50`) |
| 151 | `QCell[32]` | u16×32 | ҷараёни холӣкунии банкаҳо |
| 215 | `ErrCells[32]` | u8×32 | хатогиҳои BMS |
| 247 | `IAkbMPPT[16]` | u16×16 | ҷараёни MPPT-ҳои параллел |
| 279 | `ModeMPPT[16]` | u8×16 | режими MPPT |
| 295 | `TakbMPPT[16]` | u8×16 | ҳарорати MPPT |
| 311 | `ErrMPPT[16]` | u8×16 | хатогиҳои MPPT |

`KESS` (танзимот, 40×u16 LE, `off = 5 + 2×i`, статуси RFC) — рӯйхати пурра дар
`docs/kes-contract.md` §`KESS`.

Энергияи умумии шабонарӯз: `PwrKW×1000 + PwrW`.

#### 3.2.1. Декодер дар Python

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

Payload — маҳз **107 байт**, майдонҳои чандбайта — **big-endian**.
Дар поён — порча (80 байти аввал; 27 байти боқимонда — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | андоза | майдон | навъ | воҳид |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 пуркунии MOS, b1 холӣкунии MOS, b2 мувозинат |
| 4 | 32 | `cells_mv[16]` | u16×16 | мВ |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 А, холӣкунӣ `+` |
| 56 | 4 | `power_w` | i32 | Вт |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 А·ч |
| 68 | 4 | `remaining_mah` | u32 | 0.001 А·ч |
| 72 | 4 | `rated_mah` | u32 | 0.001 А·ч |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 В |
| 80 | 2 | `port_cv` | u16 | 0.01 В |
| 82 | 4 | `balance_mask` | u32 | ниқоб |
| 86 | 2 | `mode` | u16 | байти хурд — enum |
| 88 | 19 | `alarm` | u8×19 | битфилд, ҷойгиршавӣ аз рӯи навъ |

`mode`-и каноникӣ: `0` интизорӣ, `1` холӣкунӣ, `2` пуркунӣ, `3` нигоҳдорӣ,
`4` пуркунии пурра, `5` хомӯшкунӣ.

`alarm` — 152 бит; ҷойгиршавӣ аз навъи BMS вобаста аст (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` шумораи унсурҳои назаррасро муқаррар мекунанд; думҳои
массивҳо нодида гирифта мешаванд. Арзишҳои баромада (min/max/avg) клиент худаш
ҳисоб мекунад.

#### 3.3.1. Декодер дар Python

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

Ҳуҷҷати JSON-и инвертор: майдонҳо аллакай масштаб шудаанд ва бо калидҳои i18n
имзо гузошта шудаанд.

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

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, басомад,
  L1/L2/L3 (U/I), ҳароратҳои 1/2, ҳолат, калимаҳои ҳолати фавқулодда 0..5.
- Арзишҳо — сатрҳо, аллакай бо масштаб; `nd` — маълумот нест.
- `alarm_words` — калимаҳои hex, `alarms` — рамзҳои тафсиршуда.

## 4. Навиштан ба дастгоҳҳо

Навиштан тавассути ҳамон токен/cookie дастрас аст. Дар поён — танҳо шартномаҳои
барои клиент зарурӣ; ҷузъиёти дохилӣ — дар `docs/map-write-api.md` ва
`docs/kes-reader.md`.

### 4.1. `POST /api/map/write`

Авторизатсия: cookie ё Bearer. Бадан — то 64 КиБ.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **танҳо як навъ**: ё `reg`, ё `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` дар hex бе
  `0x`; `addr ≤ 3FF` — EEPROM, `≥400` — RAM; то 256 байт (Modbus — то 250).
- `cmd`: `{"op":"cmd","code":"07"}` — маҳз як фармон. Рамзҳо: `1` OFF, `2` ON,
  `3` EEPROM WR (хизматӣ), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (санҷиши бехатар), `8` StatReset, `9` DischOff.
- Максимум 64 амалиёт.
- `no_frame: true` — бо фармонҳои хизматӣ иҳота накардан (одатан ба клиент зарур
  нест).

Посух:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

Статусҳои HTTP: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Бадан — мисли control-сокет `kes-reader` (`docs/kes-reader.md` §4.2.3), web
`cmd:"write"`-ро илова мекунад ва прокси мекунад.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — танҳо `op=reg`; амалиёти `relay` нест.
- `addr`/`data` — hex бе `0x`; `addr` 0x0000..0xFFFF; `data` — 1..256 байт.
- Максимум 64 амалиёт.
- Релеи КЭС — ниқоби байтӣ дар ҳуҷайраи `0x0E3D` (битҳои 0/1/2); клиент RMW-ро
  худаш мекунад.
- Посух — JSON-и `kes-reader`; статуси HTTP аз рӯи рамз: `200` ok, `400`
  bad_request, `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Омор

Ҳамаи эндпоинтҳо зери авторизатсия. Вақт — **unix-миллисония**.

| Эндпоинт | Параметрҳо | Посух |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | ҳамон, тирезаи «N дақиқаи охирин» |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (дар рӯйхати омор).
- `id` — идентификатори сатрии дастгоҳ (рақами истеҳсолӣ/ID логикӣ).
- `source`: `raw` ё `avg` (барои `series`).
- `follow=1` — режими «ОНЛАЙН», манбаъ ҳамеша `raw`, `depth` — дақиқаҳо (1..10,
  пешфарз 5); `after` — боркунии танҳо нуқтаҳои нав.
- `table`: `dir=from|to`, `limit` 1..2000 (пешфарз 200), `detail=1` —
  сатрҳои муфассалро дохил кардан, `rawmin` — вақти ҳадди аққали raw.

Мисоли `series`:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

Калидҳои `series` — **`field_id`-и ададии шартномаи телеметрия** дар шакли сатрҳо
(`"1"` = `UNET`, `"2"` = `INET`), на суроғаҳо/ID-ҳои ҳуҷайраҳои `map-contract` ва
на номҳои майдонҳо. Номҳо ва воҳидҳо — дар `columns`-и ҷадвал. Рӯйхати пурраи
`field_id` аз рӯи навъҳо — `docs/stat.md`. Параметри `fields` ҳамин id-ҳои ададиро
бо вергул қабул мекунад; `fields`-и холӣ — ҳамаи майдонҳо.

Мисоли `table`:

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

- `columns[]` — объектҳои `statsCol`; `id` — `field_id` (id-и тартибии шартномаи
  телеметрия), `type` — рамзи навъи арзиш, `name` — номи майдон.
- `rows[]` — объектҳои `statsRow`: `t` — ms, `v` — версияи шартнома, `f` — арзишҳо
  аз рӯи `field_id` (калидҳо сатрӣ).

## 6. Хатогиҳо

| Рамз | Бадан | Кай |
|---|---|---|
| `400` | `{"error":"bad_request"}` | JSON/параметрҳои каҷ |
| `401` | `{"error":"unauthorized"}` | токени нест/мӯҳлатгузашта |
| `404` | `{"error":"..."}` / холӣ | дастгоҳ/слот нест, `id+gen` мувофиқат накард |
| `405` | — | усули нодуруст |
| `422` | `{"ok":false,"code":"..."}` | хатогии дастгоҳ |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | дастгоҳ посух надод |
| `500` | `{"ok":false,"code":"internal"}` | хатогии дохилӣ |

## 7. Давраи тавсияшудаи клиент

1. `POST /api/token` → токен.
2. `GET /api/<type>/devices` (ё `/api/mainlist`) → рӯйхати `id/gen`.
3. `GET /api/<type>/raw?id&gen` → акси дуӣ. Барои `grid` эндпоинти raw нест:
   акс тавассути `GET /api/grid/device?serial=` гирифта мешавад.
4. Аз рӯи шартнома декод кардан; ҳангоми `404` — рӯйхатро дубора хондан.
5. Барои таърих — `/api/stats/devices` ва `/api/stats/series|table`.
6. Барои навиштан — `POST /api/map/write` ё `/api/kes/write` бо ҳамон `id/gen`.

## 8. Истинодҳо

- [api-legacy.md](api-legacy.md) — мутобиқати legacy.
- `docs/api-auth.md` — авторизатсия ва анбори токенҳо.
- `docs/map-contract.md` — шартномаи пурраи МАП (402 майдон).
- `docs/kes-contract.md` — шартномаи КЭС (KESE/KESR/KESS).
- `docs/bms-reader.md` — шартномаи BMS (107 байт).
- `docs/grid-reader.md` — инверторҳо.
- `docs/map-write-api.md` — навиштан ба МАП.
- `docs/kes-reader.md` — навиштан ба КЭС.