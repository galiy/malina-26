# API: native (lists, raws, ჩაწერა)

დოკუმენტი კლიენტების შემქმნელებისთვის. აღწერს `malina-web`-ის საკუთარ HTTP-API-ს:
ავტორიზაციას, მოწყობილობების სიებს, ნედლ სნაპშოტებს (`raw`), მოწყობილობებში ჩაწერას და
სტატისტიკას. Legacy-თავსებადობა — [api-legacy.md](api-legacy.md).

- ბაზა: `http://<მოწყობილობის-მისამართი>`.
- ყველა პასუხი, `raw`-ის გარდა — JSON (`application/json; charset=utf-8`).
- `raw` — ბინარული მონაცემები (`application/octet-stream`) კონტრაქტებით
  `map-reader`/`kes-reader`/`bms-reader`.
- ვერსიონირება: `raw`-კონტრაქტი ფიქსირებულია სათაურში magic/version; სიები
  ვერსიონირდება ველებით, საპირისპირო თავსებადობა კონტრაქტის ცვლილებისას
  არ არის გარანტირებული.

## 1. ავტორიზაცია

ერთიანი მექანიზმი: **`malina` მომხმარებლის PAM-პაროლი** (იგივე, რაც ვებისა და SSH-ისთვის).
როლები არ არსებობს: ნებისმიერი მოქმედი ტოკენი = სრული წვდომა.

მხარდაჭერილია ორი გზა:

1. **Cookie-სესია** `malina_session` — ბრაუზერისთვის. `HttpOnly`, `SameSite=Lax`,
   TTL 1 საათი, განახლდება ყოველ მოთხოვნაზე.
2. **Bearer-ტოკენი** — პროგრამული კლიენტებისთვის.

### 1.1. ტოკენის მიღება

```http
POST /api/token
Content-Type: application/json

{"password": "პაროლი"}
```

წარმატება:

```json
{
  "token": "64-სიმბოლოიანი hex-სტრიქონი",
  "expires": 1792358872
}
```

`expires` — unix-დრო, რომლამდეც ტოკენი იცოცხლებს **უმოქმედობისას**. ეს არ არის
მკაცრი ბოლო ვადა: TTL სრიალაა (იხ. §1.3).

შეცდომები:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. გამოყენება

```http
Authorization: Bearer <token>
```

```bash
TOKEN=$(curl -s -X POST http://10.10.100.254/api/token \
  -H 'Content-Type: application/json' \
  -d '{"password":"პაროლი"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://10.10.100.254/api/mainlist'
```

### 1.3. სრიალა TTL და გაუქმება

- ტოკენი ცოცხლობს **1 საათი ბოლო გამოყენებიდან**, და არა გამოშვებიდან.
- ნებისმიერი ავტორიზებული მოთხოვნა აახლებს `lastUsed`-ს.
- აბსოლუტური მაქსიმუმი არ არსებობს.
- ინახება მხოლოდ ტოკენის `sha256` ფაილში `/dev/shm/malina-auth`; გადატვირთვისას ყველა
  ტოკენი ქრება, `malina-web`-ის გადატვირთვისას — ნარჩუნდება.
- საკუთარი ტოკენის გაუქმება:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- პაროლის შეცვლა აუქმებს **ყველა** სესიას და ტოკენს.
- `GET /api/token` არ არის მხარდაჭერილი → `405`.

### 1.4. ავტორიზაციის შეცდომები

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

HTML-გვერდებისთვის 401-ის ნაცვლად გამოიყენება რედირექტი `/login`-ზე.
სათაური `WWW-Authenticate` დგება `/api/token`-ზე; ძველი API-ჰენდლერების ნაწილი
(მაგალითად, `/api/mainlist`) პასუხობს «შიშველი» `401`-ით ტანის გარეშე — კლიენტი
უნდა დაეყრდნოს სტატუსს.

## 2. მოწყობილობების სიები (lists)

ზოგადი იდეა: კლიენტი იღებს მოწყობილობების სიას, ირჩევს წყვილს **`id` + `gen`** და
მის მიხედვით ითხოვს `raw`-ს. `id` — სლოტის ინდექსი; `gen` — სლოტის «იჯარის თაობა».
წყვილი ცალსახაა დროში: მოწყობილობის ხელახლა მიერთების შემდეგ `gen` იცვლება და
ძველი `raw` დააბრუნებს `404`-ს.

### 2.1. `GET /api/mainlist`

