# API: legacy-үйлесімділік (толық API)

Клиент әзірлеушілеріне арналған құжат. «Малина» өнімінің барлық ескі HTTP-API
(оқу және жазу) сипаттайды, оны `malina-web` «Шебер-Узел» және сыртқы
тұтынушылармен үйлесімділік үшін қолдайды. Жаңа API — [api-native.md](api-native.md).

- База: `http://<құрылғы-мекенжайы>` (TLS — сыртқы қабат, мұнда қарастырылмайды).
- Legacy жауаптар форматы — **`text/html; charset=UTF-8`**, бірақ дене JSON немесе
  мәтінді (`Ok`/`error`) қамтиды. Бұл тарихи мінез-құлық, қате емес.
- Legacy оқу — **авторизациясыз**; legacy жазу — **HTTP Basic + PAM**.

## 1. Авторизация матрицасы

| Эндпоинт | Әдіс | Авторизация |
|---|---|---|
| `/read_json.php?device=map` | GET | жоқ |
| `/read_json.php?device=mppt` | GET | жоқ |
| `/read_json.php?device=bat` | GET | жоқ |
| `/read_memory.php` | GET | жоқ |
| `/check_daemons.php` | GET | жоқ |
| `/read_bms.php` | GET | жоқ |
| `/read_sec.php` | GET | жоқ |
| `/write_eeprom.php` | POST | **Basic** (`malina` + құпиясөз → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + құпиясөз → PAM) |

- Basic пайдаланушысы — бекітілген: **`malina`**. Құпиясөз — веб және SSH үшін
  бірдей. Тексеру PAM арқылы өтеді; сәтті тексерулер 5 минут кэштеледі.
- Қате/жоқ Basic → `401`.
- Legacy жазу үшін Bearer-токен **қабылданбайды** (тек Basic).
- Rate-limit және lockout жоқ.

