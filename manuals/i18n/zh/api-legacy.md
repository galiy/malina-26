# API：legacy 兼容（完整 API）

本文档面向客户端开发者。描述「Malina」全部旧 HTTP API（读和写），
`malina-web` 为了与「主节点」和外部消费方兼容而予以支持。新 API —
[api-native.md](api-native.md)。

- 基础地址：`http://<设备地址>`（TLS 是外层，此处不考虑）。
- legacy 响应格式 — **`text/html; charset=UTF-8`**，但正文包含 JSON 或
  文本（`Ok`/`error`）。这是历史行为，不是错误。
- legacy 读 — **无授权**；legacy 写 — **HTTP Basic + PAM**。

## 1. 授权矩阵

| 端点 | 方法 | 授权 |
|---|---|---|
| `/read_json.php?device=map` | GET | 无 |
| `/read_json.php?device=mppt` | GET | 无 |
| `/read_json.php?device=bat` | GET | 无 |
| `/read_memory.php` | GET | 无 |
| `/check_daemons.php` | GET | 无 |
| `/read_bms.php` | GET | 无 |
| `/read_sec.php` | GET | 无 |
| `/write_eeprom.php` | POST | **Basic**（`malina` + 密码 → PAM） |
| `/write_sec.php` | GET/POST | **Basic**（`malina` + 密码 → PAM） |

- Basic 用户是固定的：**`malina`**。密码与 Web 和 SSH 相同。校验通过 PAM
  进行；成功的校验会缓存 5 分钟。
- 无效/缺失的 Basic → `401`。
- legacy 写**不接受** Bearer 令牌（仅 Basic）。
- 没有速率限制，也没有锁定。

