# API: native (lists, raws, writing)

Document for client developers. Describes `malina-web`'s own HTTP-API:
authorization, device lists, raw snapshots (`raw`), writing to devices and
statistics. For legacy compatibility, see [api-legacy.md](api-legacy.md).

- Base: `http://<device-address>`.
- All responses except `raw` are JSON (`application/json; charset=utf-8`).
- `raw` — binary data (`application/octet-stream`) per the `map-reader`/`kes-reader`/
  `bms-reader` contracts.
- Versioning: the `raw` contract is fixed by magic/version in the header; lists
  are versioned by fields, backward compatibility is not guaranteed when the
  contract changes.

## 1. Authorization

A single mechanism: the **PAM password of the `malina` user** (the same as for the
web and SSH). There are no roles: any valid token means full access.

Two methods are supported:

1. **Cookie session** `malina_session` — for the browser. `HttpOnly`, `SameSite=Lax`,
   TTL 1 hour, refreshed on every request.
2. **Bearer token** — for software clients.

### 1.1. Getting a token

```http
POST /api/token
Content-Type: application/json

{"password": "PASSWORD"}
```

Success:

```json
{
  "token": "64-character hex string",
  "expires": 1792358872
}
```

`expires` — the unix time until which the token will live **while idle**. This is not
a hard deadline: the TTL is sliding (see §1.3).

Errors:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. Usage

```http
Authorization: Bearer <token>
```

```bash
TOKEN=$(curl -s -X POST http://10.10.100.254/api/token \
  -H 'Content-Type: application/json' \
  -d '{"password":"PASSWORD"}' | jq -r .token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://10.10.100.254/api/mainlist'
```

### 1.3. Sliding TTL and revocation

- The token lives **1 hour from the last use**, not from issue.
- Any authorized request updates `lastUsed`.
- There is no absolute maximum.
- Only the `sha256` of the token is stored in `/dev/shm/malina-auth`; on reboot all
  tokens disappear, on `malina-web` restart — they are kept.
- Revoking your own token:

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- Changing the password revokes **all** sessions and tokens.
- `GET /api/token` is not supported → `405`.

### 1.4. Authorization errors

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

For HTML pages instead of 401 a redirect to `/login` is used.
The `WWW-Authenticate` header is set on `/api/token`; some old
API handlers (for example, `/api/mainlist`) respond with a "bare" `401` without a body
— the client must rely on the status.

## 2. Device lists (lists)

The general idea: the client gets the list of devices, takes the pair **`id` + `gen`**
and requests `raw` by it. `id` is the slot index; `gen` is the "lease generation" of
the slot. The pair is unique in time: after the device reconnects `gen` changes, and
the old `raw` returns `404`.

### 2.1. `GET /api/mainlist`

A summary of all devices for the main page. Under authorization.

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

Schema:

| Field | Type | Meaning |
|---|---|---|
| `updated` | int64 | unix time of response assembly |
| `maps[]` | array | MAP tiles |
| `kes[]` | array | KES tiles |
| `bms[]` | array | BMS tiles |
| `grid[]` | array | grid inverter tiles |

### 2.2. MAP tile (`maps[]`)

| Field | Type | Meaning |
|---|---|---|
| `id` | int | slot index |
| `gen` | uint64 | slot generation (for `/api/map/raw`) |
| `title` | string | localized label |
| `mode` | string | mode text |
| `mode_num` | uint32 | composite code 0..18 |
| `soc` | float/null | battery charge, % |
| `grid`, `batt`, `load` | block | blocks of 5 values |
| `online` | bool | `lastSeen` is not older than 2 minutes |

`block`:

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — value in SI units: V, A, W, Hz, °C, %.
- `nd: true` — no data, show "—".
- The server returns **raw** values without rounding; rounding and coloring are on
  the client.
- For the MAP `batt`, a **negative** current is charge, a **positive** current is
  discharge (see the example above: charge mode, `i = −93.375`).