```bash
# оқу — авторизациясыз
curl -s 'http://10.10.100.254/read_json.php?device=map'

# жазу — Basic
curl -s -u 'malina:ҚҰПИЯСӨЗ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. МАП-қа байланыстыру

Legacy модулі **бір МАП-қа** сериялық нөмір бойынша бекітіледі (сериялық нөмірдің
әрпі ескерілмейді). `read_json.php`, `read_memory.php` және `write_eeprom.php` дәл
осымен жұмыс істейді. МАП жоғалған кезде модуль 60 секунд күтеді, содан кейін
бірінші қолжетімдісін қабылдайды. Байланыс болмаса, жазу `error` қайтарады.

## 3. `GET /read_json.php?device=map`

Ескі `mapd` форматындағы МАП өрістерінің көрінісі. Шамамен секундына бір рет
жаңартылады.

- `device=map` — МАП үшін жалғыз қолдау көрсетілетін мән.
- Басқа мәндер → `400 {"error":"unsupported device"}`.
- Барлық мәндер — **жолдар** (тарихи формат).
- `timestamp` — көріністің unix-уақыты, `time` — `ЧЧ:ММ:СС`.

### 3.1. Мысал

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

Біздің іске асыру **бекітілген** кілттер жинағын қайтарады; мысалда жоқ кілттер
§3.4-те көрсетілген.

### 3.2. Негізгі өрістер мен формулалар

| Кілт | Мағынасы | Формула/ескерту |
|---|---|---|
| `_UID` | МАП сериялық нөмірі | `SerialNum0 + SerialNum1×256` |
| `_MODE` | композиттік режим | 0..18, §3.3 қараңыз |
| `_Status_Char` | заряд биттері | 1 — аз тоқ, 2 — заряд керек, 4 — 2-саты, 8 — қосымша заряд |
| `_Uacc` | АКБ кернеуі, В | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | заряд берілген мәндері, В | `(V << UACC)/10` |
| `_Iacc` | АКБ тогы, А | `IAcc_med_A_2 × 2`, таңбасыз магнитуда |
| `_IAcc_med_A_u16` | таңбалы АКБ тогы, А | `+` заряд, `−` разряд |
| `_PLoad` | жүктеме қуаты, Вт | `PLoad_L × 100`, таңбасыз магнитуда |
| `_PLoad_calc` | АКБ қуаты, Вт | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | желі кернеуі, В | `0` — желі жоқ, әйтпесе `V+100` |
| `_INET` | желі тогы, А | магнитуда |
| `_INET_16_4` | таңбалы желі тогы, А | таңба `PNET_Sign_P` бойынша: тұтыну `+`, желіге беру `−` |
| `_PNET` | желі қуаты, Вт | `int16(PNET_8) × 10` (таңбалы) |
| `_PNET_calc` | желі қуаты, Вт | `UNET × |INET_16_4|`, таңба `PNET_Sign_P` бойынша |
| `_TFNET` | желі жиілігі, Гц | `6250/V` |
| `_UOUTmed` | жүктеме кернеуі, В | `0` — жоқ, әйтпесе `V+100` |
| `_Temp_Grad0/1/2` | температуралар, °C | `V−50` |
| `_I2C_Err` | I²C қатесі | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **жалпы** КЭС тогы, А | тарихи атау; түпнұсқада орташа болған |
| `_P_mppt_avg` | КЭС қуаты, Вт | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | фазалық тоқтар, А | `((L+(H&0x7F)×256)/10)`, таңба — бит `0x80` |
| `_I_acc_3ph` | фазалық тоқтар қосындысы, А | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | релелер | `Status_RELEdop` биттері 0/1 |
| `fw` | бағдарлама нұсқасы | `(V&0x1F).(V>>5)` |

**Маңызды (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — енді КЭС бойынша **қосынды**,
орташа емес. Орташаны күткен тұтынушыларға белсенді КЭС санына бөлу керек.

**Маңызды (2026-10-18):** `_INET_16_4` таңбамен беріледі: тұтыну `+`, желіге беру `−`.
Магнитуда үшін `abs(_INET_16_4)` алыңыз.

### 3.3. `_MODE` режимдері

| Код | Мәтін |
|---|---|
| 0 | МАП өшірулі және кірісте желі жоқ |
| 1 | МАП өшірулі. Кірісте желі бар |
| 2 | МАП қосулы. АКБ-дан генерация. Желі жоқ |
| 3 | МАП қосулы және желіні трансляциялайды |
| 4 | Трансляция + заряд |
| 5 | Буферлік заряд |
| 6 | Заряд, 1-саты |
| 7 | Заряд, 2-саты |
| 8 | Қосымша заряд, 3-саты |
| 9 | Басқа заряд режимдері (`StatusCh` dec/bin) |
| 10 | Мәжбүрлі генерация |
| 11 | Тарифтік желі, максималды тариф, мәжбүрлі генерация |
| 12 | Тарифтік желі, минималды тариф |
| 13 | Трансляция + эко-қосымша заряд |
| 14 | Трансляция + желіге сату |
| 15 | Сыртқы толық зарядты күту |
| 16 | Тарифтік желі, трансляция + эко-қосымша заряд |
| 17 | Тарифтік желі, трансляция + желіге сату |
| 18 | Pmax қосымша заряд |

### 3.4. Басқа кілттер

Қосымша әрқашан беріледі: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` және олардың `_B`-нұсқалары,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — шамадан жүктелу бит-өрістері; `_UNET_Limit`, `_TFNET_Limit` —
берілген мәндер; `_RSErr*`, `_RSWarning` — қателер/ескертулер; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — қызметтік; `_CoolerSpeed`, `Temp_off` —
қызметтік.

## 4. `GET /read_json.php?device=mppt`

Барлық табылған КЭС-тердің көрінісі. Жауап — объектілердің JSON-массиві (әр КЭС-ке
бір). Барлық мәндер — **жолдар**. Бос слоттар (ұзындығы ≤ 10) алынып тасталады.

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