შემაჯამებელი ყველა მოწყობილობაზე მთავარი გვერდისთვის. ავტორიზაციის ქვეშ.

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

სქემა:

| ველი | ტიპი | მნიშვნელობა |
|---|---|---|
| `updated` | int64 | პასუხის შედგენის unix-დრო |
| `maps[]` | array | MAP-ის ფილები |
| `kes[]` | array | KES-ის ფილები |
| `bms[]` | array | BMS-ის ფილები |
| `grid[]` | array | ქსელური ინვერტორების ფილები |

### 2.2. MAP-ის ფილა (`maps[]`)

| ველი | ტიპი | მნიშვნელობა |
|---|---|---|
| `id` | int | სლოტის ინდექსი |
| `gen` | uint64 | სლოტის თაობა (`/api/map/raw`-სთვის) |
| `title` | string | ლოკალიზებული ხელმოწერა |
| `mode` | string | რეჟიმის ტექსტი |
| `mode_num` | uint32 | კომპოზიტური კოდი 0..18 |
| `soc` | float/null | აკუმულატორის მუხტი, % |
| `grid`, `batt`, `load` | block | ბლოკები 5-5 მნიშვნელობით |
| `online` | bool | `lastSeen` არ არის 2 წუთზე ძველი |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — მნიშვნელობა SI ერთეულებში: V, A, W, Hz, °C, %.
- `nd: true` — მონაცემი არ არის, აჩვენე «—».
- სერვერი აბრუნებს **ნედლ** მნიშვნელობებს დამრგვალების გარეშე; დამრგვალება და გაფერადება —
  კლიენტზეა.
- `batt`-ისთვის MAP-ში **უარყოფითი** დენი — დამუხტვა, **დადებითი** — გამოცმა (იხ. მაგალითი ზემოთ: მუხტის რეჟიმი, `i = −93.375`).

### 2.3. KES-ის ფილა (`kes[]`)

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

`panel` — პანელები, `batt` — აკუმულატორი. `f` KES-ისთვის ყოველთვის `0/nd`.

### 2.4. BMS-ის ფილა (`bms[]`)

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

- `pack.i` — დენი ნიშნით: გამოცმა `+`, დამუხტვა `−`.
- `cells_mv` — მასივი სიგრძით `cell_count` (მაგალითში შემოკლებულია).
- `cell_warn_mask`/`cell_prot_mask` — უჯრედების გაფრთხილებების/დაცვების ბიტური ნიღბები.

### 2.5. ინვერტორის ფილა (`grid[]`)

```json
{
  "id": 8,
  "gen": 1,
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "ავტოფარეხი",
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

`status` — **კავშირის** სტატუსი (`OK`/კავშირი არ არის), `inverter_status` — ინვერტორის
სამუშაო მდგომარეობა.

### 2.6. სხვა სიები

| ენდპოინტი | პასუხი |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

მაგალითი `/api/map/devices`:

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

მაგალითი `/api/bms/devices`:

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

`state=1` — მოწყობილობა იყო კავშირზე არა უგვიანეს 60 წამისა.

## 3. ნედლი სურათები (raw)

`raw` — ბოლო წარმატებული გამოკითხვის ბინარული სურათი. კლიენტი ვალდებულია შეამოწმოს
`id`+`gen` სიის მიხედვით; წინააღმდეგ შემთხვევაში შეიძლება მიიღოს სხვისი/მოძველებული სლოტი.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

payload-ის ზომაა `64 + ceil(402/8) + 402×4 = 1723` ბაიტი (`nfields=402`-ზე).
ქვემოთ — ფრაგმენტი (პირველი 80 ბაიტი).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

payload-ის სათაური:

| გადაადგილება | ზომა | ველი | მნიშვნელობა |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (მიმდინარე) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-ით დასრულებული სტრიქონი |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` მიკროარტი, `1` Modbus |
| 42 | 1 | `unit` | Modbus მისამართი |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | სლოტის თაობა |
| 56 | 8 | `lastSeen` | unix-დრო |
| 64 | `ceil(n/8)` | `presence` | ველის არსებობის ბიტური რუკა |
| `64+presence` | `n×4` | `values[]` | თითო ველზე `uint32` LE |

- `values[i]` — `id=i` ველის **ნედლი** მნიშვნელობა. მასშტაბი — კლიენტზე.
- `presence` — ბიტი `i` = 1, თუ ველი რეალურად წაიკითხა; დაკარგული
  მნიშვნელობებისთვის ჩვეულებრივ `values`-შია `0xFFFFFFFF`, მაგრამ მხოლოდ ამაზე დაყრდნობა არ შეიძლება.