### 2.3. KES tile (`kes[]`)

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

`panel` — panels, `batt` — battery. `f` for KES is always `0/nd`.

### 2.4. BMS tile (`bms[]`)

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

- `pack.i` — current with sign: discharge `+`, charge `−`.
- `cells_mv` — array of length `cell_count` (shortened in the example).
- `cell_warn_mask`/`cell_prot_mask` — bit masks of per-cell warnings/protections.

### 2.5. Inverter tile (`grid[]`)

```json
{
  "id": 8,
  "gen": 1,
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "Garage",
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

`status` — **link** status (`OK`/no link), `inverter_status` — the inverter operating
state.

### 2.6. Other lists

| Endpoint | Response |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

Example `/api/map/devices`:

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

Example `/api/bms/devices`:

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

`state=1` — the device was in contact no later than 60 seconds ago.

## 3. Raw snapshots (raw)

`raw` — a binary snapshot of the last successful poll. The client must check
`id`+`gen` against the list; otherwise it can get a foreign/stale slot.

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Payload size — `64 + ceil(402/8) + 402×4 = 1723` bytes (for `nfields=402`).
Below is a fragment (the first 80 bytes).

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Payload header:

| Offset | Size | Field | Value |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"` (`0x4441504D` LE) |
| 4 | 2 | `version` | `5` (current) |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL-terminated string |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` MicroArt, `1` Modbus |
| 42 | 1 | `unit` | Modbus address |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | slot generation |
| 56 | 8 | `lastSeen` | unix time |
| 64 | `ceil(n/8)` | `presence` | field presence bit map |
| `64+presence` | `n×4` | `values[]` | `uint32` LE per field |

- `values[i]` — **raw** value of the field `id=i`. Scaling is on the client.
- `presence` — bit `i` = 1 if the field was actually read; for absent
  values it is usually `0xFFFFFFFF` in `values`, but you cannot rely on that alone.
- The full list of 402 fields (`id`, name, address, size) — `docs/map-contract.md`.
- Key formulas — §3.1.1.

#### 3.1.1. Scales of key fields

| Contract field | id | Formula |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` V |
| `MODE` | 36 | composite mode (see legacy §3.3) |
| `UNET` | 147 | `0`/`V+100` V |
| `INET` | 148 | A, magnitude |
| `TFNET` | 150 | `6250/V` Hz |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` A; sign: `PNET_Sign_P==0` → export (`−`) |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` A; sign depends on `MODE_RAM` |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` A; sign — high bit of `H` |
| `PNET_8` | 387 | `int16(...) × 10` W (signed) |

#### 3.1.2. Python decoder

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

Payload — **1536 bytes**: three sub-blocks of 512 bytes `[KESE][KESR][KESS]`.
Below is a fragment (the first 32 bytes).

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE` (EEPROM, passport):

