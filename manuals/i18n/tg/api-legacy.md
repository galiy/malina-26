# API: мувофиқати legacy (пурраи API)

Ҳуҷҷат барои таҳиягарони клиентҳо. Тамоми HTTP-API-и кӯҳнаи «Малина»-ро
(хониш ва навишт) тавсиф мекунад, ки `malina-web` онро барои мувофиқат бо
«Узли-Мастер» ва истеъмолкунандагони беруна дастгирӣ мекунад. API-и нав —
[api-native.md](api-native.md).

- База: `http://<суроғаи-дастгоҳ>` (TLS — қабати беруна, дар ин ҷо баррасӣ
  намешавад).
- Формати ҷавобҳои legacy — **`text/html; charset=UTF-8`**, вале бадан JSON ё
  матнро (`Ok`/`error`) дар бар мегирад. Ин рафтори таърихӣ аст, на хато.
- Хониши legacy — **бе муҷаввизӣ**; навишти legacy — **Basic-и HTTP + PAM**.

## 1. Матритсаи муҷаввизӣ

| Эндпоинт | Метод | Муҷаввизӣ |
|---|---|---|
| `/read_json.php?device=map` | GET | не |
| `/read_json.php?device=mppt` | GET | не |
| `/read_json.php?device=bat` | GET | не |
| `/read_memory.php` | GET | не |
| `/check_daemons.php` | GET | не |
| `/read_bms.php` | GET | не |
| `/read_sec.php` | GET | не |
| `/write_eeprom.php` | POST | **Basic** (`malina` + парол → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + парол → PAM) |

- Истифодабарандаи Basic — собит: **`malina`**. Парол — ҳамоне, ки барои веб ва
  SSH. Санҷиш тавассути PAM мегузарад; санҷишҳои муваффақ ба муддати 5 дақиқа
  кэш мешаванд.
- Basic-и нодуруст/ғоиб → `401`.
- Bearer-токен барои навишти legacy **қабул намешавад** (танҳо Basic).
- Rate-limit ва lockout вуҷуд надоранд.

