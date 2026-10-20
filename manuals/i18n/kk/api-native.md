# API: native (тізімдер, raw, жазу)

Клиент әзірлеушілеріне арналған құжат — `malina-web` өзінің HTTP-API сипаттайды:
авторизация, құрылғылар тізімдері, шикі көріністер (`raw`), құрылғыларға жазу және
статистика. Legacy-үйлесімділік — [api-legacy.md](api-legacy.md).

- База: `http://<құрылғы-мекенжайы>`.
- Барлық жауаптар, `raw`-тан басқасы — JSON (`application/json; charset=utf-8`).
- `raw` — екілік деректер (`application/octet-stream`) контрактілер бойынша
  `map-reader`/`kes-reader`/`bms-reader`.
- Версиялау: `raw` контрактісі тақырыптағы magic/version арқылы бекітілген;
  тізімдер өрістер арқылы версияланады, контракті ауысқан де кері үйлесімділік
  кепілденбейді.

## 1. Авторизация

Бірыңғай тетік: **`malina` пайдаланушысының PAM-құпиясөзі** (веб пен SSH үшін
бірдей). Рөлдер жоқ: кез келген жарамды токен = толық қолжетімділік.

Екі тәсіл қолданылады:

1. **Cookie-сессия** `malina_session` — браузерге арналған. `HttpOnly`, `SameSite=Lax`,
   TTL 1 сағат, әр сұрауда жаңартылады.
2. **Bearer-токен** — бағдарламалық клиенттерге арналған.

### 1.1. Токен алу

```http
POST /api/token
Content-Type: application/json

{"password": "ҚҰПИЯСӨЗ"}
```

Сәтті:

```json
{
  "token": "64-символдық hex-жол",
  "expires": 1792358872
}
```

`expires` — токен **бос тұрғанда** өмір сүретін unix-уақыты. Бұл қатаң мерзім емес:
TTL сырғанамалы (§1.3 қараңыз).

Қателер:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Пайдалану

```http
Authorization: Bearer <token>
```

```bash
TOKEN=$(curl -s -X POST http://10.10.100.254/api/token \
  -H 'Content-Type: application/json' \
  -d '{"password":"ҚҰПИЯСӨЗ"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://10.10.100.254/api/mainlist'
```

### 1.3. Сырғанамалы TTL және қайтарып алу

- Токен **соңғы пайдаланудан кейін 1 сағат** өмір сүреді, шығарылғаннан емес.
- Кез келген авторизацияланған сұрау `lastUsed` мәнін жаңартады.
- Абсолюттік максимум жоқ.
- `/dev/shm/malina-auth` ішінде тек токеннің `sha256` сақталады; қайта жүктегенде
  барлық токендер жоғалады, `malina-web` қайта іске қосылғанда — сақталады.
- Өз токеніңді қайтарып алу:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Құпиясөзді ауыстыру **барлық** сессиялар мен токендерді қайтарып алады.
- `GET /api/token` қолданылмайды → `405`.

### 1.4. Авторизация қателері

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

HTML-беттер үшін 401 орнына `/login` редіректі қолданылады. `WWW-Authenticate`
тақырыбы `/api/token` үстіне қойылады; кейбір ескі API-хендлерлер (мысалы,
`/api/mainlist`) «жалаңаш» `401` денесіз жауап береді — клиент мәртебеге сүйенуі
керек.

## 2. Құрылғылар тізімдері (lists)

Жалпы идея: клиент құрылғылар тізімін алады, **`id` + `gen`** жұбын алып, соған
`raw` сұрайды. `id` — слот индексі; `gen` — слоттың «жалдау ұрпағы». Жұп уақытта
бірмәнді: құрылғы қайта қосылғанда `gen` өзгереді, ал ескі `raw` `404` қайтарады.

### 2.1. `GET /api/mainlist`

Бас бетке арналған барлық құрылғылар бойынша жиынтық. Авторизациямен.

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

| Өріс | Түрі | Мағынасы |
|---|---|---|
| `updated` | int64 | жауапты құрастырудың unix-уақыты |
| `maps[]` | array | МАП плиткалары |
| `kes[]` | array | КЭС плиткалары |
| `bms[]` | array | BMS плиткалары |
| `grid[]` | array | желілік инверторлар плиткалары |

### 2.2. МАП плиткасы (`maps[]`)