- 402 ველის სრული სია (`id`, სახელი, მისამართი, ზომა) — `docs/map-contract.md`.
- ძირითადი ფორმულები — §3.1.1.

#### 3.1.1. ძირითადი ველების მასშტაბები

| კონტრაქტის ველი | id | ფორმულა |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` V |
| `MODE` | 36 | კომპოზიტური რეჟიმი (იხ. legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` V |
| `INET` | 148 | A, მაგნიტუდა |
| `TFNET` | 150 | `6250/V` Hz |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` A; ნიშანი: `PNET_Sign_P==0` → მიცემა (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` A; ნიშანი დამოკიდებულია `MODE_RAM`-ზე |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` A; ნიშანი — `H`-ის უფროსი ბიტი |
| `PNET_8` | 387 | `int16(...) × 10` W (ნიშნიანი) |

#### 3.1.2. დეკოდერი Python-ზე

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

Payload — **1536 ბაიტი**: სამი ქვებლოკი 512-512 ბაიტი `[KESE][KESR][KESS]`.
ქვემოთ — ფრაგმენტი (პირველი 32 ბაიტი).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, პასპორტი):

| off | ველი | ტიპი | მნიშვნელობა |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | სერიული ნომერი |
| 8 | `Device` | u8 | 5 KES-ისთვის |
| 9 | `Pow` | u8 | სიმძლავრე/ტიპი |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | W = მნიშვნელობა×100 |
| 12 | `ModelVolt` | u8 | V = მნიშვნელობა×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, ტელემეტრია, `off` — ბლოკის დასაწყისის მიმართ):

| off | ველი | ტიპი | მნიშვნელობა |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix-დრო |
| 14 | `RelayC` | u8 | რელეს 1/2/3 ბიტები |
| 15 | `RSErrSis` | u8 | კონტროლერის შეცდომები |
| 16 | `I2CErr` | u8 | I²C შეცდომა |
| 18 | `VcPV` | u16 LE | პანელების ძაბვა ×100 |
| 20 | `IcPV` | u16 LE | პანელების დენი ×100 |
| 22 | `VBat` | u16 LE | AKB ძაბვა ×100 |
| 24 | `PPV` | u16 LE | პანელების სიმძლავრე, W |
| 26 | `POut` | u16 LE | გამომავალი სიმძლავრე, W |
| 28 | `PLoad` | u16 LE | დატვირთვის სიმძლავრე, W |
| 30 | `PCurr` | u16 LE | დამუხტვის სიმძლავრე, W |
| 32 | `ICh` | u16 LE | დამუხტვის დენი ×100 |
| 34 | `IOut` | u16 LE | გამომავალი დენი ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | ენერგია დღე-ღამეში, W |
| 40 | `PwrKW` | u16 LE | ენერგია დღე-ღამეში, kW |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | გარე სენსორები |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — მონაცემები არ არის |
| 55 | `UakbCell[32]` | u16×32 | ქილების ძაბვები |
| 119 | `TakbCell[32]` | u8×32 | ქილების ტემპერატურები (`x−50`) |
| 151 | `QCell[32]` | u16×32 | ქილების განმუხტვის დენი |
| 215 | `ErrCells[32]` | u8×32 | BMS-ის შეცდომები |
| 247 | `IAkbMPPT[16]` | u16×16 | პარალელური MPPT-ების დენი |
| 279 | `ModeMPPT[16]` | u8×16 | MPPT რეჟიმი |
| 295 | `TakbMPPT[16]` | u8×16 | MPPT ტემპერატურა |
| 311 | `ErrMPPT[16]` | u8×16 | MPPT-ების შეცდომები |

`KESS` (პარამეტრები, 40×u16 LE, `off = 5 + 2×i`, RFC სტატუსი) — სრული სია
`docs/kes-contract.md` §`KESS`-ში.

ჯამური ენერგია დღე-ღამეში: `PwrKW×1000 + PwrW`.

#### 3.2.1. დეკოდერი Python-ზე

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

Payload — ზუსტად **107 ბაიტი**, მრავალბაიტიანი ველები — **big-endian**.
ქვემოთ — ფრაგმენტი (პირველი 80 ბაიტი; დანარჩენი 27 — `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | ზომა | ველი | ტიპი | ერთეული |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 დამუხტვის MOS, b1 განმუხტვის MOS, b2 ბალანსირება |
| 4 | 32 | `cells_mv[16]` | u16×16 | mV |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 A, განმუხტვა `+` |
| 56 | 4 | `power_w` | i32 | W |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 Ah |
| 68 | 4 | `remaining_mah` | u32 | 0.001 Ah |
| 72 | 4 | `rated_mah` | u32 | 0.001 Ah |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 V |
| 80 | 2 | `port_cv` | u16 | 0.01 V |
| 82 | 4 | `balance_mask` | u32 | ნიღაბი |
| 86 | 2 | `mode` | u16 | ქვედა ბაიტი — enum |
| 88 | 19 | `alarm` | u8×19 | ბიტველი, განაწილება ტიპის მიხედვით |

