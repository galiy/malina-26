# API: legacy compatibility (full API)

Document for client developers. Describes the entire old HTTP-API of "Malina"
(read and write), which `malina-web` supports for compatibility with
"Master-Node" and external consumers. The new API is described in [api-native.md](api-native.md).

- Base: `http://<device-address>` (TLS is an external layer, not considered here).
- Legacy response format — **`text/html; charset=UTF-8`**, but the body contains JSON or
  text (`Ok`/`error`). This is historical behavior, not a bug.
- Legacy read — **without authorization**; legacy write — **HTTP Basic + PAM**.

## 1. Authorization matrix

| Endpoint | Method | Authorization |
|---|---|---|
| `/read_json.php?device=map` | GET | none |
| `/read_json.php?device=mppt` | GET | none |
| `/read_json.php?device=bat` | GET | none |
| `/read_memory.php` | GET | none |
| `/check_daemons.php` | GET | none |
| `/read_bms.php` | GET | none |
| `/read_sec.php` | GET | none |
| `/write_eeprom.php` | POST | **Basic** (`malina` + password → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + password → PAM) |

- The Basic user is fixed: **`malina`**. The password is the same as for the web and
  SSH. Verification goes through PAM; successful checks are cached for 5 minutes.
- Invalid/missing Basic → `401`.
- Bearer token for legacy write **is not accepted** (Basic only).
- No rate-limit and no lockout.