```bash
# хониш — бе муҷаввизӣ
curl -s 'http://10.10.100.254/read_json.php?device=map'

# навишт — Basic
curl -s -u 'malina:ПАРОЛ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. Пайванд ба МАП

Модули legacy ба **як МАП** аз рӯи рақами силсилавии ададӣ пайванд мешавад
(ҳарфи силсилабаҳисоб гирифта намешавад). `read_json.php`, `read_memory.php` ва
`write_eeprom.php` маҳз бо он кор мекунанд. Ҳангоми гум шудани МАП модул 60
сония интизор мешавад, сипас аввалин дастрасро мегирад. Агар пайванд набошад,
навишт `error` бармегардонад.

## 3. `GET /read_json.php?device=map`

Суръатгирии майдонҳои МАП дар формати `mapd`-и кӯҳна. Тақрибан як маротиба
дар сония нав мешавад.

- `device=map` — ягона қимати дастгиришаванда барои МАП.
- Қиматҳои дигар → `400 {"error":"unsupported device"}`.
- Ҳамаи қиматҳо — **сатрҳо** (формати таърихӣ).
- `timestamp` — вақти unix-и суръатгирӣ, `time` — вақт дар формати `ЧЧ:ММ:СС`.

### 3.1. Намуна

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

Татбиқи мо **собит** маҷмӯи калидҳоро медиҳад; калидҳои дар намуна ғоиб дар §3.4
номбар шудаанд.

### 3.2. Майдонҳои калидӣ ва формулаҳо

| Калид | Маъно | Формула/эзоҳ |
|---|---|---|
| `_UID` | рақами силсилавии МАП | `SerialNum0 + SerialNum1×256` |
| `_MODE` | режими композитӣ | 0..18, ниг. §3.3 |
| `_Status_Char` | битҳои заряд | 1 — ҷараёни хурд, 2 — заряд лозим, 4 — зинаи 2-юм, 8 — дозаряд |
| `_Uacc` | шиддати АКБ, В | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | танзимоти заряд, В | `(V << UACC)/10` |
| `_Iacc` | ҷараёни АКБ, А | `IAcc_med_A_2 × 2`, бузургии беаломат |
| `_IAcc_med_A_u16` | ҷараёни АКБ бо аломат, А | `+` заряд, `−` ҳолӣкунӣ |
| `_PLoad` | қуввати бор, Вт | `PLoad_L × 100`, бузургии беаломат |
| `_PLoad_calc` | қуввати АКБ, Вт | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | шиддати шабака, В | `0` — шабака нест, вагарна `V+100` |
| `_INET` | ҷараёни шабака, А | бузургӣ |
| `_INET_16_4` | ҷараёни шабака бо аломат, А | аломат аз рӯи `PNET_Sign_P`: истеъмол `+`, додашавӣ `−` |
| `_PNET` | қуввати шабака, Вт | `int16(PNET_8) × 10` (аломатдор) |
| `_PNET_calc` | қуввати шабака, Вт | `UNET × |INET_16_4|`, аломат аз рӯи `PNET_Sign_P` |
| `_TFNET` | басомади шабака, Гц | `6250/V` |
| `_UOUTmed` | шиддати бор, В | `0` — нест, вагарна `V+100` |
| `_Temp_Grad0/1/2` | ҳароратҳо, °C | `V−50` |
| `_I2C_Err` | хатои I²C | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | ҷараёни **умумӣ**-и КЭС, А | номи таърихӣ; дар асл миёна буд |
| `_P_mppt_avg` | қуввати КЭС, Вт | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | ҷараёнҳои фазӣ, А | `((L+(H&0x7F)×256)/10)`, аломат — бити `0x80` |
| `_I_acc_3ph` | ҷамъи ҷараёнҳои фазӣ, А | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | реле | битҳои 0/1-и `Status_RELEdop` |
| `fw` | версияи нармафзор | `(V&0x1F).(V>>5)` |

**Муҳим (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — ҳоло **ҷамъ** барои КЭС, на
миёна. Истеъмолкунандагоне, ки миёнаро интизор буданд, бояд ба шумораи КЭС-ҳои фаъол
тақсим кунанд.

**Муҳим (2026-10-18):** `_INET_16_4` бо аломат дода мешавад: истеъмол `+`, додашавӣ `−`.
Барои бузургӣ `abs(_INET_16_4)`-ро гиред.

### 3.3. Режимҳои `_MODE`

| Код | Матн |
|---|---|
| 0 | МАП хомӯш ва шабака дар вуруд нест |
| 1 | МАП хомӯш. Шабака дар вуруд аст |
| 2 | МАП фурӯзон. Генератсия аз АКБ. Шабака нест |
| 3 | МАП фурӯзон ва шабакаро интиқол медиҳад |
| 4 | Интиқол + заряд |
| 5 | Заряди буферӣ |
| 6 | Заряд, зинаи 1-юм |
| 7 | Заряд, зинаи 2-юм |
| 8 | Дозаряд, зинаи 3-юм |
| 9 | Режимҳои дигари заряд (`StatusCh` dec/bin) |
| 10 | Генератсияи маҷбурӣ |
| 11 | Шабакаи тарифӣ, тарифи максималӣ, генератсияи маҷбурӣ |
| 12 | Шабакаи тарифӣ, тарифи минималӣ |
| 13 | Интиқол + эко-таъминкунӣ (иловакунӣ) |
| 14 | Интиқол + фурӯш ба шабака |
| 15 | Интизори заряди пурраи берунӣ |
| 16 | Шабакаи тарифӣ, интиқол + эко-таъминкунӣ |
| 17 | Шабакаи тарифӣ, интиқол + фурӯш ба шабака |
| 18 | Таъминкунии Pmax |

### 3.4. Калидҳои дигар

Иловатан ҳамеша дода мешаванд: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` ва версияҳои `_B`-и онҳо,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — битмайдҳои зиёдатсалбӣ (аз ҳад зиёд боркунӣ); `_UNET_Limit`, `_TFNET_Limit` —
нишондодҳои заряд; `_RSErr*`, `_RSWarning` — хатоҳо/огоҳиҳо; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — хизматӣ; `_CoolerSpeed`, `Temp_off` —
хизматӣ.

## 4. `GET /read_json.php?device=mppt`

Суръатгирии ҳамаи КЭС-ҳои ёфтшуда. Ҷавоб — масфури JSON-и объектҳо (якто барои
ҳар КЭС). Ҳамаи қиматҳо — **сатрҳо**. Ҷойҳои холӣ (дарозӣ ≤ 10) партофта мешаванд.

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