```bash
# 读 — 无授权
curl -s 'http://10.10.100.254/read_json.php?device=map'

# 写 — Basic
curl -s -u 'malina:PASSWORD' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. 与 MAP 的绑定

legacy 模块按数字序列号绑定到**单个 MAP**（序列号字母不参与）。
`read_json.php`、`read_memory.php` 和 `write_eeprom.php` 就与它交互。若 MAP
丢失，模块等待 60 秒，然后接管第一个可用的。若没有绑定，写操作返回 `error`。

## 3. `GET /read_json.php?device=map`

以旧 `mapd` 格式给出的 MAP 字段快照。大约每秒刷新一次。

- `device=map` — MAP 唯一支持的值。
- 其它值 → `400 {"error":"unsupported device"}`。
- 所有值都是**字符串**（历史格式）。
- `timestamp` — 快照的 unix 时间，`time` — `HH:MM:SS`。

### 3.1. 示例

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

我们的实现返回**固定**的键集合；示例中未出现的键列在 §3.4。

### 3.2. 关键字段与公式

| 键 | 含义 | 公式/备注 |
|---|---|---|
| `_UID` | MAP 序列号 | `SerialNum0 + SerialNum1×256` |
| `_MODE` | 复合模式 | 0..18，见 §3.3 |
| `_Status_Char` | 充电位 | 1 — 小电流，2 — 需要充电，4 — 第 2 阶梯，8 — 补充充电 |
| `_Uacc` | 电池电压，V | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | 充电设定值，V | `(V << UACC)/10` |
| `_Iacc` | 电池电流，A | `IAcc_med_A_2 × 2`，无符号幅值 |
| `_IAcc_med_A_u16` | 带符号电池电流，A | `+` 充电，`−` 放电 |
| `_PLoad` | 负载功率，W | `PLoad_L × 100`，无符号幅值 |
| `_PLoad_calc` | 电池功率，W | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | 电网电压，V | `0` — 无电网，否则 `V+100` |
| `_INET` | 电网电流，A | 幅值 |
| `_INET_16_4` | 带符号电网电流，A | 符号取自 `PNET_Sign_P`：消耗 `+`，回馈 `−` |
| `_PNET` | 电网功率，W | `int16(PNET_8) × 10`（带符号） |
| `_PNET_calc` | 电网功率，W | `UNET × |INET_16_4|`，符号取自 `PNET_Sign_P` |
| `_TFNET` | 电网频率，Hz | `6250/V` |
| `_UOUTmed` | 负载电压，V | `0` — 无，否则 `V+100` |
| `_Temp_Grad0/1/2` | 温度，°C | `V−50` |
| `_I2C_Err` | I²C 错误 | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **总** KES 电流，A | 历史名称；原始是平均值 |
| `_P_mppt_avg` | KES 功率，W | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | 相电流，A | `((L+(H&0x7F)×256)/10)`，符号 — 位 `0x80` |
| `_I_acc_3ph` | 各相电流之和，A | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | 继电器 | `Status_RELEdop` 的位 0/1 |
| `fw` | 固件版本 | `(V&0x1F).(V>>5)` |

**重要（2026-10-14）：** `_I_mppt_avg`/`_P_mppt_avg` — 现在是各 KES 的**总和**，
而不是平均值。期望平均值的消费方需要用活跃 KES 的数量来除。

**重要（2026-10-18）：** `_INET_16_4` 带符号返回：消耗 `+`，回馈 `−`。
幅值请用 `abs(_INET_16_4)`。

### 3.3. `_MODE` 模式

| 代码 | 文本 |
|---|---|
| 0 | MAP 关闭且入口无电网 |
| 1 | MAP 关闭。入口有电网 |
| 2 | MAP 开启。由电池发电。无电网 |
| 3 | MAP 开启并转发电网 |
| 4 | 转发 + 充电 |
| 5 | 缓冲充电 |
| 6 | 充电，第 1 阶梯 |
| 7 | 充电，第 2 阶梯 |
| 8 | 补充充电，第 3 阶梯 |
| 9 | 其它充电模式（`StatusCh` dec/bin） |
| 10 | 强制发电 |
| 11 | 计费电网，最高费率，强制发电 |
| 12 | 计费电网，最低费率 |
| 13 | 转发 + 生态泵送 |
| 14 | 转发 + 向电网售电 |
| 15 | 等待外部充满 |
| 16 | 计费电网，转发 + 生态泵送 |
| 17 | 计费电网，转发 + 向电网售电 |
| 18 | Pmax 泵送 |

### 3.4. 其它键

额外总是返回：`_ThFMAP`、`_TFNET_Limit`、`_UNET_Limit`、
`_RSErrDop`、`Temp_off`、`_I_acc_avg`、`_MPPTs_mode`、`_P_acc_3ph`、`_P_ph1..3`、
`_E_NET`、`_E_ACC`、`_E_ACC_CHARGE`、`_E_NET_SIGN` 及其 `_B` 版本、
`_I_MPPT_WIND`、`_P_MPPT_WIND`、`_fl_UAccChBUF_24h`。

`_F_Acc_Over`、`_F_Net_Over` — 过载位字段；`_UNET_Limit`、`_TFNET_Limit` —
设定值；`_RSErr*`、`_RSWarning` — 错误/警告；`_Flag_ECO`、
`_flagUnet2`、`_MPPTs_mode`、`_Inet_flag` — 辅助；`_CoolerSpeed`、`Temp_off` —
辅助。

## 4. `GET /read_json.php?device=mppt`

所有已发现 KES 的快照。响应是对象数组（每个 KES 一个）。所有值都是**字符串**。
空槽（长度 ≤ 10）会被丢弃。

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

| 字段 | 含义 |
|---|---|
| `UID` | KES 序列号 |
| `Vc_PV`、`Ic_PV` | 面板电压/电流 |
| `V_Bat` | 电池电压 |
| `P_PV`、`P_Out`、`P_Load`、`P_curr` | 面板/输出/负载/充电功率，W |
| `I_Ch`、`I_Out` | 充电/输出电流 |
| `Temp_Int`、`Temp_Bat` | 温度（`x−50`） |
| `Pwr_kW` | 当日能量，kW·h（小数值） |
| `I_EXTS0/1`、`P_EXTS0/1` | 外部电流/功率传感器 |
| `Relay_C` | 继电器位（0/1/2） |
| `RSErrSis` | 控制器错误 |
| `Sign_C0`、`Sign_C1` | 传感器符号；总是 `0`，不发布到 KES 契约 |
| `Mode` | 充电模式（`i`/`v`/`B`/`b`/`S`），ASCII |
| `Sign` | 电流符号（`+`/`-`） |
| `MPP` | MPP 点（`R`/`L`/`0`） |
| `windspeed` | 风力发电机频率；`65535` — 无数据 |
| `FW` | 固件版本 |
| `R1..R3` | 继电器状态 |

当 `device` 不在 `{map, mppt, bat}` 中时 — `400 {"error":"unsupported device"}`。

## 5. `GET /read_json.php?device=bat`

模拟 `batmon` 供「主节点」使用。响应是**两个**对象的数组。所有值都是字符串。

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

- `C_100_remain` — 电荷估计，%（硬件 `SoC`，否则用 OCV）。
- `mppt_day_E` — 各 KES 当日总输出，W·h
  （`Σ(Pwr_kW×1000 + Pwr_W)`）。
- `est_soc` — 电荷估计，%。
- 其余累计字段为零：我们没有相应的计数器。

## 6. `GET /read_memory.php?offset=&count=`

已绑定 MAP 的内存映像字节。

- `offset` ≥ 0，`count` ≥ 0。
- 边界按映像大小截断（`0x000..0x7FF`）。
- 响应是字节 JSON 数组 `[0..255]`，`text/html; charset=UTF-8`。

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

以旧 `check_daemons.php` 格式给出的守护进程状态。

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| 键 | 值 |
|---|---|
| `mapd` | `0` — MAP 可用（已找到或有快照），`1` — 无 |
| `mpptd0..3` | `0` — KES 存在，`1` — 无（限制 4 个槽） |
| `batmond` | `0` — 有 KES（batmon 模拟），`1` — 无 |
| `bms` | `0` — MAP 快照中有 BMS 组，`1` — 无 |
| `"0".."7"` | shm 5000 状态字节：0 — mapd，1..4 — mpptd0..3，5 — chargerd，6 — emon，7 — dbe_flash |
| `chargerd`、`dbed`、`emd`、`emon`、`dbe_flash` | 我们这边缺失 → `1` |

## 8. `GET /read_bms.php`

ANT BMS 的无状态快照，由当前 MAP 快照（第一个可用的）组装而来。格式是
`bmslistener` 守护进程。

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

- `devices` — 至多一个元素。
- 无 MAP/快照 → `{"updated":0,"devices":[]}`。
- `cells_v`、`temperatures_c` — 变长数组（`cell_count`）。
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — 总是 `0`（MAP 快照中
  没有这些标志）。

## 9. `GET /read_sec.php?id=&offset=&count=`

KES 虚拟地址空间的字节。

- `id` — **从 1 开始**（`1..N`）。
- `offset` ≥ 0；`count` 可选（默认 1），`offset+count ≤ 0xE80`。
- 当 `offset=0` 时 — 字节 JSON 数组；当 `offset>0` 时 — JSON 对象
  `{"<十进制地址>":<byte>, ...}`。
- 错误（无 `id`/`offset`，越界）→ `error`。

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

虚拟空间由 `KESE+KESR+KESS` 快照组装：一个大小为 `0xE80` 的零数组，
值按其原始 RAM 地址展开进去。

## 10. `POST /write_eeprom.php`

写入已绑定的 MAP。授权 — **Basic**。

正文：`application/x-www-form-urlencoded`，字段 `data`：

```
data=[[<addr>,<val>], ...]
```

- `addr`、`val` — **十进制**。
- `addr == 0` → MAP 命令，`val` — 命令代码（1..9）。
- `addr != 0` → 写字节：`addr` 0..0x7FF，`val` 0..255。
- 成对的顺序保留；连续进行的寄存器写入合并为一个请求，每条命令单独
  发送。
- 对 `map-reader` 的一次请求限制 — **≤64 个操作**；长序列自动切断。
- 给客户端的响应超时 — **10 秒**。长时间写入期间客户端可能收到
  `error`，而操作稍后完成。

响应：`text/html; charset=UTF-8`，文本 `Ok` 或 `error`。

```bash
# 开启 MAP（命令 ON=2）
curl -s -u 'malina:PASSWORD' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# 写入时钟 09:54（0x1B6=438, 0x44B=1099）
curl -s -u 'malina:PASSWORD' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

