# API: legacy-თავსებადობა (სრული API)

დოკუმენტი კლიენტების შემქმნელებისთვის. აღწერს «მალინას» მთელ ძველ HTTP-API-ს
(კითხვა და ჩაწერა), რომელსაც `malina-web` მხარს უჭერს «მასტერ-კვანძთან» და
გარე მომხმარებლებთან თავსებადობისთვის. ახალი API — [api-native.md](api-native.md).

- ბაზა: `http://<მოწყობილობის-მისამართი>` (TLS — გარე შრე, აქ არ განიხილება).
- legacy-პასუხების ფორმატი — **`text/html; charset=UTF-8`**, მაგრამ ტანი შეიცავს JSON-ს ან
  ტექსტს (`Ok`/`error`). ეს ისტორიული ქცევაა, შეცდომა არ არის.
- legacy-კითხვა — **ავტორიზაციის გარეშე**; legacy-ჩაწერა — **HTTP Basic + PAM**.

## 1. ავტორიზაციის მატრიცა

| ენდპოინტი | მეთოდი | ავტორიზაცია |
|---|---|---|
| `/read_json.php?device=map` | GET | არა |
| `/read_json.php?device=mppt` | GET | არა |
| `/read_json.php?device=bat` | GET | არა |
| `/read_memory.php` | GET | არა |
| `/check_daemons.php` | GET | არა |
| `/read_bms.php` | GET | არა |
| `/read_sec.php` | GET | არა |
| `/write_eeprom.php` | POST | **Basic** (`malina` + პაროლი → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + პაროლი → PAM) |

- Basic-მომხმარებელი — ფიქსირებული: **`malina`**. პაროლი — იგივე, რაც ვებსა და
  SSH-სთვის. შემოწმება ხდება PAM-ით; წარმატებული შემოწმებები ქეშირდება 5 წუთით.
- არასწორი/არმყოფი Basic → `401`.
- Bearer-ტოკენი legacy-ჩაწერისთვის **არ მიიღება** (მხოლოდ Basic).
- Rate-limit და lockout არ არსებობს.

