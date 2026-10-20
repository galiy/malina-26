# API：native（lists、raws、写入）

本文档面向客户端开发者。描述 `malina-web` 自身的 HTTP API：
授权、设备列表、原始快照（`raw`）、写入设备和统计。legacy 兼容 —
[api-legacy.md](api-legacy.md)。

- 基础地址：`http://<设备地址>`。
- 除 `raw` 外的所有响应都是 JSON（`application/json; charset=utf-8`）。
- `raw` — 按 `map-reader`/`kes-reader`/`bms-reader` 契约的二进制数据
  （`application/octet-stream`）。
- 版本管理：`raw` 契约由头部中的 magic/version 固定；列表
  靠字段进行版本管理，契约变更时向后兼容不保证。

## 1. 授权

单一机制：**用户 `malina` 的 PAM 密码**（与 Web 和 SSH 相同）。
没有角色：任何有效令牌即完全访问。

支持两种方式：

1. **Cookie 会话** `malina_session` — 供浏览器使用。`HttpOnly`、`SameSite=Lax`，
   TTL 1 小时，每次请求时刷新。
2. **Bearer 令牌** — 供程序客户端使用。

### 1.1. 获取令牌

```http
POST /api/token
Content-Type: application/json

{"password": "PASSWORD"}
```

成功：

```json
{
  "token": "64 个字符的十六进制字符串",
  "expires": 1792358872
}
```

`expires` — 令牌在**空闲**状态下存活的 unix 时间。这不是
硬性截止：TTL 是滑动的（见 §1.3）。

错误：

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_grant"

{"error": "invalid_credentials"}
```

```json
HTTP/1.1 400 Bad Request
{"error": "bad_request"}
```

### 1.2. 使用

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

### 1.3. 滑动 TTL 与撤销

- 令牌**自上次使用起存活 1 小时**，而不是自发行起。
- 任何已授权的请求都会更新 `lastUsed`。
- 没有绝对上限。
- 在 `/dev/shm/malina-auth` 中只存储令牌的 `sha256`；重启后所有
  令牌消失，重启 `malina-web` — 保留。
- 撤销自己的令牌：

```http
DELETE /api/token
Authorization: Bearer <token>
```

```json
{"ok": true}
```

- 修改密码会撤销**所有**会话和令牌。
- `GET /api/token` 不支持 → `405`。

### 1.4. 授权错误

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="malina", error="invalid_token"
Content-Type: application/json

{"error": "unauthorized"}
```

对于 HTML 页面，不使用 401，而是重定向到 `/login`。
`WWW-Authenticate` 头在 `/api/token` 上设置；部分旧的
API 处理器（例如 `/api/mainlist`）用无正文的「裸」`401` 响应 —
客户端必须以状态码为准。

## 2. 设备列表（lists）

总体思路：客户端获取设备列表，取**`id` + `gen`**这一对，
并据此请求 `raw`。`id` — 槽索引；`gen` — 槽的「租约代数」。
这一对在时间上唯一：设备重新连接后 `gen` 改变，旧的 `raw` 返回 `404`。

### 2.1. `GET /api/mainlist`

供主页使用的所有设备汇总。需授权。

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

结构：

| 字段 | 类型 | 含义 |
|---|---|---|
| `updated` | int64 | 响应组装的 unix 时间 |
| `maps[]` | array | MAP 卡片 |
| `kes[]` | array | KES 卡片 |
| `bms[]` | array | BMS 卡片 |
| `grid[]` | array | 电网逆变器卡片 |

### 2.2. MAP 卡片（`maps[]`）

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | int | 槽索引 |
| `gen` | uint64 | 槽代数（供 `/api/map/raw`） |
| `title` | string | 本地化标签 |
| `mode` | string | 模式文本 |
| `mode_num` | uint32 | 复合代码 0..18 |
| `soc` | float/null | 电池电荷，% |
| `grid`、`batt`、`load` | block | 每块 5 个值 |
| `online` | bool | `lastSeen` 不超过 2 分钟 |