```bash
# read — no authorization
curl -s 'http://10.10.100.254/read_json.php?device=map'

# write — Basic
curl -s -u 'malina:PASSWORD' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. Binding to the MAP

The legacy module is bound to **one MAP** by the numeric serial number (the serial
letter is ignored). `read_json.php`, `read_memory.php` and `write_eeprom.php` work
exactly with it. If the MAP is lost, the module waits 60 seconds, then picks up the
first available one. If there is no binding, write returns `error`.

## 3. `GET /read_json.php?device=map`

A snapshot of MAP fields in the old `mapd` format. Refreshed roughly once a second.

- `device=map` — the only supported value for the MAP.
- Other values → `400 {"error":"unsupported device"}`.
- All values are **strings** (historical format).
- `timestamp` — unix time of the snapshot, `time` — `HH:MM:SS`.

### 3.1. Example

```http
GET /read_json.php?device=map
Content-Type: text/html; charset=UTF-8
```

```json
{
  "timestamp": "1792355272",
  "time": "23:27:52",
  "_UID": "00000",
  "_MODE": "6",
  "_Status_Char": "2",
  "_Uacc": "53.5",
  "_Uch_T": "57.6",
  "_Ubuf_T": "53.6",
  "_Iacc": "92",
  "_PLoad": "6900",
  "_PLoad_calc": "5002.2",
  "_F_Acc_Over": "0",
  "_F_Net_Over": "0",
  "_UNET": "218",
  "_INET": "25",
  "_PNET": "4850",
  "_PNET_calc": "5450.0",
  "_TFNET": "50.0",
  "_UOUTmed": "215",
  "_RSErrSis": "0",
  "_RSErrJobM": "0",
  "_RSErrJob": "0",
  "_RSWarning": "0",
  "_Temp_Grad0": "27",
  "_Temp_Grad1": "59",
  "_Temp_Grad2": "68",
  "_INET_16_4": "25.0",
  "_IAcc_med_A_u16": "93.5",
  "_I_mppt_avg": "29.6",
  "_P_mppt_avg": "1582",
  "_I_acc_3ph": "6.0",
  "_I_ph1": "-93.0",
  "_I_ph2": "-3155.4",
  "_I_ph3": "3254.4",
  "fw": "30.0",
  "_Relay1": "0",
  "_Relay2": "0"
}
```

Our implementation returns a **fixed** set of keys; keys not present in the example
are listed in §3.4.

### 3.2. Key fields and formulas

| Key | Meaning | Formula/note |
|---|---|---|
| `_UID` | MAP serial number | `SerialNum0 + SerialNum1×256` |
| `_MODE` | composite mode | 0..18, see §3.3 |
| `_Status_Char` | charge bits | 1 — small current, 2 — charge needed, 4 — stage 2, 8 — top-up charge |
| `_Uacc` | battery voltage, V | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | charge setpoints, V | `(V << UACC)/10` |
| `_Iacc` | battery current, A | `IAcc_med_A_2 × 2`, unsigned magnitude |
| `_IAcc_med_A_u16` | battery current with sign, A | `+` charge, `−` discharge |
| `_PLoad` | load power, W | `PLoad_L × 100`, unsigned magnitude |
| `_PLoad_calc` | battery power, W | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | grid voltage, V | `0` — no grid, otherwise `V+100` |
| `_INET` | grid current, A | magnitude |
| `_INET_16_4` | grid current with sign, A | sign from `PNET_Sign_P`: consumption `+`, export `−` |
| `_PNET` | grid power, W | `int16(PNET_8) × 10` (signed) |
| `_PNET_calc` | grid power, W | `UNET × |INET_16_4|`, sign from `PNET_Sign_P` |
| `_TFNET` | grid frequency, Hz | `6250/V` |
| `_UOUTmed` | load voltage, V | `0` — none, otherwise `V+100` |
| `_Temp_Grad0/1/2` | temperatures, °C | `V−50` |
| `_I2C_Err` | I²C error | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **total** KES current, A | historical name; originally it was the average |
| `_P_mppt_avg` | KES power, W | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | phase currents, A | `((L+(H&0x7F)×256)/10)`, sign — bit `0x80` |
| `_I_acc_3ph` | sum of phase currents, A | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | relays | bits 0/1 of `Status_RELEdop` |
| `fw` | firmware version | `(V&0x1F).(V>>5)` |

**Important (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — now the **sum** across
KES devices, not the average. Consumers that expected the average need to divide by
the number of active KES devices.

**Important (2026-10-18):** `_INET_16_4` is returned with a sign: consumption `+`,
export `−`. For the magnitude use `abs(_INET_16_4)`.

### 3.3. `_MODE` modes

| Code | Text |
|---|---|
| 0 | MAP off and no grid at the input |
| 1 | MAP off. Grid at the input |
| 2 | MAP on. Generation from battery. No grid |
| 3 | MAP on and passes through the grid |
| 4 | Pass-through + charge |
| 5 | Buffer charge |
| 6 | Charge, stage 1 |
| 7 | Charge, stage 2 |
| 8 | Top-up charge, stage 3 |
| 9 | Other charging modes (`StatusCh` dec/bin) |
| 10 | Forced generation |
| 11 | Tariff grid, maximum tariff, forced generation |
| 12 | Tariff grid, minimum tariff |
| 13 | Pass-through + eco-boost |
| 14 | Pass-through + export to grid |
| 15 | Waiting for external full charge |
| 16 | Tariff grid, pass-through + eco-boost |
| 17 | Tariff grid, pass-through + export to grid |
| 18 | Boost Pmax |

### 3.4. Other keys

Additionally always returned: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` and their `_B`-versions,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — overload bitfields; `_UNET_Limit`, `_TFNET_Limit` —
setpoints; `_RSErr*`, `_RSWarning` — errors/warnings; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — auxiliary; `_CoolerSpeed`, `Temp_off` —
auxiliary.

## 4. `GET /read_json.php?device=mppt`

A snapshot of all found KES devices. The response is a JSON array of objects (one per
KES device). All values are **strings**. Empty slots (length ≤ 10) are dropped.

```json
[
  {
    "timestamp": "1792355271",
    "time": "23:27:51",
    "UID": "00000",
    "Vc_PV": "103.9",
    "Ic_PV": "5.7",
    "V_Bat": "57.1",
    "P_PV": "592",
    "P_Out": "580",
    "P_Load": "0",
    "P_curr": "580",
    "I_Ch": "10.2",
    "I_Out": "10.2",
    "Temp_Int": "14",
    "Temp_Bat": "2",
    "Pwr_kW": "150.18951",
    "I_EXTS0": "0.0",
    "I_EXTS1": "0.0",
    "P_EXTS0": "0",
    "P_EXTS1": "0",
    "Relay_C": "5",
    "RSErrSis": "0",
    "Sign_C0": "0",
    "Sign_C1": "0",
    "Mode": "b",
    "Sign": "-",
    "MPP": "R",
    "windspeed": "65535",
    "FW": "8.5",
    "R1": "1",
    "R2": "0",
    "R3": "1"
  }
]
```