```bash
# კითხვა — ავტორიზაციის გარეშე
curl -s 'http://10.10.100.254/read_json.php?device=map'

# ჩაწერა — Basic
curl -s -u 'malina:პაროლი' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. MAP-ზე მიბმა

legacy-მოდული მიმაგრებულია **ერთ MAP-ზე** რიცხვითი სერიული ნომრით (სერიული
ნომრის ასო არ ითვალისწინება). `read_json.php`, `read_memory.php` და `write_eeprom.php`
მუშაობენ სწორედ მასთან. MAP-ის დაკარგვისას მოდული ელოდება 60 წამს, შემდეგ იჭერს
პირველ ხელმისაწვდომს. თუ მიბმა არ არის, ჩაწერა აბრუნებს `error`-ს.

## 3. `GET /read_json.php?device=map`

MAP-ის ველების სნაპშოტი ძველი `mapd`-ის ფორმატში. განახლდება დაახლოებით წამში ერთხელ.

- `device=map` — MAP-ისთვის ერთადერთი მხარდაჭერილი მნიშვნელობა.
- სხვა მნიშვნელობები → `400 {"error":"unsupported device"}`.
- ყველა მნიშვნელობა — **სტრიქონი** (ისტორიული ფორმატი).
- `timestamp` — სნაპშოტის unix-დრო, `time` — `სთ:წთ:წმ`.

### 3.1. მაგალითი

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

ჩვენი იმპლემენტაცია აბრუნებს **ფიქსირებულ** გასაღებთა სიმრავლეს; მაგალითში
არმყოფი გასაღებები ჩამოთვლილია §3.4-ში.

### 3.2. ძირითადი ველები და ფორმულები

| გასაღები | მნიშვნელობა | ფორმულა/შენიშვნა |
|---|---|---|
| `_UID` | MAP-ის სერიული ნომერი | `SerialNum0 + SerialNum1×256` |
| `_MODE` | კომპოზიტური რეჟიმი | 0..18, იხ. §3.3 |
| `_Status_Char` | დამუხტვის ბიტები | 1 — მცირე დენი, 2 — საჭიროა დამუხტვა, 4 — მე-2 საფეხური, 8 — დამუხტვა |
| `_Uacc` | AKB-ის ძაბვა, V | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | დამუხტვის დაყენებები, V | `(V << UACC)/10` |
| `_Iacc` | AKB-ის დენი, A | `IAcc_med_A_2 × 2`, უნიშნო სიდიდე |
| `_IAcc_med_A_u16` | AKB-ის დენი ნიშნით, A | `+` დამუხტვა, `−` განმუხტვა |
| `_PLoad` | დატვირთვის სიმძლავრე, W | `PLoad_L × 100`, უნიშნო სიდიდე |
| `_PLoad_calc` | AKB-ის სიმძლავრე, W | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | ქსელის ძაბვა, V | `0` — ქსელი არ არის, თორემ `V+100` |
| `_INET` | ქსელის დენი, A | სიდიდე |
| `_INET_16_4` | ქსელის დენი ნიშნით, A | ნიშანი `PNET_Sign_P`-ით: მოხმარება `+`, მიცემა `−` |
| `_PNET` | ქსელის სიმძლავრე, W | `int16(PNET_8) × 10` (ნიშნიანი) |
| `_PNET_calc` | ქსელის სიმძლავრე, W | `UNET × |INET_16_4|`, ნიშანი `PNET_Sign_P`-ით |
| `_TFNET` | ქსელის სიხშირე, Hz | `6250/V` |
| `_UOUTmed` | დატვირთვის ძაბვა, V | `0` — არ არის, თორემ `V+100` |
| `_Temp_Grad0/1/2` | ტემპერატურები, °C | `V−50` |
| `_I2C_Err` | I²C შეცდომა | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **ჯამური** KES-ის დენი, A | ისტორიული სახელი; ორიგინალში საშუალო იყო |
| `_P_mppt_avg` | KES-ის სიმძლავრე, W | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | ფაზური დენები, A | `((L+(H&0x7F)×256)/10)`, ნიშანი — ბიტი `0x80` |
| `_I_acc_3ph` | ფაზური დენების ჯამი, A | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | რელე | `Status_RELEdop`-ის ბიტები 0/1 |
| `fw` | პროგრამული ვერსია | `(V&0x1F).(V>>5)` |

**მნიშვნელოვანი (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — ახლა KES-ზე **ჯამია**, ვიდრე
საშუალო. მომხმარებლებმა, რომლებიც საშუალოს ელოდნენ, უნდა გაყონ აქტიური KES-ების რაოდენობაზე.

**მნიშვნელოვანი (2026-10-18):** `_INET_16_4` გადმოიცემა ნიშნით: მოხმარება `+`, მიცემა `−`.
სიდიდისთვის აიღეთ `abs(_INET_16_4)`.

### 3.3. რეჟიმები `_MODE`

| კოდი | ტექსტი |
|---|---|
| 0 | MAP გამორთულია და შესასვლელზე ქსელი არ არის |
| 1 | MAP გამორთულია. ქსელი შესასვლელზე |
| 2 | MAP ჩართულია. გენერაცია AKB-დან. ქსელი არ არის |
| 3 | MAP ჩართულია და ქსელს გადასცემს |
| 4 | გადაცემა + დამუხტვა |
| 5 | ბუფერული დამუხტვა |
| 6 | დამუხტვა, 1-ლი საფეხური |
| 7 | დამუხტვა, მე-2 საფეხური |
| 8 | დამუხტვა, მე-3 საფეხური |
| 9 | სხვა დამუხტვის რეჟიმები (`StatusCh` dec/bin) |
| 10 | იძულებითი გენერაცია |
| 11 | ტარიფული ქსელი, მაქსიმალური ტარიფი, იძულებითი გენერაცია |
| 12 | ტარიფული ქსელი, მინიმალური ტარიფი |
| 13 | გადაცემა + ეკო-მიწოდება |
| 14 | გადაცემა + ქსელში გაყიდვა |
| 15 | გარე სრული დამუხტვის ლოდინი |
| 16 | ტარიფული ქსელი, გადაცემა + ეკო-მიწოდება |
| 17 | ტარიფული ქსელი, გადაცემა + ქსელში გაყიდვა |
| 18 | Pmax მიწოდება |

### 3.4. სხვა გასაღებები

დამატებით ყოველთვის გადმოიცემა: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` და მათი `_B`-ვერსიები,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — გადატვირთვის ბიტველები; `_UNET_Limit`, `_TFNET_Limit` —
დაყენებები; `_RSErr*`, `_RSWarning` — შეცდომები/გაფრთხილებები; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — სამსახურებრივი; `_CoolerSpeed`, `Temp_off` —
სამსახურებრივი.

## 4. `GET /read_json.php?device=mppt`

ყველა ნაპოვნი KES-ის სნაპშოტი. პასუხი — JSON-მასივი ობიექტებიდან (თითო KES-ზე).
ყველა მნიშვნელობა — **სტრიქონი**. ცარიელი სლოტები (სიგრძე ≤ 10) გამოირიცხება.

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

