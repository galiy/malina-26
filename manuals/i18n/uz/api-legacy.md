# API: legacy-мослик (тўлиқ API)

Мижозларни ишлаб чиқувчилар учун ҳужжат. «Малина»нинг бутун эски HTTP-APIсини
(ўқиш ва ёзиш) тавсифлайди; у «Мастер-Узлом» ва ташқи истеъмолчилар билан
мослик учун `malina-web` томонидан қўллаб-қувватланади. Янги API —
[api-native.md](api-native.md).

- База: `http://<қурилма-манзили>` (TLS — ташқи қатлам, бу ерда кўрилмайди).
- Legacy жавобларининг формати — **`text/html; charset=UTF-8`**, аммо танада JSON ёки
  матн (`Ok`/`error`) бўлади. Бу тарихий ҳатти-ҳаракат, хато эмас.
- Legacy-ўқиш — **авторизациясиз**; legacy-ёзиш — **HTTP Basic + PAM**.

## 1. Авторизация матрицаси

| Эндпоинт | Метод | Авторизация |
|---|---|---|
| `/read_json.php?device=map` | GET | йўқ |
| `/read_json.php?device=mppt` | GET | йўқ |
| `/read_json.php?device=bat` | GET | йўқ |
| `/read_memory.php` | GET | йўқ |
| `/check_daemons.php` | GET | йўқ |
| `/read_bms.php` | GET | йўқ |
| `/read_sec.php` | GET | йўқ |
| `/write_eeprom.php` | POST | **Basic** (`malina` + парол → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + парол → PAM) |

- Basic фойдаланувчиси — қатъий: **`malina`**. Парол — веб ва SSH учун бир хил.
  Текширув PAM орқали ўтади; муваффақиятли текширувлар 5 дақиқага кэшланади.
- Нотўғри/мавжуд бўлмаган Basic → `401`.
- Legacy-ёзиш учун Bearer-токен **қабул қилинмайди** (фақат Basic).
- Rate-limit ва lockout йўқ.