| Майдон | Маъно |
|---|---|
| `UID` | рақами силсилавии КЭС |
| `Vc_PV`, `Ic_PV` | шиддат/ҷараёни панелҳо |
| `V_Bat` | шиддати АКБ |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | қувватҳои панелҳо/баромад/бор/заряд, Вт |
| `I_Ch`, `I_Out` | ҷараёнҳои заряд/баромад |
| `Temp_Int`, `Temp_Bat` | ҳароратҳо (`x−50`) |
| `Pwr_kW` | энергияи дар шабонарӯз, кВт·соат (касрӣ) |
| `I_EXTS0/1`, `P_EXTS0/1` | сенсорҳои берунаи ҷараён/қувват |
| `Relay_C` | битҳои реле (0/1/2) |
| `RSErrSis` | хатоҳои контроллер |
| `Sign_C0`, `Sign_C1` | аломатҳои сенсорҳо; ҳамеша `0`, ба контракти КЭС нашр намешаванд |
| `Mode` | режими заряд (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | аломати ҷараён (`+`/`-`) |
| `MPP` | нуқтаи МРР (`R`/`L`/`0`) |
| `windspeed` | басомади ВГ; `65535` — маълумот нест |
| `FW` | версияи нармафзор |
| `R1..R3` | ҳолати реле |

Ҳангоми `device` берун аз `{map, mppt, bat}` — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

Эмулятсияи `batmon` барои «Узли-Мастер». Ҷавоб — масфури **ду** объект.
Ҳамаи қиматҳо — сатрҳо.

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

- `C_100_remain` — баҳодиҳии заряд, % (`SoC`-и сахтафзор, вагарна OCV).
- `mppt_day_E` — баромади умумии КЭС дар шабонарӯз, Вт·соат
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — баҳодиҳии заряд, %.
- Майдонҳои боқимондаи ҷамъшаванда — сифрҳо: ҳисоблагирҳои дахлдор назди мо нестанд.

## 6. `GET /read_memory.php?offset=&count=`

Байтҳои образи хотираи МАП-и пайвандшуда.

- `offset` ≥ 0, `count` ≥ 0.
- Сарҳадҳо аз рӯи андозаи образ бурида мешаванд (`0x000..0x7FF`).
- Ҷавоб — масфури JSON-и байтҳо `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Ҳолатҳои демонҳо дар формати `check_daemons.php`-и кӯҳна.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Калид | Қимат |
|---|---|
| `mapd` | `0` — МАП дастрас (ёфта шуд ё суръатгирӣ ҳаст), `1` — не |
| `mpptd0..3` | `0` — КЭС мавҷуд, `1` — не (ҳадди 4 ҷой) |
| `batmond` | `0` — КЭС ҳаст (эмулятсияи batmon), `1` — не |
| `bms` | `0` — дар суръатгирии МАП банкҳои BMS ҳастанд, `1` — не |
| `"0".."7"` | байтҳои ҳолати shm 5000: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | назди мо ғоибанд → `1` |

## 8. `GET /read_bms.php`

Суръатгирии stateless-и ANT BMS, ки аз суръатгирии ҷории МАП (аввалини дастрас)
гирд оварда мешавад. Формати демони `bmslistener`.

```json
{
  "updated": 1792355272,
  "devices": [
    {
      "deviceName": "AntBms 500 A/h (Доминатор)",
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

- `devices` — на бештар аз як унсур.
- МАП/суръатгирӣ нест → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — масфурҳои дарозии тағйирёбанда (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — ҳамеша `0` (дар суръатгирии МАП
  ин нишонаҳо нестанд).

## 9. `GET /read_sec.php?id=&offset=&count=`

Байтҳои фазои суроғаҳои виртуалии КЭС.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` ҳатмӣ нест (бо тариқи пешфарз 1), `offset+count ≤ 0xE80`.
- Ҳангоми `offset=0` — масфури JSON-и байтҳо; ҳангоми `offset>0` — объекти JSON
  `{"<суроғаи-адд>":<byte>, ...}`.
- Хато (нест будани `id`/`offset`, берун аз диапазон) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

Фазои виртуалӣ аз суръатгириҳои `KESE+KESR+KESS` гирд оварда мешавад: масфури
сифрҳо бо андозаи `0xE80`, ки қиматҳо ба он мувофиқи суроғаҳои аслии RAM ҷойгир
мешаванд.

## 10. `POST /write_eeprom.php`

Навишт ба МАП-и пайвандшуда. Муҷаввизӣ — **Basic**.

Бадан: `application/x-www-form-urlencoded`, майдони `data`:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **даҳӣ**.
- `addr == 0` → фармони МАП, `val` — коди фармон (1..9).
- `addr != 0` → навишти байт: `addr` 0..0x7FF, `val` 0..255.
- Тартиби ҷуфтҳо нигоҳ дошта мешавад; навиштҳои регистрӣ, ки паи ҳам меоянд,
  дар як дархост муттаҳид мешаванд, ҳар фармон алоҳида меравад.
- Ҳадди як заявка ба `map-reader` — **≤64 амалиёт**; силсилаҳои дароз худкор бурида
  мешаванд.
- Муҳлати ҷавоб ба клиент — **10 с**. Ҳангоми навишти тӯлонӣ клиент метавонад
  `error` гирад, вале амалиёт дертар анҷом мешавад.

Ҷавоб: `text/html; charset=UTF-8`, матни `Ok` ё `error`.

```bash
# фурӯзон кардани МАП (фармони ON=2)
curl -s -u 'malina:ПАРОЛ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# навишти соат 09:54 (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:ПАРОЛ' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Кодҳои фармонҳо ва регистрҳо — `docs/map-write.md`. Пас аз навишт модул МАП-ро
дубора мехонад ва суръатгириҳои `read_json.php`/`read_memory.php`-ро аз нав месозад.

## 11. `GET|POST /write_sec.php`

Навишт ба КЭС. Муҷаввизӣ — **Basic**.

### 11.1. Реле

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- Ҳолати ҷорӣ аз `Relay_C` (0x3E3) хонда мешавад; агар реле аллакай дар ҳолати
  лозимӣ бошад — `Ok`-и барвақтӣ бе навишт.
- Вагарна RMW-и битҳо ва навишти байти ҳолат ба `0x0E3D` (3645).

### 11.2. Ячейкаҳои ихтиёрӣ

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — даҳӣ.
- Агар `relay` дода шуда бошад, `data` нодида гирифта мешавад.

Ҷавоб: `Ok`, `error` ё `unread`. `unread` (навбати 2200) дар API-и мо бо
`error` иваз мешавад.

```bash
curl -s -u 'malina:ПАРОЛ' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. Чӣ аз API-и кӯҳна дастгирӣ намешавад

Танҳо ручкаҳои дар боло овардашуда кӯчонида шудаанд. Эндпоинтҳои боқимондаи legacy
татбиқ нашудаанд; қисме ба зерсистемаҳое тааллуқ дорад, ки назди мо нестанд
(таърихи MPPT, batmon, Emoncms, абр, USB-нисха, навсозкунанда).

| Гурӯҳ | Дастгирӣ намешавад |
|---|---|
| Система/шабака | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Навсозӣ/нисха | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| Таърих/энергия | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Меню/танзимот | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Абр | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Хизматӣ | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Алтернативаҳои мубрам: ҳолати система — `/api/system`, журналҳо — `/api/log`,
хизматҳо — `/api/services`, шабака — саҳифаи веб `/network`, таърих — `/api/stats/*`
(ниг. [api-native.md](api-native.md)).

## 13. Хатоҳо ва кодҳо

| Ҳолат | Ҷавоб |
|---|---|
| `device`-и нодуруст дар `read_json.php` | `400 {"error":"unsupported device"}` |
| Берун аз диапазон дар `read_memory`/`read_sec` | `error` ё масфури буридашуда |
| Basic ғоиб/нодуруст дар навишт | `401` (`WWW-Authenticate: Basic`) |
| `data`-и бад, пайванд ба МАП нест, рад/муҳлат | матни `error` |
| Метод не GET/POST | `405` |

## 14. Декодери хурд ба Python

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. Пайвандҳо

- [api-native.md](api-native.md) — API-и нав (lists/raws, токенҳо).
- `docs/api-auth.md` — матритсаи муҷаввизӣ.
- `docs/legacy/legacy_read_json_map.md` — харитасозии пурраи `read_json.php?device=map`.
- `docs/legacy/kes-legacy-malina2.md` — таҳлили ручкаҳои legacy-и КЭС.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — контрактҳои `bat`/`read_bms`.
- `docs/map-write.md`, `docs/map-write-api.md` — навишт ба МАП.
- `docs/legacy/legacy-endpoints.md` — инвентаризатсияи ручкаҳои кӯчониданашуда.