კანონიკური `mode`: `0` ლოდინი, `1` განმუხტვა, `2` დამუხტვა, `3` შენარჩუნება,
`4` სრული დამუხტვა, `5` გამორთვა.

`alarm` — 152 ბიტი; განაწილება დამოკიდებულია BMS-ის ტიპზე (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` განსაზღვრავენ მნიშვნელოვანი ელემენტების რაოდენობას; მასივების
კუდები უგულებელყოფილია. წარმოებულებს (min/max/avg) კლიენტი თავად ითვლის.

#### 3.3.1. დეკოდერი Python-ზე

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

ინვერტორის JSON-დოკუმენტი: ველები უკვე მასშტაბირებულია და ხელმოწერილია i18n-გასაღებებით.

```json
{
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "ავტოფარეხი",
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

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, სიხშირე,
  L1/L2/L3 (U/I), ტემპერატურები 1/2, მდგომარეობა, ავარიული სიტყვები 0..5.
- მნიშვნელობები — სტრიქონები, უკვე მასშტაბით; `nd` — მონაცემები არ არის.
- `alarm_words` — hex-სიტყვები, `alarms` — გაშიფრული კოდები.

## 4. ჩაწერა მოწყობილობებში

ჩაწერა ხელმისაწვდომია იმავე ტოკენით/cookie-ით. ქვემოთ — მხოლოდ კლიენტისთვის საჭირო
კონტრაქტები; შინაგანი დეტალები — `docs/map-write-api.md`-სა და `docs/kes-reader.md`-ში.

### 4.1. `POST /api/map/write`

ავტორიზაცია: cookie ან Bearer. ტანი — 64 KiB-მდე.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **მხოლოდ ერთი ტიპი**: ან `reg`, ან `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` hex-ში `0x`-ის
  გარეშე; `addr ≤ 3FF` — EEPROM, `≥400` — RAM; 256 ბაიტამდე (Modbus — 250-მდე).
- `cmd`: `{"op":"cmd","code":"07"}` — ზუსტად ერთი ბრძანება. კოდები: `1` OFF, `2` ON,
  `3` EEPROM WR (სამსახურებრივი), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (უსაფრთხო შემოწმება), `8` StatReset, `9` DischOff.
- მაქსიმუმ 64 ოპერაცია.
- `no_frame: true` — არ ჩაიცვას სამსახურებრივ ბრძანებებში (კლიენტს ჩვეულებრივ არ სჭირდება).

პასუხი:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP-სტატუსები: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

ტანი — როგორც `kes-reader`-ის control-სოკეტში (`docs/kes-reader.md` §4.2.3), web
ამატებს `cmd:"write"`-ს და პროქსირებს.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — მხოლოდ `op=reg`; `relay` ოპერაცია არ არსებობს.
- `addr`/`data` — hex `0x`-ის გარეშე; `addr` 0x0000..0xFFFF; `data` — 1..256 ბაიტი.
- მაქსიმუმ 64 ოპერაცია.
- KES-ის რელე — ბაიტური ნიღაბი `0x0E3D` უჯრაში (ბიტები 0/1/2); კლიენტი RMW-ს თავად აკეთებს.
- პასუხი — JSON `kes-reader`; HTTP-სტატუსი კოდის მიხედვით: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. სტატისტიკა

ყველა ენდპოინტი ავტორიზაციით. დრო — **unix-მილიწამები**.

| ენდპოინტი | პარამეტრები | პასუხი |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | იგივე, ფანჯარა «ბოლო N წუთი» |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (სტატისტიკის სიაში).
- `id` — მოწყობილობის სტრიქონული იდენტიფიკატორი (სერიული ნომერი/ლოგიკური ID).
- `source`: `raw` ან `avg` (`series`-ისთვის).
- `follow=1` — რეჟიმი «ONLINE», წყარო ყოველთვის `raw`, `depth` — წუთები (1..10,
  ნაგულისხმევი 5); `after` — მხოლოდ ახალი წერტილების დატვირთვა.
- `table`: `dir=from|to`, `limit` 1..2000 (ნაგულისხმევი 200), `detail=1` —
  დეტალური სტრიქონების ჩართვა, `rawmin` — raw-ის მინიმალური დრო.

`series`-ის მაგალითი:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

`series`-ის გასაღებები — ტელემეტრიის კონტრაქტის **რიცხვითი `field_id`** სტრიქონების სახით
(`"1"` = `UNET`, `"2"` = `INET`), და არა `map-contract`-ის მისამართები/უჯრების ID და არა ველების
სახელები. სახელები და ერთეულები — `columns`-ში ცხრილის. `field_id`-ების სრული სია ტიპების მიხედვით —
`docs/stat.md`. პარამეტრი `fields` იღებს იმავე რიცხვით id-ებს მძიმით; ცარიელი `fields` — ყველა ველი.

`table`-ის მაგალითი:

```json
{
  "status": 0,
  "columns": [
    {"id": "1", "name": "UNET", "type": 4, "unit": "V",
     "raw": false, "desc": "stats.f.map.UNET"},
    {"id": "2", "name": "INET", "type": 4, "unit": "A",
     "raw": false, "desc": "stats.f.map.INET"}
  ],
  "rows": [
    {"t": 1792353001000, "v": 1, "f": {"1": 218.0, "2": 25.0}}
  ]
}
```

- `columns[]` — `statsCol` ობიექტები; `id` — `field_id` (ტელემეტრიის კონტრაქტის რიგითი id),
  `type` — მნიშვნელობის ტიპის კოდი, `name` — ველის სახელი.
- `rows[]` — `statsRow` ობიექტები: `t` — ms, `v` — კონტრაქტის ვერსია, `f` — მნიშვნელობები
  `field_id`-ების მიხედვით (გასაღებები სტრიქონული).

## 6. შეცდომები

| კოდი | ტანი | როდის |
|---|---|---|
| `400` | `{"error":"bad_request"}` | გაფუჭებული JSON/პარამეტრები |
| `401` | `{"error":"unauthorized"}` | ტოკენის გარეშე/ვადაგასული |
| `404` | `{"error":"..."}` / ცარიელი | არ არის მოწყობილობა/სლოტი, `id+gen` არ დაემთხვა |
| `405` | — | არასწორი მეთოდი |
| `422` | `{"ok":false,"code":"..."}` | მოწყობილობის შეცდომა |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | მოწყობილობამ არ უპასუხა |
| `500` | `{"ok":false,"code":"internal"}` | შინაგანი შეცდომა |

## 7. კლიენტის რეკომენდებული ციკლი

1. `POST /api/token` → ტოკენი.
2. `GET /api/<type>/devices` (ან `/api/mainlist`) → `id/gen`-ის სია.
3. `GET /api/<type>/raw?id&gen` → ბინარული სურათი. `grid`-სთვის raw-ენდპოინტი არ არსებობს:
   სურათი იღება `GET /api/grid/device?serial=`-ით.
4. კონტრაქტის მიხედვით გაშიფვრა; `404`-ზე — სიის ხელახლა წაკითხვა.
5. ისტორიისთვის — `/api/stats/devices` და `/api/stats/series|table`.
6. ჩაწერისთვის — `POST /api/map/write` ან `/api/kes/write` იმავე `id/gen`-ით.

## 8. ბმულები

- [api-legacy.md](api-legacy.md) — legacy-თავსებადობა.
- `docs/api-auth.md` — ავტორიზაცია და ტოკენების საცავი.
- `docs/map-contract.md` — MAP-ის სრული კონტრაქტი (402 ველი).
- `docs/kes-contract.md` — KES-ის კონტრაქტი (KESE/KESR/KESS).
- `docs/bms-reader.md` — BMS-ის კონტრაქტი (107 ბაიტი).
- `docs/grid-reader.md` — ინვერტორები.
- `docs/map-write-api.md` — ჩაწერა MAP-ში.
- `docs/kes-reader.md` — ჩაწერა KES-ში.