```bash
# ўқиш — авторизациясиз
curl -s 'http://10.10.100.254/read_json.php?device=map'

# ёзиш — Basic
curl -s -u 'malina:ПАРОЛ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. МАПга боғланиш

Legacy-модуль **бир МАПга** рақамли серий рақами бўйича бириктирилади (серий
рақамнинг ҳарфи ҳисобга олинмайди). `read_json.php`, `read_memory.php` ва
`write_eeprom.php` айнан у билан ишлайди. МАП йўқолганда модуль 60 сония кутади,
сўнгра биринчи мавжуд бирини эгаллаб олади. Агар боғланиш бўлмаса, ёзиш `error`
қайтаради.

## 3. `GET /read_json.php?device=map`

Эски `mapd` форматидаги МАП майдонларининг снимоки. Тахминан сонияда бир марта
янгиланади.

- `device=map` — МАП учун қўллаб-қувватланадиган ягона қиймат.
- Бошқа қийматлар → `400 {"error":"unsupported device"}`.
- Барча қийматлар — **сатрлар** (тарихий формат).
- `timestamp` — снимокнинг unix-вақти, `time` — `ЧЧ:ММ:СС`.

### 3.1. Мисол

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

Бизнинг амалга ошириш **қатъий** калитлар тўпламини қайтаради; мисолда мавжуд
бўлмаган калитлар §3.4 да келтирилган.

### 3.2. Асосий майдонлар ва формулалар

| Калит | Маъноси | Формула/эслатма |
|---|---|---|
| `_UID` | МАП серий рақами | `SerialNum0 + SerialNum1×256` |
| `_MODE` | композит режим | 0..18, қ. §3.3 |
| `_Status_Char` | заряд битлари | 1 — кичик ток, 2 — заряд керак, 4 — 2-босқич, 8 — тўлдириш заряди |
| `_Uacc` | АКБ кучланиши, В | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | заряд белгиланган қийматлари, В | `(V << UACC)/10` |
| `_Iacc` | АКБ токи, А | `IAcc_med_A_2 × 2`, ишорасиз катталик |
| `_IAcc_med_A_u16` | ишорали АКБ токи, А | `+` заряд, `−` разряд |
| `_PLoad` | юкланиш қуввати, Вт | `PLoad_L × 100`, ишорасиз катталик |
| `_PLoad_calc` | АКБ қуввати, Вт | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | тармоқ кучланиши, В | `0` — тармоқ йўқ, акс ҳолда `V+100` |
| `_INET` | тармоқ токи, А | катталик |
| `_INET_16_4` | ишорали тармоқ токи, А | ишора `PNET_Sign_P` бўйича: истеъмол `+`, қайтиш `−` |
| `_PNET` | тармоқ қуввати, Вт | `int16(PNET_8) × 10` (ишорали) |
| `_PNET_calc` | тармоқ қуввати, Вт | `UNET × |INET_16_4|`, ишора `PNET_Sign_P` бўйича |
| `_TFNET` | тармоқ частотаси, Гц | `6250/V` |
| `_UOUTmed` | юкланиш кучланиши, В | `0` — йўқ, акс ҳолда `V+100` |
| `_Temp_Grad0/1/2` | ҳароратлар, °C | `V−50` |
| `_I2C_Err` | I²C хатоси | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | КЭСнинг **умумий** токи, А | тарихий ном; аслида ўртача эди |
| `_P_mppt_avg` | КЭС қуввати, Вт | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | фаза токлари, А | `((L+(H&0x7F)×256)/10)`, ишора — `0x80` бити |
| `_I_acc_3ph` | фаза токлари йиғиндиси, А | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | реле | `Status_RELEdop` нинг 0/1 битлари |
| `fw` | дастурий таъминот версияси | `(V&0x1F).(V>>5)` |

**Муҳим (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — энди КЭС бўйича **йиғинди**, ўртача
эмас. Ўртачани кутган истеъмолчилар фаол КЭСлар сонига бўлиши керак.

**Муҳим (2026-10-18):** `_INET_16_4` ишора билан қайтарилади: истеъмол `+`, қайтиш `−`.
Магнитуда учун `abs(_INET_16_4)` олинг.

### 3.3. `_MODE` режимлари

| Код | Матн |
|---|---|
| 0 | МАП ўчиқ ва киришда тармоқ йўқ |
| 1 | МАП ўчиқ. Киришда тармоқ бор |
| 2 | МАП ёниқ. АКБдан генерация. Тармоқ йўқ |
| 3 | МАП ёниқ ва тармоқни трансляция қилади |
| 4 | Трансляция + заряд |
| 5 | Буфер заряд |
| 6 | Заряд, 1-босқич |
| 7 | Заряд, 2-босқич |
| 8 | Тўлдириш заряди, 3-босқич |
| 9 | Бошқа заряд режимлари (`StatusCh` dec/bin) |
| 10 | Мажбурий генерация |
| 11 | Тариф тармоғи, максимал тариф, мажбурий генерация |
| 12 | Тариф тармоғи, минимал тариф |
| 13 | Трансляция + эко-қўшимча заряд |
| 14 | Трансляция + тармоқка сотиш |
| 15 | Ташқи тўлиқ зарядни кутиш |
| 16 | Тариф тармоғи, трансляция + эко-қўшимча заряд |
| 17 | Тариф тармоғи, трансляция + тармоқка сотиш |
| 18 | Pmax қўшимча заряд |

### 3.4. Бошқа калитлар

Қўшимча равишда доимо қайтарилади: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` ва уларнинг `_B`-версиялари,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — ортиқча юкланиш битфилдлари; `_UNET_Limit`, `_TFNET_Limit` —
белгиланган қийматлар; `_RSErr*`, `_RSWarning` — хато/оғоҳлантиришлар; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — хизмат; `_CoolerSpeed`, `Temp_off` —
хизмат.

## 4. `GET /read_json.php?device=mppt`

Топилган барча КЭСларнинг снимоки. Жавоб — объектларнинг JSON-массиви (ҳар КЭСга
биттадан). Барча қийматлар — **сатрлар**. Бўш слотлар (узунлик ≤ 10) ташлаб
юборилади.

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