| Өріс | Мағынасы |
|---|---|
| `UID` | КЭС сериялық нөмірі |
| `Vc_PV`, `Ic_PV` | панельдер кернеуі/тогы |
| `V_Bat` | АКБ кернеуі |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | панельдер/шығыс/жүктеме/заряд қуаттары, Вт |
| `I_Ch`, `I_Out` | заряд/шығыс тоқтары |
| `Temp_Int`, `Temp_Bat` | температуралар (`x−50`) |
| `Pwr_kW` | тәуліктегі энергия, кВт·сағ (бөлшек) |
| `I_EXTS0/1`, `P_EXTS0/1` | тоқ/қуаттың сыртқы датчиктері |
| `Relay_C` | реле биттері (0/1/2) |
| `RSErrSis` | контроллер қателері |
| `Sign_C0`, `Sign_C1` | датчик таңбалары; әрқашан `0`, КЭС контрактісіне жарияланбайды |
| `Mode` | заряд режимі (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | тоқ таңбасы (`+`/`-`) |
| `MPP` | МРР нүктесі (`R`/`L`/`0`) |
| `windspeed` | ВГ жиілігі; `65535` — дерек жоқ |
| `FW` | бағдарлама нұсқасы |
| `R1..R3` | реле күйі |

`device` `{map, mppt, bat}`-тан тыс болса — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

«Шебер-Узел» үшін `batmon` эмуляциясы. Жауап — **екі** объектінің массиві.
Барлық мәндер — жолдар.

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

- `C_100_remain` — заряд бағасы, % (аппараттық `SoC`, басқа жағдайда OCV).
- `mppt_day_E` — тәуліктегі КЭС жалпы өндірісі, Вт·сағ
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — заряд бағасы, %.
- Қалған жинақтаушы өрістер — нөлдер: сәйкес есептегіштер бізде жоқ.

## 6. `GET /read_memory.php?offset=&count=`

Бекітілген МАП-тың жады бейнесінің байттары.

- `offset` ≥ 0, `count` ≥ 0.
- Шекаралар бейне өлшеміне кесіледі (`0x000..0x7FF`).
- Жауап — байттардың JSON-массиві `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Ескі `check_daemons.php` форматындағы демондар күйлері.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Кілт | Мәні |
|---|---|
| `mapd` | `0` — МАП қолжетімді (табылған немесе көрініс бар), `1` — жоқ |
| `mpptd0..3` | `0` — КЭС бар, `1` — жоқ (4 слот шегі) |
| `batmond` | `0` — КЭС бар (batmon эмуляциясы), `1` — жоқ |
| `bms` | `0` — МАП көрінісінде BMS-банктер бар, `1` — жоқ |
| `"0".."7"` | shm 5000 күй байттары: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | бізде жоқ → `1` |

## 8. `GET /read_bms.php`

ANT BMS-нің stateless-көрінісі, МАП-тың ағымдағы көрінісінен (бірінші қолжетімді)
жиналады. `bmslistener` демонының форматы.

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

- `devices` — бір элементтен артық емес.
- МАП/көрініс жоқ → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — айнымалы ұзындықтағы массивтер (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — әрқашан `0` (МАП көрінісінде
  бұл белгілер жоқ).

## 9. `GET /read_sec.php?id=&offset=&count=`

КЭС-тің виртуалды адрестік кеңістігінің байттары.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` міндетті емес (әдепкі 1), `offset+count ≤ 0xE80`.
- `offset=0` кезінде — байттардың JSON-массиві; `offset>0` кезінде — JSON-объект
  `{"<dec-адрес>":<byte>, ...}`.
- Қате (`id`/`offset` жоқ, диапазоннан тыс) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

Виртуалды кеңістік `KESE+KESR+KESS` көріністерінен жиналады: `0xE80` өлшемді
нөл массиві, оған мәндер бастапқы RAM-адрестері бойынша орналастырылады.

## 10. `POST /write_eeprom.php`

Бекітілген МАП-қа жазу. Авторизация — **Basic**.

Дене: `application/x-www-form-urlencoded`, `data` өрісі:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **ондық**.
- `addr == 0` → МАП командасы, `val` — команда коды (1..9).
- `addr != 0` → байт жазу: `addr` 0..0x7FF, `val` 0..255.
- Жұптар тәртібі сақталады; қатар келе жатқан регистрлік жазулар бір сұранысқа
  біріктіріледі, әр команда бөлек жүреді.
- `map-reader`-ге бір сұраныс шегі — **≤64 операция**; ұзын сериялар автоматты
  кесіледі.
- Клиентке жауап таймауты — **10 с**. Ұзақ жазу кезінде клиент `error` алуы мүмкін,
  ал операция кейінірек аяқталады.

Жауап: `text/html; charset=UTF-8`, мәтін `Ok` немесе `error`.

```bash
# МАП-ты қосу (команда ON=2)
curl -s -u 'malina:ҚҰПИЯСӨЗ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# сағатты 09:54 етіп жазу (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:ҚҰПИЯСӨЗ' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Команда кодтары мен регистрлер — `docs/map-write.md`. Жазудан кейін модуль МАП-ты
қайта оқиды және `read_json.php`/`read_memory.php` көріністерін қайта жинайды.

## 11. `GET|POST /write_sec.php`

КЭС-ке жазу. Авторизация — **Basic**.

### 11.1. Релелер

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- Ағымдағы күй `Relay_C` (0x3E3) ішінен оқылады; реле қажет күйде болса — жазбасыз
  ерте `Ok`.
- Әйтпесе биттердің RMW және күй байтын `0x0E3D` (3645) ішіне жазу.

### 11.2. Еркін ұяшықтар

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — ондық.
- `relay` берілсе, `data` еленбейді.

Жауап: `Ok`, `error` немесе `unread`. `unread` (кезек 2200) біздің API-де `error`
-мен алмастырылады.

```bash
curl -s -u 'malina:ҚҰПИЯСӨЗ' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. Ескі API-ден не қолдау көрсетілмейді

Тек жоғарыдағы ручкалар ғана көшірілді. Қалған legacy-эндпоинттер іске
асырылмаған; бір бөлігі бізде жоқ қосалқы жүйелерге қатысты (MPPT-тарих, batmon,
Emoncms, бұлт, USB-бэкап, апдейтер).

| Топ | Қолдау көрсетілмейді |
|---|---|
| Жүйе/желі | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Жаңарту/бэкап | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| Тарих/энергия | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Мәзір/баптаулар | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Бұлт | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Қызметтік | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Өзекті баламалар: жүйе күйі — `/api/system`, журналдар — `/api/log`,
қызметтер — `/api/services`, желі — веб-парақ `/network`, тарих — `/api/stats/*`
([api-native.md](api-native.md) қараңыз).

## 13. Қателер мен кодтар

| Жағдай | Жауап |
|---|---|
| `read_json.php` ішінде қате `device` | `400 {"error":"unsupported device"}` |
| `read_memory`/`read_sec` диапазоннан тыс | `error` немесе кесілген массив |
| Жазбада жоқ/қате Basic | `401` (`WWW-Authenticate: Basic`) |
| Жаман `data`, МАП-қа байланыс жоқ, бас тарту/таймаут | мәтін `error` |
| Әдіс GET/POST емес | `405` |

## 14. Python мини-декодері

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. Сілтемелер

- [api-native.md](api-native.md) — жаңа API (lists/raws, токендер).
- `docs/api-auth.md` — авторизация матрицасы.
- `docs/legacy/legacy_read_json_map.md` — `read_json.php?device=map` толық мэппингі.
- `docs/legacy/kes-legacy-malina2.md` — legacy-ручкалар КЭС разборы.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — `bat`/`read_bms` контрактілері.
- `docs/map-write.md`, `docs/map-write-api.md` — МАП-қа жазу.
- `docs/legacy/legacy-endpoints.md` — көшірілмеген ручкалар инвентаризациясы.