命令代码和寄存器 — `docs/map-write.md`。写入后模块重新读取 MAP
并重建 `read_json.php`/`read_memory.php` 快照。

## 11. `GET|POST /write_sec.php`

写入 KES。授权 — **Basic**。

### 11.1. 继电器

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 从 1 开始。
- `relay` — 1..3。
- `mode` — `on|off|1|0`。
- 当前状态从 `Relay_C`（0x3E3）读取；如果继电器已处于所需
  状态 — 无需写入的提前 `Ok`。
- 否则 RMW 位并将状态字节写入 `0x0E3D`（3645）。

### 11.2. 任意单元

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`、`val` — 十进制。
- 如果传了 `relay`，`data` 被忽略。

响应：`Ok`、`error` 或 `unread`。在我们的 API 中 `unread`（队列 2200）替换为
`error`。

```bash
curl -s -u 'malina:PASSWORD' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. 旧 API 中不支持的内容

只迁移了上面的处理器。其余 legacy 端点未实现；其中一部分属于我们
没有的子系统（MPPT 历史、batmon、Emoncms、云、USB 备份、升级器）。

| 组 | 不支持 |
|---|---|
| 系统/网络 | `get_cpu_mem.php`、`get_lan_state.php`、`get_wlan_state.php`、`eth_set.php`、`wifi_set.php`、`hostname_set.php`、`time_set.php`、`switch_lang.php`、`reboot_do.php`、`shutdown_do.php`、`service_control.php`、`devices.php`、`find_devices.php`、`errors_counter.php` |
| 更新/备份 | `check_update.php`、`update_main.php`、`update_start.php`、`usb_backup.php`、`get_usbhd_info.php`、`db_backup.php`、`db_restore.php`、`get_db_status.php`、`clear_events.php` |
| 历史/能量 | `get_data.php`、`get_map_e.php`、`get_mppt_e.php`、`map_errors.php`、`map_errors_history.php`、`map_counters_history.php`、`mppt_errors.php`、`mppt_errors_history.php`、`mppt_counters_history.php`、`batmon_history.php` |
| 菜单/设置 | `load_menu.php`、`get_nodes.php`、`master_node_load.php`、`master_node_save.php`、`load_ab_set.php`、`save_ab_set.php`、`wind_mppts_set.php`、`events_get.php`、`events_set.php`、`email_set.php`、`email_test.php` |
| 云 | `cloud_switch.php`、`get_cloud_enable.php`、`get_cloud_status.php`、`set_cloud_settings.php`、`emoncms_switch.php`、`get_emoncms_enable.php`、`get_emoncms_status.php`、`get_emoncms_settings.php`、`set_emoncms_settings.php` |
| 辅助 | `write_shm.php`、`get_eeprom_state.php`、`batmon_100.php`、`batmon_reset.php`、`request_node.php` |