| off | field | type | meaning |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | serial number |
| 8 | `Device` | u8 | 5 for KES |
| 9 | `Pow` | u8 | power/type |
| 10 | `FW` | u8 | `major=v&0x0F`, `minor=v>>5` |
| 11 | `ModelPow` | u8 | W = value×100 |
| 12 | `ModelVolt` | u8 | V = value×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR` (RAM, telemetry, `off` relative to the block start):

| off | field | type | meaning |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix time |
| 14 | `RelayC` | u8 | relay bits 1/2/3 |
| 15 | `RSErrSis` | u8 | controller errors |
| 16 | `I2CErr` | u8 | I²C error |
| 18 | `VcPV` | u16 LE | panel voltage ×100 |
| 20 | `IcPV` | u16 LE | panel current ×100 |
| 22 | `VBat` | u16 LE | battery voltage ×100 |
| 24 | `PPV` | u16 LE | panel power, W |
| 26 | `POut` | u16 LE | output power, W |
| 28 | `PLoad` | u16 LE | load power, W |
| 30 | `PCurr` | u16 LE | charge power, W |
| 32 | `ICh` | u16 LE | charge current ×100 |
| 34 | `IOut` | u16 LE | output current ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | energy for the day, W |
| 40 | `PwrKW` | u16 LE | energy for the day, kW |
| 42..49 | `IExts0/1`, `PExts0/1` | u16 LE | external sensors |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — no data |
| 55 | `UakbCell[32]` | u16×32 | cell voltages |
| 119 | `TakbCell[32]` | u8×32 | cell temperatures (`x−50`) |
| 151 | `QCell[32]` | u16×32 | cell discharge current |
| 215 | `ErrCells[32]` | u8×32 | BMS errors |
| 247 | `IAkbMPPT[16]` | u16×16 | parallel MPPT current |
| 279 | `ModeMPPT[16]` | u8×16 | MPPT mode |
| 295 | `TakbMPPT[16]` | u8×16 | MPPT temperature |
| 311 | `ErrMPPT[16]` | u8×16 | MPPT errors |

`KESS` (settings, 40×u16 LE, `off = 5 + 2×i`, RFC status) — the full list in
`docs/kes-contract.md` §`KESS`.

Total energy for the day: `PwrKW×1000 + PwrW`.

#### 3.2.1. Python decoder

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

Payload is exactly **107 bytes**, multi-byte fields are **big-endian**.
Below is a fragment (the first 80 bytes; the remaining 27 are `alarm`).

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | size | field | type | unit |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 charge MOS, b1 discharge MOS, b2 balancing |
| 4 | 32 | `cells_mv[16]` | u16×16 | mV |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 A, discharge `+` |
| 56 | 4 | `power_w` | i32 | W |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 Ah |
| 68 | 4 | `remaining_mah` | u32 | 0.001 Ah |
| 72 | 4 | `rated_mah` | u32 | 0.001 Ah |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 V |
| 80 | 2 | `port_cv` | u16 | 0.01 V |
| 82 | 4 | `balance_mask` | u32 | mask |
| 86 | 2 | `mode` | u16 | low byte — enum |
| 88 | 19 | `alarm` | u8×19 | bitfield, layout by type |

Canonical `mode`: `0` standby, `1` discharge, `2` charge, `3` holding,
`4` full charge, `5` shutdown.

`alarm` — 152 bits; the layout depends on the BMS type (`docs/bms/<TYPE>.md`).
`cell_count`/`temp_count` set the number of significant elements; array tails are
ignored. Derived values (min/max/avg) are computed by the client.

#### 3.3.1. Python decoder

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

JSON inverter document: fields are already scaled and keyed by i18n keys.

```json
{
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "Garage",
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

- `fields[].id` — 1..24: PV1/PV2 (U/I/P), AC active/reactive, frequency,
  L1/L2/L3 (U/I), temperatures 1/2, state, alarm words 0..5.
- Values are strings, already scaled; `nd` — no data.
- `alarm_words` — hex words, `alarms` — decoded codes.

## 4. Writing to devices

Writing is available with the same token/cookie. Below are only the contracts the
client needs; internal details — in `docs/map-write-api.md` and `docs/kes-reader.md`.

### 4.1. `POST /api/map/write`

Authorization: cookie or Bearer. Body — up to 64 KiB.

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **only one type**: either `reg` or `cmd`.
- `reg`: `{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` in hex without
  `0x`; `addr ≤ 3FF` — EEPROM, `≥400` — RAM; up to 256 bytes (Modbus — up to 250).
- `cmd`: `{"op":"cmd","code":"07"}` — exactly one command. Codes: `1` OFF, `2` ON,
  `3` EEPROM WR (service), `4` ChargeOFF, `5` ChargeON, `6` Reset, `7`
  Call_load_EEProm (safe check), `8` StatReset, `9` DischOff.
- Maximum 64 operations.
- `no_frame: true` — do not frame with service commands (usually not needed by the
  client).

Response:

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP statuses: `200` ok, `400` bad_request, `401` unauthorized, `404` no_device,
`422` map_error, `502` retry_exhausted, `500` internal.

### 4.2. `POST /api/kes/write`

Body — same as the control socket of `kes-reader` (`docs/kes-reader.md` §4.2.3), the
web adds `cmd:"write"` and proxies.

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — only `op=reg`; there is no `relay` operation.
- `addr`/`data` — hex without `0x`; `addr` 0x0000..0xFFFF; `data` — 1..256 bytes.
- Maximum 64 operations.
- KES relay — a byte mask at cell `0x0E3D` (bits 0/1/2); the client does the RMW itself.
- Response — `kes-reader` JSON; HTTP status by code: `200` ok, `400` bad_request,
  `401`, `404` no_device, `422` kes_error, `502` retry_exhausted, `500`.

## 5. Statistics

All endpoints under authorization. Time — **unix milliseconds**.

| Endpoint | Parameters | Response |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | same, "last N minutes" window |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`: `map`, `kes`, `bms`, `grid`, `mbms` (in the statistics list).
- `id` — string device identifier (serial number/logical ID).
- `source`: `raw` or `avg` (for `series`).
- `follow=1` — "ONLINE" mode, source is always `raw`, `depth` — minutes (1..10,
  default 5); `after` — loading only new points.
- `table`: `dir=from|to`, `limit` 1..2000 (default 200), `detail=1` —
  include detailed rows, `rawmin` — minimum raw time.

Example `series`:

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

The `series` keys — **numeric `field_id` of the telemetry contract** as strings
(`"1"` = `UNET`, `"2"` = `INET`), not the addresses/IDs of `map-contract` cells and not
field names. Names and units are in the `columns` of the table. The full list of
`field_id` by type — `docs/stat.md`. The `fields` parameter accepts the same numeric
ids comma-separated; empty `fields` — all fields.

Example `table`:

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

- `columns[]` — `statsCol` objects; `id` — `field_id` (ordinal contract
  id of the telemetry), `type` — value type code, `name` — the field name.
- `rows[]` — `statsRow` objects: `t` — ms, `v` — contract version, `f` — values
  by `field_id` (string keys).

## 6. Errors

| Code | Body | When |
|---|---|---|
| `400` | `{"error":"bad_request"}` | malformed JSON/parameters |
| `401` | `{"error":"unauthorized"}` | missing/expired token |
| `404` | `{"error":"..."}` / empty | no device/slot, `id+gen` mismatch |
| `405` | — | wrong method |
| `422` | `{"ok":false,"code":"..."}` | device error |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | device did not respond |
| `500` | `{"ok":false,"code":"internal"}` | internal error |

## 7. Recommended client loop

1. `POST /api/token` → token.
2. `GET /api/<type>/devices` (or `/api/mainlist`) → list of `id/gen`.
3. `GET /api/<type>/raw?id&gen` → binary snapshot. For `grid` there is no raw
   endpoint: the snapshot is taken via `GET /api/grid/device?serial=`.
4. Decode per the contract; on `404` — re-read the list.
5. For history — `/api/stats/devices` and `/api/stats/series|table`.
6. For writing — `POST /api/map/write` or `/api/kes/write` with the same `id/gen`.

## 8. References

- [api-legacy.md](api-legacy.md) — legacy compatibility.
- `docs/api-auth.md` — authorization and token storage.
- `docs/map-contract.md` — full MAP contract (402 fields).
- `docs/kes-contract.md` — KES contract (KESE/KESR/KESS).
- `docs/bms-reader.md` — BMS contract (107 bytes).
- `docs/grid-reader.md` — inverters.
- `docs/map-write-api.md` — writing to the MAP.
- `docs/kes-reader.md` — writing to a KES device.