| Майдон | Маъноси |
|---|---|
| `UID` | КЭС серий рақами |
| `Vc_PV`, `Ic_PV` | панеллар кучланиши/токи |
| `V_Bat` | АКБ кучланиши |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | панел/чиқиш/юкланиш/заряд қувватлари, Вт |
| `I_Ch`, `I_Out` | заряд/чиқиш токлари |
| `Temp_Int`, `Temp_Bat` | ҳароратлар (`x−50`) |
| `Pwr_kW` | суткалик энергия, кВт·ч (касрли) |
| `I_EXTS0/1`, `P_EXTS0/1` | ташқи ток/қувват датчиклари |
| `Relay_C` | реле битлари (0/1/2) |
| `RSErrSis` | контроллер хатоси |
| `Sign_C0`, `Sign_C1` | датчик ишоралари; доимо `0`, КЭС контрактига эълон қилинмайди |
| `Mode` | заряд режими (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | ток ишораси (`+`/`-`) |
| `MPP` | МРР нуқтаси (`R`/`L`/`0`) |
| `windspeed` | ВГ частотаси; `65535` — маълумот йўқ |
| `FW` | дастурий таъминот версияси |
| `R1..R3` | реле ҳолати |

`device` `{map, mppt, bat}` дан ташқари бўлса — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

«Мастер-Узлом» учун `batmon` эмуляцияси. Жавоб — **иккита** объектдан иборат массив.
Барча қийматлар — сатрлар.

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

- `C_100_remain` — заряд баҳоси, % (аппарат `SoC`, акс ҳолда OCV).
- `mppt_day_E` — КЭСларнинг суткалик умумий ишлаб чиқариши, Вт·ч
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — заряд баҳоси, %.
- Қолган йиғувчи майдонлар — ноллар: бизда мос ҳисоблагичлар йўқ.

## 6. `GET /read_memory.php?offset=&count=`

Бириктирилган МАПнинг хотира образи байтлари.

- `offset` ≥ 0, `count` ≥ 0.
- Чегаралар образ ўлчами бўйича кесилади (`0x000..0x7FF`).
- Жавоб — байтларнинг JSON-массиви `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Демонлар ҳолатлари эски `check_daemons.php` форматида.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Калит | Қиймат |
|---|---|
| `mapd` | `0` — МАП мавжуд (топилган ёки снимок бор), `1` — йўқ |
| `mpptd0..3` | `0` — КЭС мавжуд, `1` — йўқ (4 та слот чегараси) |
| `batmond` | `0` — КЭС бор (batmon эмуляцияси), `1` — йўқ |
| `bms` | `0` — МАП снимокида BMS-банклар бор, `1` — йўқ |
| `"0".."7"` | shm 5000 ҳолат байтлари: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | бизда йўқ → `1` |

## 8. `GET /read_bms.php`

Жорий МАП снимокидан (биринчи мавжуд) йиғилган ANT BMS нинг stateless снимоки.
`bmslistener` демони формати.

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

- `devices` — биттадан кўп бўлмаган элемент.
- МАП/снимок йўқ → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — ўзгарувчан узунликдаги массивлар (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — доимо `0` (МАП снимокида бу
  белгилар йўқ).

## 9. `GET /read_sec.php?id=&offset=&count=`

КЭС виртуал манзил майдони байтлари.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` ихтиёрий (сукунат бўйича 1), `offset+count ≤ 0xE80`.
- `offset=0` да — байтларнинг JSON-массиви; `offset>0` да — JSON-объект
  `{"<dec-манзил>":<byte>, ...}`.
- Хато (`id`/`offset` йўқ, диапазондан чиқиш) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

Виртуал майдон `KESE+KESR+KESS` снимокларидан йиғилади: ўлчами `0xE80` бўлган
ноллар массиви, унга қийматлар асл RAM-манзиллари бўйича жойланади.

## 10. `POST /write_eeprom.php`

Бириктирилган МАПга ёзиш. Авторизация — **Basic**.

Тана: `application/x-www-form-urlencoded`, `data` майдони:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **ўнлик**.
- `addr == 0` → МАП буйруғи, `val` — буйруқ коди (1..9).
- `addr != 0` → байт ёзиш: `addr` 0..0x7FF, `val` 0..255.
- Жуфтлик тартиби сақланади; ёнма-ён келаётган регистр ёзувлари битта сўровга
  бирлаштирилади, ҳар бир буйруқ алоҳида юради.
- `map-reader`га битта заявка чегараси — **≤64 операция**; узун сериялар автоматик
  кесилади.
- Мижозга жавоб таймаути — **10 с**. Узоқ ёзишда мижоз `error` олиши мумкин, операция
  эса кейинроқ тугалланади.

Жавоб: `text/html; charset=UTF-8`, `Ok` ёки `error` матни.

```bash
# МАПни ёқиш (ON=2 буйруғи)
curl -s -u 'malina:ПАРОЛ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# соат 09:54 ни ёзиш (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:ПАРОЛ' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Буйруқ кодлари ва регистрлар — `docs/map-write.md`. Ёзишдан сўнг модуль МАПни қайта
ўқийди ва `read_json.php`/`read_memory.php` снимокларини қайта йиғади.

## 11. `GET|POST /write_sec.php`

КЭСга ёзиш. Авторизация — **Basic**.

### 11.1. Реле

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- Жорий ҳолат `Relay_C` (0x3E3) дан ўқилади; агар реле аллақачон керакли
  ҳолатда бўлса — ёзишсиз эрта `Ok`.
- Акс ҳолда RMW битлар ва ҳолат байтини `0x0E3D` (3645) га ёзиш.

### 11.2. Ихтиёрий ячейкалар

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — ўнлик.
- Агар `relay` узатилса, `data` эътиборга олинмайди.

Жавоб: `Ok`, `error` ёки `unread`. `unread` (2200 навбати) бизнинг APIда `error`
билан алмаштирилади.

```bash
curl -s -u 'malina:ПАРОЛ' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. Эски APIнинг нимаси қўллаб-қувватланмайди

Фақат юқоридаги ручкалар кўчирилган. Қолган legacy-эндпоинтлар амалга оширилмаган;
бир қисми бизда бўлмаган подтизимларга тааллуқли (MPPT-тарих, batmon, Emoncms, булут,
USB-бэкап, апдейтер).

| Гуруҳ | Қўллаб-қувватланмайди |
|---|---|
| Тизим/тармоқ | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Янгилаш/бэкап | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| Тарих/энергия | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Меню/созламалар | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Булут | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Хизмат | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Долзарб муқобиллар: тизим ҳолати — `/api/system`, журналлар — `/api/log`,
хизматлар — `/api/services`, тармоқ — веб-саҳифа `/network`, тарих — `/api/stats/*`
(қ. [api-native.md](api-native.md)).

## 13. Хато ва кодлар

| Вазият | Жавоб |
|---|---|
| `read_json.php` да нотўғри `device` | `400 {"error":"unsupported device"}` |
| `read_memory`/`read_sec` диапазондан чиқиши | `error` ёки кесилган массив |
| Ёзишда Basic йўқ/нотўғри | `401` (`WWW-Authenticate: Basic`) |
| Ёмон `data`, МАПга боғланиш йўқ, рад/таймаут | `error` матни |
| Метод GET/POST эмас | `405` |

## 14. Python-мини-декодер

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. Ҳавола

- [api-native.md](api-native.md) — янги API (lists/raws, токенлар).
- `docs/api-auth.md` — авторизация матрицаси.
- `docs/legacy/legacy_read_json_map.md` — `read_json.php?device=map` нинг тўлиқ мэппинги.
- `docs/legacy/kes-legacy-malina2.md` — КЭС legacy-ручкалари таҳлили.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — `bat`/`read_bms` контрактлари.
- `docs/map-write.md`, `docs/map-write-api.md` — МАПга ёзиш.
- `docs/legacy/legacy-endpoints.md` — кўчирилмаган ручкалар инвентаризацияси.