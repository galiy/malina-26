# API: legacy-сумяшчальнасць (поўны API)

Дакумент для распрацоўнікаў кліентаў. Апісвае ўвесь стары HTTP-API «Маліны»
(чытанне і запіс), які падтрымліваецца `malina-web` для сумяшчальнасці з
«Майстар-Вузлом» і знешнімі спажыўцамі. Новы API — [api-native.md](api-native.md).

- База: `http://<адрас-прылады>` (TLS — знешні слой, тут не разглядаецца).
- Фармат адказаў legacy — **`text/html; charset=UTF-8`**, але цела ўтрымлівае JSON або
  тэкст (`Ok`/`error`). Гэта гістарычнае паводзіны, не памылка.
- Legacy-чытанне — **без аўтарызацыі**; legacy-запіс — **HTTP Basic + PAM**.

## 1. Матрыца аўтарызацыі

| Эндпоінт | Метад | Аўтарызацыя |
|---|---|---|
| `/read_json.php?device=map` | GET | не |
| `/read_json.php?device=mppt` | GET | не |
| `/read_json.php?device=bat` | GET | не |
| `/read_memory.php` | GET | не |
| `/check_daemons.php` | GET | не |
| `/read_bms.php` | GET | не |
| `/read_sec.php` | GET | не |
| `/write_eeprom.php` | POST | **Basic** (`malina` + пароль → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + пароль → PAM) |

- Карыстальнік Basic — фіксаваны: **`malina`**. Пароль — той самы, што для вэб і
  SSH. Праверка ідзе праз PAM; паспяховыя праверкі кэшыруюцца на 5 хвілін.
- Няправільны/адсутны Basic → `401`.
- Bearer-токен для legacy-запісу **не прымаецца** (толькі Basic).
- Rate-limit і lockout адсутнічаюць.

```bash
# чытанне — без аўтарызацыі
curl -s 'http://10.10.100.254/read_json.php?device=map'

# запіс — Basic
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. Прывязка да МАП

Legacy-модуль замацоўваецца за **адным МАП** па лікавым серыйніку (літара серыйніка
не ўлічваецца). `read_json.php`, `read_memory.php` і `write_eeprom.php` працуюць
менавіта з ім. Пры страце МАП модуль чакае 60 секунд, затым падхоплівае першы
даступны. Калі прывязкі няма, запіс вяртае `error`.

## 3. `GET /read_json.php?device=map`

Здымак палёў МАП у фармаце старога `mapd`. Абнаўляецца прыкладна раз у секунду.

- `device=map` — адзінае падтрыманае значэнне для МАП.
- Іншыя значэнні → `400 {"error":"unsupported device"}`.
- Усе значэнні — **радкі** (гістарычны фармат).
- `timestamp` — unix-час здымка, `time` — `ЧЧ:ММ:СС`.

### 3.1. Прыклад

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

Наша рэалізацыя аддае **фіксаваны** набор ключоў; адсутныя ў прыкладзе
ключы пералічаны ў §3.4.

### 3.2. Ключавыя палі і формулы

| Ключ | Сэнс | Формула/заўвага |
|---|---|---|
| `_UID` | серыйнік МАП | `SerialNum0 + SerialNum1×256` |
| `_MODE` | кампазітны рэжым | 0..18, гл. §3.3 |
| `_Status_Char` | біты зараду | 1 — малы ток, 2 — патрэбен зарад, 4 — 2-я ступень, 8 — дазарад |
| `_Uacc` | напруга АКБ, В | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | устаўкі зараду, В | `(V << UACC)/10` |
| `_Iacc` | ток АКБ, А | `IAcc_med_A_2 × 2`, беззнакавая магнітуда |
| `_IAcc_med_A_u16` | ток АКБ са знакам, А | `+` зарад, `−` разрад |
| `_PLoad` | магутнасць нагрузкі, Вт | `PLoad_L × 100`, беззнакавая магнітуда |
| `_PLoad_calc` | магутнасць АКБ, Вт | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | напруга сеткі, В | `0` — сеткі няма, інакш `V+100` |
| `_INET` | ток сеткі, А | магнітуда |
| `_INET_16_4` | ток сеткі са знакам, А | знак па `PNET_Sign_P`: спажыванне `+`, аддача `−` |
| `_PNET` | магутнасць сеткі, Вт | `int16(PNET_8) × 10` (знакавы) |
| `_PNET_calc` | магутнасць сеткі, Вт | `UNET × |INET_16_4|`, знак па `PNET_Sign_P` |
| `_TFNET` | частата сеткі, Гц | `6250/V` |
| `_UOUTmed` | напруга нагрузкі, В | `0` — не, інакш `V+100` |
| `_Temp_Grad0/1/2` | тэмпературы, °C | `V−50` |
| `_I2C_Err` | памылка I²C | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **сумарны** ток КЭС, А | гістарычная назва; у арыгінале было сярэдняе |
| `_P_mppt_avg` | магутнасць КЭС, Вт | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | фазныя токі, А | `((L+(H&0x7F)×256)/10)`, знак — біт `0x80` |
| `_I_acc_3ph` | сума фазных токаў, А | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | рэле | біты 0/1 `Status_RELEdop` |
| `fw` | версія ПЗ | `(V&0x1F).(V>>5)` |

**Важна (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — цяпер **сума** па КЭС, а не
сярэдняе. Спажыўцам, якія чакалі сярэдняе, трэба дзяліць на лік актыўных КЭС.

**Важна (2026-10-18):** `_INET_16_4` аддаецца са знакам: спажыванне `+`, аддача `−`.
Для магнітуды бярыце `abs(_INET_16_4)`.

### 3.3. Рэжымы `_MODE`

| Код | Тэкст |
|---|---|
| 0 | МАП выключаны і няма сеткі на ўваходзе |
| 1 | МАП выключаны. Сетка на ўваходзе |
| 2 | МАП уключаны. Генерацыя ад АКБ. Няма сеткі |
| 3 | МАП уключаны і транслюе сетку |
| 4 | Трансляцыя + зарад |
| 5 | Буферны зарад |
| 6 | Зарад, 1-я ступень |
| 7 | Зарад, 2-я ступень |
| 8 | Дазарад, 3-я ступень |
| 9 | Іншыя зарадныя рэжымы (`StatusCh` dec/bin) |
| 10 | Прымусовая генерацыя |
| 11 | Тарыфная сетка, максімальны тарыф, прымусовая генерацыя |
| 12 | Тарыфная сетка, мінімальны тарыф |
| 13 | Трансляцыя + эка-падпампоўка |
| 14 | Трансляцыя + продаж у сетку |
| 15 | Чаканне знешняга поўнага зараду |
| 16 | Тарыфная сетка, трансляцыя + эка-падпампоўка |
| 17 | Тарыфная сетка, трансляцыя + продаж у сетку |
| 18 | Падпампоўка Pmax |

### 3.4. Іншыя ключы

Дадаткова заўсёды аддаюцца: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` і іх `_B`-версіі,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — бітфілды перагрузак; `_UNET_Limit`, `_TFNET_Limit` —
устаўкі; `_RSErr*`, `_RSWarning` — памылкі/папярэджанні; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — службовыя; `_CoolerSpeed`, `Temp_off` —
службовыя.