| ველი | მნიშვნელობა |
|---|---|
| `UID` | KES-ის სერიული ნომერი |
| `Vc_PV`, `Ic_PV` | პანელების ძაბვა/დენი |
| `V_Bat` | AKB-ის ძაბვა |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | პანელების/გამოსავლის/დატვირთვის/დამუხტვის სიმძლავრეები, W |
| `I_Ch`, `I_Out` | დამუხტვის/გამოსავლის დენები |
| `Temp_Int`, `Temp_Bat` | ტემპერატურები (`x−50`) |
| `Pwr_kW` | ენერგია დღე-ღამეში, kWh (წილადი) |
| `I_EXTS0/1`, `P_EXTS0/1` | დენის/სიმძლავრის გარე სენსორები |
| `Relay_C` | რელეების ბიტები (0/1/2) |
| `RSErrSis` | კონტროლერის შეცდომები |
| `Sign_C0`, `Sign_C1` | სენსორების ნიშნები; ყოველთვის `0`, KES-ის კონტრაქტში არ ქვეყნდება |
| `Mode` | დამუხტვის რეჟიმი (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | დენის ნიშანი (`+`/`-`) |
| `MPP` | MPP-წერტილი (`R`/`L`/`0`) |
| `windspeed` | ქარის გენერატორის სიხშირე; `65535` — მონაცემები არ არის |
| `FW` | პროგრამული ვერსია |
| `R1..R3` | რელეების მდგომარეობა |

როცა `device` არ არის `{map, mppt, bat}` — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

`batmon`-ის ემულაცია «მასტერ-კვანძისთვის». პასუხი — **ორი** ობიექტის მასივი.
ყველა მნიშვნელობა — სტრიქონები.

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

- `C_100_remain` — დამუხტვის შეფასება, % (აპარატული `SoC`, სხვა შემთხვევაში OCV).
- `mppt_day_E` — KES-ის ჯამური გამომუშავება დღე-ღამეში, Wh
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — დამუხტვის შეფასება, %.
- დანარჩენი მაკუმულირებელი ველები — ნულები: შესაბამისი მთვლელები ჩვენ არ გვაქვს.

## 6. `GET /read_memory.php?offset=&count=`

მიმაგრებული MAP-ის მეხსიერების სურათის ბაიტები.

- `offset` ≥ 0, `count` ≥ 0.
- საზღვრები იჭრება სურათის ზომის მიხედვით (`0x000..0x7FF`).
- პასუხი — ბაიტების JSON-მასივი `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

დემონების სტატუსები ძველი `check_daemons.php`-ის ფორმატში.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| გასაღები | მნიშვნელობა |
|---|---|
| `mapd` | `0` — MAP ხელმისაწვდომია (ნაპოვნია ან არის სნაპშოტი), `1` — არა |
| `mpptd0..3` | `0` — KES არის, `1` — არა (ლიმიტი 4 სლოტი) |
| `batmond` | `0` — არის KES (batmon-ის ემულაცია), `1` — არა |
| `bms` | `0` — MAP-ის სნაპშოტში არის BMS-ბანკები, `1` — არა |
| `"0".."7"` | shm 5000-ის სტატუს-ბაიტები: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | ჩვენ არ გვაქვს → `1` |

## 8. `GET /read_bms.php`

ANT BMS-ის stateless-სნაპშოტი, აწყობილი MAP-ის მიმდინარე სნაპშოტიდან (პირველი ხელმისაწვდომი).
დემონის `bmslistener`-ის ფორმატი.

```json
{
  "updated": 1792355272,
  "devices": [
    {
      "deviceName": "AntBms 500 A/h (დომინატორი)",
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

- `devices` — არა უმეტეს ერთი ელემენტისა.
- MAP/სნაპშოტი არ არის → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — ცვლადი სიგრძის მასივები (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — ყოველთვის `0` (MAP-ის სნაპშოტში
  ეს ნიშნები არ არის).

## 9. `GET /read_sec.php?id=&offset=&count=`

KES-ის ვირტუალური მისამართებული სივრცის ბაიტები.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` არასავალდებულოა (სტანდარტულად 1), `offset+count ≤ 0xE80`.
- როცა `offset=0` — ბაიტების JSON-მასივი; როცა `offset>0` — JSON-ობიექტი
  `{"<dec-მისამართი>":<byte>, ...}`.
- შეცდომა (არ არის `id`/`offset`, დიაპაზონიდან გასვლა) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

ვირტუალური სივრცე იკრიბება `KESE+KESR+KESS` სნაპშოტებიდან: ნულების მასივი
ზომით `0xE80`, რომელშიც მნიშვნელობები თავსდება თავდაპირველი RAM-მისამართების მიხედვით.

## 10. `POST /write_eeprom.php`

ჩაწერა მიმაგრებულ MAP-ში. ავტორიზაცია — **Basic**.

ტანი: `application/x-www-form-urlencoded`, ველი `data`:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **ათობითი**.
- `addr == 0` → MAP-ის ბრძანება, `val` — ბრძანების კოდი (1..9).
- `addr != 0` → ბაიტის ჩაწერა: `addr` 0..0x7FF, `val` 0..255.
- წყვილების რიგი ინახება; ზედიზედ მიმავალი რეგისტრული ჩაწერები გაერთიანებულია ერთ
  მოთხოვნაში, ყოველი ბრძანება მიდის ცალ-ცალკე.
- ერთი განაცხადის ლიმიტი `map-reader`-ისთვის — **≤64 ოპერაცია**; გრძელი სერიები იჭრება
  ავტომატურად.
- კლიენტზე პასუხის ტაიმაუტი — **10 წამი**. ხანგრძლივი ჩაწერისას კლიენტმა შეიძლება მიიღოს
  `error`, ხოლო ოპერაცია დასრულდება მოგვიანებით.

პასუხი: `text/html; charset=UTF-8`, ტექსტი `Ok` ან `error`.

```bash
# MAP-ის ჩართვა (ბრძანება ON=2)
curl -s -u 'malina:პაროლი' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# საათის ჩაწერა 09:54 (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:პაროლი' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

ბრძანებების კოდები და რეგისტრები — `docs/map-write.md`. ჩაწერის შემდეგ მოდული ხელახლა
კითხულობს MAP-ს და ხელახლა აწყობს `read_json.php`/`read_memory.php` სნაპშოტებს.

## 11. `GET|POST /write_sec.php`

ჩაწერა KES-ში. ავტორიზაცია — **Basic**.

### 11.1. რელეები

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- მიმდინარე მდგომარეობა იკითხება `Relay_C`-დან (0x3E3); თუ რელე უკვე საჭირო
  მდგომარეობაშია — ადრეული `Ok` ჩაწერის გარეშე.
- თორემ ბიტების RMW და მდგომარეობის ბაიტის ჩაწერა `0x0E3D`-ში (3645).

### 11.2. თვითნებური უჯრედები

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — ათობითი.
- თუ გადაცემულია `relay`, `data` იგნორირებულია.

პასუხი: `Ok`, `error` ან `unread`. `unread` (რიგი 2200) ჩვენს API-ში იცვლება
`error`-ით.

```bash
curl -s -u 'malina:პაროლი' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. რა არ არის მხარდაჭერილი ძველი API-დან

გადმოტანილია მხოლოდ ზემო სახელურები. დანარჩენი legacy-ენდპოინტები არ არის რეალიზებული; ნაწილი
ეხება ქვესისტემებს, რომლებიც ჩვენ არ გვაქვს (MPPT-ისტორია, batmon, Emoncms, ღრუბელი,
USB-ბექაპი, განახლების სისტემა).

| ჯგუფი | არ არის მხარდაჭერილი |
|---|---|
| სისტემა/ქსელი | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| განახლება/ბექაპი | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| ისტორია/ენერგია | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| მენიუ/პარამეტრები | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| ღრუბელი | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| სამსახურებრივი | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

აქტუალური ალტერნატივები: სისტემის მდგომარეობა — `/api/system`, ჟურნალები — `/api/log`,
სამსახურები — `/api/services`, ქსელი — ვებ-გვერდი `/network`, ისტორია — `/api/stats/*`
(იხ. [api-native.md](api-native.md)).

## 13. შეცდომები და კოდები

| სიტუაცია | პასუხი |
|---|---|
| არასწორი `device` `read_json.php`-ში | `400 {"error":"unsupported device"}` |
| `read_memory`/`read_sec` დიაპაზონიდან გასვლა | `error` ან შეკვეცილი მასივი |
| ჩაწერაში Basic არ არის/არასწორია | `401` (`WWW-Authenticate: Basic`) |
| ცუდი `data`, MAP-ზე მიბმა არ არის, უარი/ტაიმაუტი | ტექსტი `error` |
| მეთოდი არ არის GET/POST | `405` |

## 14. მინი-დეკოდერი Python-ზე

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. ბმულები

- [api-native.md](api-native.md) — ახალი API (lists/raws, ტოკენები).
- `docs/api-auth.md` — ავტორიზაციის მატრიცა.
- `docs/legacy/legacy_read_json_map.md` — სრული მეპინგი `read_json.php?device=map`.
- `docs/legacy/kes-legacy-malina2.md` — KES-ის legacy-სახელურების გარჩევა.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — `bat`/`read_bms` კონტრაქტები.
- `docs/map-write.md`, `docs/map-write-api.md` — ჩაწერა MAP-ში.
- `docs/legacy/legacy-endpoints.md` — გადმოუტანელი სახელურების ინვენტარიზაცია.