| Field | Meaning |
|---|---|
| `UID` | KES serial number |
| `Vc_PV`, `Ic_PV` | panel voltage/current |
| `V_Bat` | battery voltage |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | power of panels/output/load/charge, W |
| `I_Ch`, `I_Out` | charge/output currents |
| `Temp_Int`, `Temp_Bat` | temperatures (`x−50`) |
| `Pwr_kW` | energy for the day, kWh (fractional) |
| `I_EXTS0/1`, `P_EXTS0/1` | external current/power sensors |
| `Relay_C` | relay bits (0/1/2) |
| `RSErrSis` | controller errors |
| `Sign_C0`, `Sign_C1` | sensor signs; always `0`, not published in the KES contract |
| `Mode` | charge mode (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | current sign (`+`/`-`) |
| `MPP` | MPP point (`R`/`L`/`0`) |
| `windspeed` | wind generator frequency; `65535` — no data |
| `FW` | firmware version |
| `R1..R3` | relay state |

For `device` outside `{map, mppt, bat}` — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

An emulation of `batmon` for "Master-Node". The response is an array of **two**
objects. All values are strings.

```json
[
  {
    "timestamp": "1792355272",
    "time": "23:27:52",
    "C20": "0.0",
    "C20_t": "0.0",
    "Uacc_avg": "0.00",
    "Iavg": "0.00",
    "C_Ah_remain": "0.0",
    "C_100_remain": "82.2",
    "C(I)": "0.0",
    "TTG": "0",
    "mppt_day_E": "489851",
    "low_V": "0.0",
    "hi_V": "0.0",
    "cycles": "0",
    "LD": "0.0",
    "DD": "0.0",
    "AD": "0.0",
    "Esum_from_bat": "0",
    "Esum_to_bat": "0",
    "No_async": "0",
    "est_soc": "82.2",
    "cons_Ah": "0.0",
    "E_wind_day": "0"
  },
  {
    "timestamp": "1792355272",
    "time": "23:27:52",
    "dCdt": "0.0000",
    "dEdt": "0.0000",
    "Isumm": "0.000",
    "IntegralC_0_t": "0.0000",
    "Integral_E_0_t": "0.0000",
    "t_bat": "0",
    "Ah_user": "0.0",
    "Wh_user": "0",
    "Sun_I": "0.0",
    "Sun_P": "0",
    "wind_I": "0.0",
    "wind_P": "0"
  }
]
```

- `C_100_remain` — charge estimate, % (hardware `SoC`, otherwise OCV).
- `mppt_day_E` — total KES output for the day, Wh
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — charge estimate, %.
- The other cumulative fields are zeros: we do not have the corresponding counters.

## 6. `GET /read_memory.php?offset=&count=`

Bytes of the memory image of the bound MAP.

- `offset` ≥ 0, `count` ≥ 0.
- Boundaries are clamped to the image size (`0x000..0x7FF`).
- The response is a JSON array of bytes `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Daemon statuses in the old `check_daemons.php` format.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Key | Value |
|---|---|
| `mapd` | `0` — MAP available (found or has a snapshot), `1` — no |
| `mpptd0..3` | `0` — KES present, `1` — no (limit of 4 slots) |
| `batmond` | `0` — KES present (batmon emulation), `1` — no |
| `bms` | `0` — MAP snapshot has BMS banks, `1` — no |
| `"0".."7"` | status bytes of shm 5000: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | absent on our side → `1` |

## 8. `GET /read_bms.php`

A stateless snapshot of the ANT BMS, assembled from the current MAP snapshot (the
first available one). The `bmslistener` daemon format.

```json
{
  "updated": 1792355272,
  "devices": [
    {
      "deviceName": "AntBms 500 A/h (Dominator)",
      "port": "/dev/ttyUSB2",
      "timestamp": 1792355272,
      "time": "23:27:52",
      "cell_count": 16,
      "cells_v": [3.340, 3.340, 3.340, 3.340],
      "current_a": 93.50,
      "soc": 82,
      "capacity_ah": 500.00,
      "remaining_ah": 411.00,
      "temperatures_c": [27, 27, 27, 26],
      "charge_mos": 0,
      "discharge_mos": 0,
      "balancer": 0,
      "power_w": 5077.1,
      "max_cell_idx": 0,
      "max_cell_v": 3.340,
      "min_cell_idx": 14,
      "min_cell_v": 3.330,
      "avg_cell_v": 3.339,
      "frames": 0
    }
  ]
}
```

- `devices` — no more than one element.
- No MAP/snapshot → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — variable-length arrays (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — always `0` (the MAP snapshot does
  not contain these flags).

## 9. `GET /read_sec.php?id=&offset=&count=`

Bytes of the virtual address space of a KES device.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` is optional (default 1), `offset+count ≤ 0xE80`.
- For `offset=0` — JSON array of bytes; for `offset>0` — JSON object
  `{"<dec-address>":<byte>, ...}`.
- Error (no `id`/`offset`, out of range) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

The virtual space is assembled from the `KESE+KESR+KESS` snapshots: a zero array of
size `0xE80` into which values are laid out by their original RAM addresses.

## 10. `POST /write_eeprom.php`

Writing to the bound MAP. Authorization — **Basic**.

Body: `application/x-www-form-urlencoded`, field `data`:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **decimal**.
- `addr == 0` → MAP command, `val` — the command code (1..9).
- `addr != 0` → write a byte: `addr` 0..0x7FF, `val` 0..255.
- The order of pairs is preserved; consecutive register writes are combined into one
  request, each command goes separately.
- The limit of one request to `map-reader` — **≤64 operations**; long series are cut
  automatically.
- The client response timeout — **10 s**. During a long write the client may get
  `error`, and the operation will finish later.

Response: `text/html; charset=UTF-8`, text `Ok` or `error`.

```bash
# turn on the MAP (command ON=2)
curl -s -u 'malina:PASSWORD' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# set the clock to 09:54 (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:PASSWORD' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Command codes and registers — `docs/map-write.md`. After writing, the module reads the
MAP again and rebuilds the `read_json.php`/`read_memory.php` snapshots.

## 11. `GET|POST /write_sec.php`

Writing to a KES device. Authorization — **Basic**.

### 11.1. Relays

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- The current state is read from `Relay_C` (0x3E3); if the relay is already in the
  desired state — early `Ok` without writing.
- Otherwise RMW the bits and write the state byte to `0x0E3D` (3645).

### 11.2. Arbitrary cells

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — decimal.
- If `relay` is passed, `data` is ignored.

Response: `Ok`, `error` or `unread`. `unread` (queue 2200) in our API is replaced
with `error`.

```bash
curl -s -u 'malina:PASSWORD' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. What is not supported from the old API

Only the handlers above were ported. The remaining legacy endpoints are not
implemented; some of them relate to subsystems we do not have (MPPT history, batmon,
Emoncms, cloud, USB backup, updater).

| Group | Not supported |
|---|---|
| System/network | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Update/backup | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| History/energy | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Menu/settings | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Cloud | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Auxiliary | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Current alternatives: system state — `/api/system`, logs — `/api/log`,
services — `/api/services`, network — the web page `/network`, history — `/api/stats/*`
(see [api-native.md](api-native.md)).

## 13. Errors and codes

| Situation | Response |
|---|---|
| Invalid `device` in `read_json.php` | `400 {"error":"unsupported device"}` |
| Out of range `read_memory`/`read_sec` | `error` or a clamped array |
| Missing/invalid Basic in write | `401` (`WWW-Authenticate: Basic`) |
| Bad `data`, no MAP binding, failure/timeout | text `error` |
| Method not GET/POST | `405` |

## 14. Mini Python decoder

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. References

- [api-native.md](api-native.md) — the new API (lists/raws, tokens).
- `docs/api-auth.md` — authorization matrix.
- `docs/legacy/legacy_read_json_map.md` — full mapping of `read_json.php?device=map`.
- `docs/legacy/kes-legacy-malina2.md` — analysis of the legacy KES handlers.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — `bat`/`read_bms` contracts.
- `docs/map-write.md`, `docs/map-write-api.md` — writing to the MAP.
- `docs/legacy/legacy-endpoints.md` — inventory of non-ported handlers.