`block`：

```json
{"u":{"v":218,"nd":false},"f":{"v":50,"nd":false},
 "i":{"v":25,"nd":false},"p":{"v":5450,"nd":false},
 "t":{"v":59,"nd":false}}
```

- `v` — 国际单位制单位下的值：V、A、W、Hz、°C、%。
- `nd: true` — 无数据，显示「—」。
- 服务器返回**原始**值，不做舍入；舍入和设色在
  客户端。
- 对 MAP `batt`，**负**电流是充电，**正**电流是放电
  （见上文示例：充电模式，`i = −93.375`）。

### 2.3. KES 卡片（`kes[]`）

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

`panel` — 面板，`batt` — 电池。KES 的 `f` 总是 `0/nd`。

### 2.4. BMS 卡片（`bms[]`）

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

- `pack.i` — 带符号电流：放电 `+`，充电 `−`。
- `cells_mv` — 长度为 `cell_count` 的数组（示例中已缩减）。
- `cell_warn_mask`/`cell_prot_mask` — 逐单体的警告/保护位掩码。

### 2.5. 逆变器卡片（`grid[]`）

```json
{
  "id": 8,
  "gen": 1,
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "车库",
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

`status` — **连接**状态（`OK`/无连接），`inverter_status` — 逆变器工作
状态。

### 2.6. 其它列表

| 端点 | 响应 |
|---|---|
| `GET /api/map/devices` | `[{"type":"map","id","gen","serial","label"}]` |
| `GET /api/kes/devices` | `[{"type":"kes","id","gen","serial","label"}]` |
| `GET /api/bms/devices` | `[{"type":"bms","id","gen","serial","base","bms_type","bms_type_label","source","framing","last_seen"}]` |
| `GET /api/grid/status` | `{"updated":...,"inverters":[{"id","gen","serial","addr","kind","status","last_seen","name","placement","logger_sn"}]}` |
| `GET /api/settings/status?type=map\|kes\|bms` | `{"type","updated","rows":[...]}` |
| `GET /api/stats/devices` | `{"devices":[{"type","id","last"}]}` |

`logger_sn` 是配置的记录器序列号（uint32，十进制），不是存储键；0 — 未设置（无配置或 shm 仍为旧配置）。

`/api/map/devices` 示例：

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

`/api/bms/devices` 示例：

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

`/api/settings/status`：

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

`state=1` — 设备在不超过 60 秒前有过连接。

## 3. 原始快照（raw）

`raw` — 上次成功轮询的二进制快照。客户端必须根据列表校验
`id`+`gen`；否则可能得到别人的/过期的槽。

### 3.1. `GET /api/map/raw?id=&gen=`

```http
GET /api/map/raw?id=0&gen=1792353796794
Content-Type: application/octet-stream
```

Payload 大小 — `64 + ceil(402/8) + 402×4 = 1723` 字节（当 `nfields=402`）。
下面是片段（前 80 字节）。

```text
00000000: 4d50 4144 0500 9201 2f64 6576 2f74 7479  MPAD..../dev/tty
00000010: 5553 4232 0000 0000 0000 0000 0000 0000  USB2............
00000020: 0000 0000 0000 0000 4400 0000 0000 0000  ........D.......
00000030: ba82 9c50 a101 0000 c82b d56a 0000 0000  ...P.....+.j....
00000040: ffff ffff ffff ffff ffff ffff ffff ffff  ................
```

Payload 头部：

| 偏移 | 大小 | 字段 | 值 |
|---|---|---|---|
| 0 | 4 | `magic` | `"MPAD"`（`0x4441504D` LE） |
| 4 | 2 | `version` | `5`（当前） |
| 6 | 2 | `nfields` | `402` |
| 8 | 32 | `port` | NUL 终止的字符串 |
| 40 | 1 | `model` | `P`/`H`/`D`/`T` |
| 41 | 1 | `protocol` | `0` MicroArt，`1` Modbus |
| 42 | 1 | `unit` | Modbus 地址 |
| 43 | 1 | `reserved` | 0 |
| 44 | 4 | `reserved` | 0 |
| 48 | 8 | `gen` | 槽代数 |
| 56 | 8 | `lastSeen` | unix 时间 |
| 64 | `ceil(n/8)` | `presence` | 字段存在位图 |
| `64+presence` | `n×4` | `values[]` | 每个字段 `uint32` LE |

- `values[i]` — 字段 `id=i` 的**原始**值。缩放比例在客户端。
- `presence` — 若字段实际读取到则位 `i` = 1；对于缺失的
  值通常 `values` 中为 `0xFFFFFFFF`，但仅凭这一点不可靠。
- 402 个字段的完整列表（`id`、名称、地址、大小）— `docs/map-contract.md`。
- 关键公式 — §3.1.1。

#### 3.1.1. 关键字段的缩放

| 契约字段 | id | 公式 |
|---|---|---|
| `UACC` | 4 | `(H×256+L)/10` V |
| `MODE` | 36 | 复合模式（见 legacy §3.3） |
| `UNET` | 147 | `0`/`V+100` V |
| `INET` | 148 | A，幅值 |
| `TFNET` | 150 | `6250/V` Hz |
| `Temp_Grad0/1/2` | 159/160/161 | `V−50` °C |
| `INET_16_4` | 162 | `raw/(INET≥16 ? 4 : 16)` A；符号：`PNET_Sign_P==0` → 回馈（`−`） |
| `IAcc_med_A_u16_L/H` | 163/164 | `(L+H×256)/16` A；符号取决于 `MODE_RAM` |
| `I_Akb_MAP_faz1..3L/H` | 323..328 | `((L&0xFF)|((H&0x7F)<<8))/10` A；符号 — `H` 的高位 |
| `PNET_8` | 387 | `int16(...) × 10` W（带符号） |

#### 3.1.2. Python 解码器

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

Payload — **1536 字节**：三个 512 字节子块 `[KESE][KESR][KESS]`。
下面是片段（前 32 字节）。

```text
00000000: 4b45 5345 0100 6f02 0501 a832 0401 0100  KESE..o....2....
00000010: 0000 0000 0000 0000 0000 0000 0000 0000  ................
```

`KESE`（EEPROM，护照）：

| off | 字段 | 类型 | 含义 |
|---|---|---|---|
| 0 | `magic` | `"KESE"` | |
| 4 | `version` | u8 | 1 |
| 6 | `Serial` | u16 LE | 序列号 |
| 8 | `Device` | u8 | KES 为 5 |
| 9 | `Pow` | u8 | 功率/类型 |
| 10 | `FW` | u8 | `major=v&0x0F`，`minor=v>>5` |
| 11 | `ModelPow` | u8 | W = 值×100 |
| 12 | `ModelVolt` | u8 | V = 值×12 |
| 13 | `Pic` | u8 | 1 |
| 14 | `Net` | u8 | |

`KESR`（RAM，遥测，`off` 相对块起点）：

| off | 字段 | 类型 | 含义 |
|---|---|---|---|
| 0 | `magic` | `"KESR"` | |
| 4 | `version` | u8 | 1 |
| 6 | `LastSeen` | u64 LE | unix 时间 |
| 14 | `RelayC` | u8 | 继电器位 1/2/3 |
| 15 | `RSErrSis` | u8 | 控制器错误 |
| 16 | `I2CErr` | u8 | I²C 错误 |
| 18 | `VcPV` | u16 LE | 面板电压 ×100 |
| 20 | `IcPV` | u16 LE | 面板电流 ×100 |
| 22 | `VBat` | u16 LE | 电池电压 ×100 |
| 24 | `PPV` | u16 LE | 面板功率，W |
| 26 | `POut` | u16 LE | 输出功率，W |
| 28 | `PLoad` | u16 LE | 负载功率，W |
| 30 | `PCurr` | u16 LE | 充电功率，W |
| 32 | `ICh` | u16 LE | 充电电流 ×100 |
| 34 | `IOut` | u16 LE | 输出电流 ×100 |
| 36 | `TempInt` | u8 | `x−50` °C |
| 37 | `TempBat` | u8 | `x−50` °C |
| 38 | `PwrW` | u16 LE | 当日能量，W |
| 40 | `PwrKW` | u16 LE | 当日能量，kW |
| 42..49 | `IExts0/1`、`PExts0/1` | u16 LE | 外部传感器 |
| 50 | `Mode` | u8 | ASCII `i/v/B/b/S` |
| 51 | `Sign` | u8 | `+`/`-` |
| 52 | `MPP` | u8 | `R`/`L`/`0` |
| 53 | `Windspeed` | u16 LE | `65535` — 无数据 |
| 55 | `UakbCell[32]` | u16×32 | 单体电压 |
| 119 | `TakbCell[32]` | u8×32 | 单体温度（`x−50`） |
| 151 | `QCell[32]` | u16×32 | 单体放电电流 |
| 215 | `ErrCells[32]` | u8×32 | BMS 错误 |
| 247 | `IAkbMPPT[16]` | u16×16 | 并联 MPPT 电流 |
| 279 | `ModeMPPT[16]` | u8×16 | MPPT 模式 |
| 295 | `TakbMPPT[16]` | u8×16 | MPPT 温度 |
| 311 | `ErrMPPT[16]` | u8×16 | MPPT 错误 |

`KESS`（设置，40×u16 LE，`off = 5 + 2×i`，RFC 状态）— 完整列表在
`docs/kes-contract.md` §`KESS`。

当日总能量：`PwrKW×1000 + PwrW`。

#### 3.2.1. Python 解码器

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

Payload 正好 **107 字节**，多字节字段为 **big-endian**。
下面是片段（前 80 字节；其余 27 是 `alarm`）。

```text
00000000: 1006 0003 0cba 0cbb 0cb4 0cb8 0cb7 0cbc  ................
00000010: 0cb9 0cbc 0cba 0cbb 0cb9 0cb9 0cb9 0cba  ................
00000020: 0cbb 0cb7 010e 010e 0104 00fa 0104 00fa  ................
00000030: 0000 0000 0000 01d6 0000 00db 00fa 0000  ................
00000040: 0002 7100 0000 a3c0 0000 0000 0000 145b  ..q............[
```

| off | 大小 | 字段 | 类型 | 单位 |
|---|---|---|---|---|
| 0 | 1 | `cell_count` | u8 | — |
| 1 | 1 | `temp_count` | u8 | — |
| 2 | 2 | `flags` | u16 | b0 充电 MOS，b1 放电 MOS，b2 均衡 |
| 4 | 32 | `cells_mv[16]` | u16×16 | mV |
| 36 | 16 | `temps_dc[8]` | i16×8 | 0.1 °C |
| 52 | 4 | `current_da` | i32 | 0.01 A，放电 `+` |
| 56 | 4 | `power_w` | i32 | W |
| 60 | 2 | `soc_dp` | u16 | 0.1 % |
| 62 | 2 | `soh_dp` | u16 | 0.1 % |
| 64 | 4 | `cap_mah` | u32 | 0.001 A·h |
| 68 | 4 | `remaining_mah` | u32 | 0.001 A·h |
| 72 | 4 | `rated_mah` | u32 | 0.001 A·h |
| 76 | 2 | `cycles` | u16 | — |
| 78 | 2 | `total_cv` | u16 | 0.01 V |
| 80 | 2 | `port_cv` | u16 | 0.01 V |
| 82 | 4 | `balance_mask` | u32 | 掩码 |
| 86 | 2 | `mode` | u16 | 低字节 — enum |
| 88 | 19 | `alarm` | u8×19 | 位字段，按类型布局 |

规范化的 `mode`：`0` 等待，`1` 放电，`2` 充电，`3` 保持，
`4` 充满，`5` 关机。

`alarm` — 152 位；布局取决于 BMS 类型（`docs/bms/<TYPE>.md`）。
`cell_count`/`temp_count` 设定有效元素的数量；数组尾部被
忽略。派生值（min/max/avg）由客户端自行计算。

#### 3.3.1. Python 解码器

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

逆变器的 JSON 文档：字段已缩放，并以 i18n 键标注。

```json
{
  "serial": "0000000000",
  "name": "Deye Right",
  "placement": "车库",
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

- `fields[].id` — 1..24：PV1/PV2（U/I/P）、AC 有功/无功、频率、
  L1/L2/L3（U/I）、温度 1/2、状态、报警字 0..5。
- 值 — 字符串，已带缩放；`nd` — 无数据。
- `alarm_words` — hex 字，`alarms` — 解码后的代码。

## 4. 写入设备

使用同一令牌/cookie 即可写入。下面仅列出客户端需要的契约；
内部细节 — 在 `docs/map-write-api.md` 和 `docs/kes-reader.md`。

### 4.1. `POST /api/map/write`

授权：cookie 或 Bearer。正文 — 至多 64 KiB。

```json
{
  "id": 0,
  "gen": 1792353796794,
  "ops": [
    {"op": "cmd", "code": "07"}
  ]
}
```

- `ops` — **仅一种类型**：要么 `reg`，要么 `cmd`。
- `reg`：`{"op":"reg","addr":"1B6","data":"000082"}` — `addr`/`data` 为 hex，
  不带 `0x`；`addr ≤ 3FF` — EEPROM，`≥400` — RAM；至多 256 字节（Modbus — 至多 250）。
- `cmd`：`{"op":"cmd","code":"07"}` — 恰好一条命令。代码：`1` OFF、`2` ON、
  `3` EEPROM WR（服务）、`4` ChargeOFF、`5` ChargeON、`6` Reset、`7`
  Call_load_EEProm（安全校验）、`8` StatReset、`9` DischOff。
- 最多 64 个操作。
- `no_frame: true` — 不用服务命令做帧封装（客户端通常不需要）。

响应：

```json
{"ok": true}
```

```json
{"ok": false, "code": "no_device", "error": "..."}
```

HTTP 状态码：`200` ok、`400` bad_request、`401` unauthorized、`404` no_device、
`422` map_error、`502` retry_exhausted、`500` internal。

### 4.2. `POST /api/kes/write`

正文 — 与 `kes-reader` 的 control 套接字相同（`docs/kes-reader.md` §4.2.3），web
添加 `cmd:"write"` 并做代理。

```json
{
  "id": 0,
  "gen": 1792312122696,
  "ops": [
    {"op": "reg", "addr": "E3D", "data": "01"}
  ]
}
```

- `ops` — 只有 `op=reg`；没有 `relay` 操作。
- `addr`/`data` — 不带 `0x` 的 hex；`addr` 0x0000..0xFFFF；`data` — 1..256 字节。
- 最多 64 个操作。
- KES 继电器 — 单元 `0x0E3D` 中的字节掩码（位 0/1/2）；客户端自行做 RMW。
- 响应 — `kes-reader` JSON；HTTP 状态码按代码：`200` ok、`400` bad_request、
  `401`、`404` no_device、`422` kes_error、`502` retry_exhausted、`500`。

## 5. 统计

所有端点需授权。时间 — **unix 毫秒**。

| 端点 | 参数 | 响应 |
|---|---|---|
| `GET /api/stats/devices` | — | `{"devices":[{"type","id","last"}]}` |
| `GET /api/stats/extremes` | `type,id` | `{"raw":{"found","min","max"},"avg":{...}}` |
| `GET /api/stats/series` | `type,id,from,to,fields,source` | `{"from","to","series":{"FIELD":[[t,v],...]}}` |
| `GET /api/stats/series?follow=1` | `type,id,depth=1..10,after` | 同上，「最近 N 分钟」窗口 |
| `GET /api/stats/table` | `type,id,from,to,dir,limit,detail,rawmin` | `{"status","columns","rows"}` |

- `type`：`map`、`kes`、`bms`、`grid`、`mbms`（在统计列表中）。
- `id` — 设备字符串标识符（序列号/逻辑 ID）。
- `source`：`raw` 或 `avg`（用于 `series`）。
- `follow=1` — 「在线」模式，来源总是 `raw`，`depth` — 分钟（1..10，
  默认 5）；`after` — 只加载新增的数据点。
- `table`：`dir=from|to`，`limit` 1..2000（默认 200），`detail=1` —
  包含详细行，`rawmin` — 最小的 raw 时间。

`series` 示例：

```json
{
  "from": 1792353000000,
  "to": 1792356600000,
  "series": {
    "1": [[1792353001000, 218.0], [1792353002000, 218.0]]
  }
}
```

`series` 的键 — 遥测契约的**数字 `field_id`**，以字符串形式表示
（`"1"` = `UNET`，`"2"` = `INET`），而不是 `map-contract` 单元的地址/ID，也不是
字段名。名称和单位在表的 `columns` 中。各类型的完整 `field_id` 列表 —
`docs/stat.md`。`fields` 参数接受以逗号分隔的相同数字 id；
空的 `fields` — 所有字段。

`table` 示例：

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

- `columns[]` — `statsCol` 对象；`id` — `field_id`（遥测契约的顺序
  id），`type` — 值的类型代码，`name` — 字段名。
- `rows[]` — `statsRow` 对象：`t` — ms，`v` — 契约版本，`f` — 按
  `field_id` 的值（字符串键）。

## 6. 错误

| 代码 | 正文 | 何时 |
|---|---|---|
| `400` | `{"error":"bad_request"}` | JSON/参数错误 |
| `401` | `{"error":"unauthorized"}` | 缺失/过期令牌 |
| `404` | `{"error":"..."}` / 空 | 无设备/槽，`id+gen` 不匹配 |
| `405` | — | 方法错误 |
| `422` | `{"ok":false,"code":"..."}` | 设备错误 |
| `502` | `{"ok":false,"code":"retry_exhausted"}` | 设备未响应 |
| `500` | `{"ok":false,"code":"internal"}` | 内部错误 |

## 7. 推荐的客户端循环

1. `POST /api/token` → 令牌。
2. `GET /api/<type>/devices`（或 `/api/mainlist`）→ `id/gen` 列表。
3. `GET /api/<type>/raw?id&gen` → 二进制快照。对于 `grid` 没有 raw
   端点：快照通过 `GET /api/grid/device?serial=` 获取。
4. 按契约解码；若 `404` — 重新读取列表。
5. 历史 — `/api/stats/devices` 与 `/api/stats/series|table`。
6. 写入 — `POST /api/map/write` 或 `/api/kes/write`，使用相同的 `id/gen`。

## 8. 参考

- [api-legacy.md](api-legacy.md) — legacy 兼容。
- `docs/api-auth.md` — 授权与令牌存储。
- `docs/map-contract.md` — 完整 MAP 契约（402 个字段）。
- `docs/kes-contract.md` — KES 契约（KESE/KESR/KESS）。
- `docs/bms-reader.md` — BMS 契约（107 字节）。
- `docs/grid-reader.md` — 逆变器。
- `docs/map-write-api.md` — 写入 MAP。
- `docs/kes-reader.md` — 写入 KES。