## 4. `GET /read_json.php?device=mppt`

Здымак усіх знойдзеных КЭС. Адказ — JSON-масіў аб'ектаў (па адным на КЭС).
Усе значэнні — **радкі**. Пустыя слоты (даўжыня ≤ 10) адкідаюцца.

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

| Поле | Сэнс |
|---|---|
| `UID` | серыйнік КЭС |
| `Vc_PV`, `Ic_PV` | напруга/ток панэляў |
| `V_Bat` | напруга АКБ |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | магутнасці панэляў/выхаду/нагрузкі/зараду, Вт |
| `I_Ch`, `I_Out` | токі зараду/выхаду |
| `Temp_Int`, `Temp_Bat` | тэмпературы (`x−50`) |
| `Pwr_kW` | энергія за суткі, кВт·г (дробавае) |
| `I_EXTS0/1`, `P_EXTS0/1` | знешнія датчыкі току/магутнасці |
| `Relay_C` | біты рэле (0/1/2) |
| `RSErrSis` | памылкі кантролера |
| `Sign_C0`, `Sign_C1` | знакі датчыкаў; заўсёды `0`, у кантракт КЭС не публікуюцца |
| `Mode` | рэжым зараду (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | знак току (`+`/`-`) |
| `MPP` | кропка МРР (`R`/`L`/`0`) |
| `windspeed` | частата ВГ; `65535` — няма даных |
| `FW` | версія ПЗ |
| `R1..R3` | стан рэле |

Пры `device` па-за `{map, mppt, bat}` — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

Эмуляцыя `batmon` для «Майстар-Вузла». Адказ — масіў з **двух** аб'ектаў.
Усе значэнні — радкі.

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

- `C_100_remain` — ацэнка зараду, % (апаратны `SoC`, інакш OCV).
- `mppt_day_E` — сумарная выпрацоўка КЭС за суткі, Вт·г
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — ацэнка зараду, %.
- Астатнія назапашвальныя палі — нулі: адпаведных лічыльнікаў у нас няма.

## 6. `GET /read_memory.php?offset=&count=`

Байты вобраза памяці замацаванага МАП.

- `offset` ≥ 0, `count` ≥ 0.
- Межы абразаюцца па памеры вобраза (`0x000..0x7FF`).
- Адказ — JSON-масіў байтаў `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Статусы дэманаў у фармаце старога `check_daemons.php`.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Ключ | Значэнне |
|---|---|
| `mapd` | `0` — МАП даступны (знойдзены або ёсць здымак), `1` — не |
| `mpptd0..3` | `0` — КЭС прысутнічае, `1` — не (ліміт 4 слоты) |
| `batmond` | `0` — ёсць КЭС (эмуляцыя batmon), `1` — не |
| `bms` | `0` — у здымку МАП ёсць BMS-банкі, `1` — не |
| `"0".."7"` | статусныя байты shm 5000: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | у нас адсутнічаюць → `1` |

## 8. `GET /read_bms.php`

Stateless-здымак ANT BMS, сабраны з бягучага здымка МАП (першы даступны).
Фармат дэмана `bmslistener`.

```json
{
  "updated": 1792355272,
  "devices": [
    {
      "deviceName": "AntBms 500 A/h (Дамінатар)",
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

- `devices` — не больш за адзін элемент.
- Няма МАП/здымка → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — масівы зменнай даўжыні (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — заўсёды `0` (у здымку МАП няма
  гэтых прыкмет).

## 9. `GET /read_sec.php?id=&offset=&count=`

Байты віртуальнай адраснай прасторы КЭС.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` неабавязковы (па змаўчанні 1), `offset+count ≤ 0xE80`.
- Пры `offset=0` — JSON-масіў байтаў; пры `offset>0` — JSON-аб'ект
  `{"<dec-адрас>":<byte>, ...}`.
- Памылка (няма `id`/`offset`, выхад за дыяпазон) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

Віртуальная прастора збіраецца са здымкаў `KESE+KESR+KESS`: масіў нулёў
памерам `0xE80`, у які значэнні раскладваюцца па зыходных RAM-адрасах.

## 10. `POST /write_eeprom.php`

Запіс у замацаваны МАП. Аўтарызацыя — **Basic**.

Цела: `application/x-www-form-urlencoded`, поле `data`:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **дзесятковыя**.
- `addr == 0` → каманда МАП, `val` — код каманды (1..9).
- `addr != 0` → запіс байта: `addr` 0..0x7FF, `val` 0..255.
- Парадак пар захоўваецца; запар ідучыя рэгістравыя запісы аб'ядноўваюцца ў адзін
  запыт, кожная каманда ідзе асобна.
- Ліміт адной заяўкі да `map-reader` — **≤64 аперацыі**; доўгія серыі рэжуцца
  аўтаматычна.
- Таймаўт адказу кліенту — **10 с**. Пры працяглым запісе кліент можа атрымаць
  `error`, а аперацыя завершыцца пазней.

Адказ: `text/html; charset=UTF-8`, тэкст `Ok` або `error`.

```bash
# уключыць МАП (каманда ON=2)
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# запісаць гадзіннік 09:54 (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Коды каманд і рэгістры — `docs/map-write.md`. Пасля запісу модуль перачытвае МАП
і перазбірае здымкі `read_json.php`/`read_memory.php`.

## 11. `GET|POST /write_sec.php`

Запіс у КЭС. Аўтарызацыя — **Basic**.

### 11.1. Рэле

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- Бягучы стан чытаецца з `Relay_C` (0x3E3); калі рэле ўжо ў патрэбным
  стане — ранні `Ok` без запісу.
- Інакш RMW бітаў і запіс байта стану ў `0x0E3D` (3645).

### 11.2. Адвольныя ячэйкі

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — дзесятковыя.
- Калі перададзены `relay`, `data` ігнаруецца.

Адказ: `Ok`, `error` або `unread`. `unread` (чарга 2200) у нашым API замяняецца
на `error`.

```bash
curl -s -u 'malina:ПАРОЛЬ' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. Што са старога API не падтрымліваецца

Перанесены толькі ручкі вышэй. Астатнія legacy-эндпоінты не рэалізаваны; частка
адносіцца да падсістэм, якіх у нас няма (MPPT-гісторыя, batmon, Emoncms, воблака,
USB-бэкап, ападатар).

| Група | Не падтрымліваецца |
|---|---|
| Сістэма/сетка | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Абнаўленне/бэкап | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| Гісторыя/энергія | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Меню/настройкі | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Воблака | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Службовыя | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Актуальныя альтэрнатывы: стан сістэмы — `/api/system`, журналы — `/api/log`,
службы — `/api/services`, сетка — вэб-старонка `/network`, гісторыя — `/api/stats/*`
(гл. [api-native.md](api-native.md)).

## 13. Памылкі і коды

| Сітуацыя | Адказ |
|---|---|
| Няправільны `device` у `read_json.php` | `400 {"error":"unsupported device"}` |
| Выхад за дыяпазон `read_memory`/`read_sec` | `error` або абрэзаны масіў |
| Няма/няправільны Basic у запісе | `401` (`WWW-Authenticate: Basic`) |
| Дрэнны `data`, няма прывязкі да МАП, адмова/таймаўт | тэкст `error` |
| Метад не GET/POST | `405` |

## 14. Міні-дэкодэр на Python

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. Спасылкі

- [api-native.md](api-native.md) — новы API (lists/raws, токены).
- `docs/api-auth.md` — матрыца аўтарызацыі.
- `docs/legacy/legacy_read_json_map.md` — поўны мэпінг `read_json.php?device=map`.
- `docs/legacy/kes-legacy-malina2.md` — разбор legacy-ручак КЭС.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — кантракты `bat`/`read_bms`.
- `docs/map-write.md`, `docs/map-write-api.md` — запіс у МАП.
- `docs/legacy/legacy-endpoints.md` — інвентарызацыя неперанесеных ручак.