| Өріс | Түрі | Мағынасы |
|---|---|---|
| `id` | int | слот индексі |
| `gen` | uint64 | слот ұрпағы (`/api/map/raw` үшін) |
| `title` | string | локализацияланған жазу |
| `mode` | string | режим мәтіні |
| `mode_num` | uint32 | құрамды код 0..18 |
| `soc` | float/null | АКБ заряды, % |
| `grid`, `batt`, `load` | block | 5 мәннен тұратын блоктар |
| `online` | bool | `lastSeen` 2 минуттан ескі емес |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — СИ бірліктеріндегі мән: В, А, Вт, Гц, °C, %.
- `nd: true` — дерек жоқ, «—» көрсету керек.
- Сервер **шикі** мәндерді дөңгелектеусіз береді; дөңгелектеу және бояу — клиентте.
- МАП `batt` үшін **теріс** тоқ — заряд, **оң** — разряд (жоғарыдағы мысалды қараңыз: заряд режимі, `i = −93.375`).

### 2.3. КЭС плиткасы (`kes[]`)

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

`panel` — панельдер, `batt` — АКБ. КЭС үшін `f` әрқашан `0/nd`.

### 2.4. BMS плиткасы (`bms[]`)

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

- `pack.i` — таңбалы тоқ: разряд `+`, заряд `−`.
- `cells_mv` — `cell_count` ұзындығындағы массив (мысалда қысқартылған).
- `cell_warn_mask`/`cell_prot_mask` — ұяшықтар бойынша ескерту/қорғаныс биттік маскалары.

### 2.5. Инвертор плиткасы (`grid[]`)

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

`status` — **байланыс** мәртебесі (`OK`/байланыс жоқ), `inverter_status` —
инвертордың жұмыс күйі.

### 2.6. Басқа тізімдер

| Эндпоинт | Жауап |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement","logger_sn"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

`logger_sn` — конфигурацияланған логгердің сериялық нөмірі (uint32, decimal); сақтау кілті емес; 0 — орнатылмаған (конфигурация жоқ немесе shm ескі конфигурациядан).

`/api/map/devices` мысалы:

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

`/api/bms/devices` мысалы:

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

`state=1` — құрылғы 60 секундтан ескі емес уақытта байланыста болды.

## 3. Шикі көріністер (raw)

`raw` — соңғы сәтті сұраудың екілік көрінісі. Клиент тізім бойынша `id`+`gen`
тексерерге міндетті; әйтпесе бөтен/ескірген слот алуға болады.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Payload өлшемі — `64 + ceil(402/8) + 402×4 = 1723` байт (`nfields=402` кезінде).
Төменде — фрагмент (алғашқы 80 байт).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Payload тақырыбы:

| Ауысым | Өлшем | Өріс | Мәні |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (ағымдағы) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-терминацияланған жол |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` Микроарт, `1` Modbus |
| 42 | 1 | `unit` | Modbus мекенжайы |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | слот ұрпағы |
| 56 | 8 | `lastSeen` | unix-уақыт |
| 64 | `ceil(n/8)` | `presence` | өрістің болуының биттік картасы |
| `64+presence` | `n×4` | `values[]` | әр өріске `uint32` LE |

- `values[i]` — `id=i` өрісінің **шикі** мәні. Масштаб — клиентте.
- `presence` — бит `i` = 1, егер өріс нақты оқылған болса; жоқ мәндер үшін әдетте
  `values` ішінде `0xFFFFFFFF`, бірақ тек соған сүйенуге болмайды.
- 402 өрістің толық тізімі (`id`, атау, мекенжай, өлшем) — `docs/map-contract.md`.
- Негізгі формулалар — §3.1.1.

#### 3.1.1. Негізгі өрістердің масштабтары

| Контракті өрісі | id | Формуласы |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` В |
| `MODE` | 36 | құрамды режим (legacy §3.3 қараңыз) |
| `UNET` | 147 | `0`/`V+100` В |
| `INET` | 148 | А, магнитуда |
| `TFNET` | 150 | `6250/V` Гц |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` А; таңбасы: `PNET_Sign_P==0` → беру (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` А; таңбасы `MODE_RAM`-ге байланысты |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` А; таңбасы — `H` жоғарғы биті |
| `PNET_8` | 387 | `int16(...) × 10` Вт (таңбалы) |

#### 3.1.2. Python декодері

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

Payload — **1536 байт**: 512 байттан үш ішкі блок `[KESE][KESR][KESS]`.
Төменде — фрагмент (алғашқы 32 байт).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, паспорт):

| off | өріс | түрі | мағынасы |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | сериялық нөмір |
| 8 | `Device` | u8 | КЭС үшін 5 |
| 9 | `Pow` | u8 | қуат/түрі |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | Вт = мән×100 |
| 12 | `ModelVolt` | u8 | В = мән×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, телеметрия, `off` блоктың басына қатысты):

| off | өріс | түрі | мағынасы |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix-уақыт |
| 14 | `RelayC` | u8 | реле 1/2/3 биттері |
| 15 | `RSErrSis` | u8 | контроллер қателері |
| 16 | `I2CErr` | u8 | I²C қатесі |
| 18 | `VcPV` | u16 LE | панель кернеуі ×100 |
| 20 | `IcPV` | u16 LE | панель тоғы ×100 |
| 22 | `VBat` | u16 LE | АКБ кернеуі ×100 |
| 24 | `PPV` | u16 LE | панель қуаты, Вт |
| 26 | `POut` | u16 LE | шығыс қуаты, Вт |
| 28 | `PLoad` | u16 LE | жүктеме қуаты, Вт |
| 30 | `PCurr` | u16 LE | заряд қуаты, Вт |
| 32 | `ICh` | u16 LE | заряд тоғы ×100 |
| 34 | `IOut` | u16 LE | шығыс тоғы ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | тәуліктегі энергия, Вт |
| 40 | `PwrKW` | u16 LE | тәуліктегі энергия, кВт |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | сыртқы датчиктер |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — дерек жоқ |
| 55 | `UakbCell[32]` | u16×32 | банка кернеулері |
| 119 | `TakbCell[32]` | u8×32 | банка температуралары (`x−50`) |
| 151 | `QCell[32]` | u16×32 | банка разряд тоғы |
| 215 | `ErrCells[32]` | u8×32 | BMS қателері |
| 247 | `IAkbMPPT[16]` | u16×16 | параллель MPPT тоғы |
| 279 | `ModeMPPT[16]` | u8×16 | MPPT режимі |
| 295 | `TakbMPPT[16]` | u8×16 | MPPT температурасы |
| 311 | `ErrMPPT[16]` | u8×16 | MPPT қателері |

`KESS` (баптаулар, 40×u16 LE, `off = 5 + 2×i`, RFC мәртебесі) — толық тізім
`docs/kes-contract.md` §`KESS`.

Тәуліктегі жиынтық энергия: `PwrKW×1000 + PwrW`.

#### 3.2.1. Python декодері

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

Payload — дәл **107 байт**, көпбайтты өрістер — **big-endian**.
Төменде — фрагмент (алғашқы 80 байт; қалған 27 — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | өлшем | өріс | түрі | бірлігі |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 заряд MOS, b1 разряд MOS, b2 теңгеру |
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
| 86 | 2 | `mode` | u16 | кіші байт — enum |
| 88 | 19 | `alarm` | u8×19 | биттік өріс, түріне қарай орналасуы |

Канондық `mode`: `0` күту, `1` разряд, `2` заряд, `3` ұстап тұру,
`4` толық заряд, `5` өшіру.

`alarm` — 152 бит; орналасуы BMS түріне байланысты (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` мәнді элементтер санын береді; массивтердің қалдық
ұштары еленбейді. Туындыларды (min/max/avg) клиент өзі есептейді.

#### 3.3.1. Python декодері

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

Инвертордың JSON-құжаты: өрістер бұдан былай масштабталған және i18n кілттерімен
қол қойылған.

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

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, жиілік,
  L1/L2/L3 (U/I), температура 1/2, күй, апаттық сөздер 0..5.
- Мәндер — жолдар, масштабпен; `nd` — дерек жоқ.
- `alarm_words` — hex-сөздер, `alarms` — түсіндірілген кодтар.

## 4. Құрылғыларға жазу

Жазу дәл сол токен/cookie арқылы қолжетімді. Төменде — тек клиентке қажет
контрактілер; ішкі бөлшектер — `docs/map-write-api.md` және `docs/kes-reader.md`.

### 4.1. `POST /api/map/write`

Авторизация: cookie немесе Bearer. Дене — 64 КиБ-қа дейін.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **тек бір түр**: не `reg`, не `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` `0x`-сіз hex;
  `addr ≤ 3FF` — EEPROM, `≥400` — RAM; 256 байтқа дейін (Modbus — 250-ке дейін).
- `cmd`: `{"op":"cmd","code":"07"}` — дәл бір команда. Кодтар: `1` OFF, `2` ON,
  `3` EEPROM WR (қызметтік), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (қауіпсіз тексеру), `8` StatReset, `9` DischOff.