当前替代方案：系统状态 — `/api/system`，日志 — `/api/log`，
服务 — `/api/services`，网络 — 网页 `/network`，历史 — `/api/stats/*`
（见 [api-native.md](api-native.md)）。

## 13. 错误与代码

| 情形 | 响应 |
|---|---|
| `read_json.php` 中 `device` 无效 | `400 {"error":"unsupported device"}` |
| `read_memory`/`read_sec` 越界 | `error` 或截断的数组 |
| 写入中缺失/无效的 Basic | `401`（`WWW-Authenticate: Basic`） |
| 坏的 `data`、无 MAP 绑定、失败/超时 | 文本 `error` |
| 方法不是 GET/POST | `405` |

## 14. 迷你 Python 解码器

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. 参考

- [api-native.md](api-native.md) — 新 API（lists/raws、令牌）。
- `docs/api-auth.md` — 授权矩阵。
- `docs/legacy/legacy_read_json_map.md` — `read_json.php?device=map` 的完整映射。
- `docs/legacy/kes-legacy-malina2.md` — legacy KES 处理器解析。
- `docs/legacy/batmon-api.md`、`docs/legacy/bms-api.md` — `bat`/`read_bms` 契约。
- `docs/map-write.md`、`docs/map-write-api.md` — 写入 MAP。
- `docs/legacy/legacy-endpoints.md` — 未迁移处理器的清单。