- Максимум 64 операция.
- `no_frame: true` — қызметтік командалармен орамау (клиентке әдетте қажет емес).

Жауап:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP-мәртебелер: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Дене — `kes-reader` control-сокеті сияқты (`docs/kes-reader.md` §4.2.3), web
`cmd:"write"` қосады және проксилейді.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — тек `op=reg`; `relay` операциясы жоқ.
- `addr`/`data` — `0x`-сіз hex; `addr` 0x0000..0xFFFF; `data` — 1..256 байт.
- Максимум 64 операция.
- КЭС релесі — `0x0E3D` ұяшығындағы байт-маска (бит 0/1/2); клиент RMW-ді өзі
  жасайды.
- Жауап — `kes-reader` JSON; HTTP-мәртебе код бойынша: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Статистика

Барлық эндпоинттер авторизациямен. Уақыт — **unix-миллисекунд**.

| Эндпоинт | Параметрлер | Жауап |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | сол сияқты, «соңғы N минут» терезесі |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (статистика тізімінде).
- `id` — құрылғының жол түріндегі идентификаторы (сериялық нөмір/логикалық ID).
- `source`: `raw` немесе `avg` (`series` үшін).
- `follow=1` — «ОНЛАЙН» режимі, көз әрқашан `raw`, `depth` — минут (1..10,
  әдепкі 5); `after` — тек жаңа нүктелерді жүктеу.
- `table`: `dir=from|to`, `limit` 1..2000 (әдепкі 200), `detail=1` —
  бөлшек жолдарды қосу, `rawmin` — raw-дың ең аз уақыты.

`series` мысалы:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

`series` кілттері — телеметрия контрактісінің **сандық `field_id`** жол түрінде
(`"1"` = `UNET`, `"2"` = `INET`), ал `map-contract` ұяшықтарының
мекенжайы/ID-сы емес және өріс атаулары емес. Атаулар мен бірліктер — `columns`
кестесінде. Типтер бойынша толық `field_id` тізімі — `docs/stat.md`. `fields`
параметрі сол сандық id-ді үтірмен қабылдайды; бос `fields` — барлық өрістер.

`table` мысалы:

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

- `columns[]` — `statsCol` нысандары; `id` — `field_id` (телеметрия контрактісінің
  реттік id-і), `type` — мән түрінің коды, `name` — өріс атауы.
- `rows[]` — `statsRow` нысандары: `t` — ms, `v` — контракті версиясы, `f` —
  `field_id` бойынша мәндер (кілттер жол түрінде).

## 6. Қателер

| Код | Дене | Қашан |
|---|---|---|
| `400` | `{"error":"bad_request"}` | бұрыс JSON/параметрлер |
| `401` | `{"error":"unauthorized"}` | токен жоқ/мерзімі өткен |
| `404` | `{"error":"..."}` / бос | құрылғы/слот жоқ, `id+gen` сәйкес келмеді |
| `405` | — | бұрыс әдіс |
| `422` | `{"ok":false,"code":"..."}` | құрылғы қатесі |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | құрылғы жауап бермеді |
| `500` | `{"ok":false,"code":"internal"}` | ішкі қате |

## 7. Клиенттің ұсынылатын циклі

1. `POST /api/token` → токен.
2. `GET /api/<type>/devices` (немесе `/api/mainlist`) → `id/gen` тізімі.
3. `GET /api/<type>/raw?id&gen` → екілік көрініс. `grid` үшін raw-эндпоинт жоқ:
   көрініс `GET /api/grid/device?serial=` арқылы алынады.
4. Контракті бойынша декодтау; `404` кезінде — тізімді қайта оқу.
5. Тарих үшін — `/api/stats/devices` және `/api/stats/series|table`.
6. Жазу үшін — дәл сол `id/gen`-мен `POST /api/map/write` немесе `/api/kes/write`.

## 8. Сілтемелер

- [api-legacy.md](api-legacy.md) — legacy-үйлесімділік.
- `docs/api-auth.md` — авторизация және токендер қоймасы.
- `docs/map-contract.md` — толық МАП контрактісі (402 өріс).
- `docs/kes-contract.md` — КЭС контрактісі (KESE/KESR/KESS).
- `docs/bms-reader.md` — BMS контрактісі (107 байт).
- `docs/grid-reader.md` — инверторлар.
- `docs/map-write-api.md` — МАП-қа жазу.
- `docs/kes-reader.md` — КЭС-ке жазу.
