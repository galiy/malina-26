# API: legacy-совместимость (полный API)

Документ для разработчиков клиентов. Описывает весь старый HTTP-API «Малины»
(чтение и запись), который поддерживается `malina-web` для совместимости с
«Мастер-Узлом» и внешними потребителями. Новый API — [api-native.md](api-native.md).

- База: `http://<адрес-устройства>` (TLS — внешний слой, здесь не рассматривается).
- Формат ответов legacy — **`text/html; charset=UTF-8`**, но тело содержит JSON или
  текст (`Ok`/`error`). Это историческое поведение, не ошибка.
- Legacy-чтение — **без авторизации**; legacy-запись — **HTTP Basic + PAM**.

## 1. Матрица авторизации

| Эндпоинт | Метод | Авторизация |
|---|---|---|
| `/read_json.php?device=map` | GET | нет |
| `/read_json.php?device=mppt` | GET | нет |
| `/read_json.php?device=bat` | GET | нет |
| `/read_memory.php` | GET | нет |
| `/check_daemons.php` | GET | нет |
| `/read_bms.php` | GET | нет |
| `/read_sec.php` | GET | нет |
| `/write_eeprom.php` | POST | **Basic** (`malina` + пароль → PAM) |
| `/write_sec.php` | GET/POST | **Basic** (`malina` + пароль → PAM) |

- Пользователь Basic — фиксированный: **`malina`**. Пароль — тот же, что для веб и
  SSH. Проверка идёт через PAM; успешные проверки кэшируются на 5 минут.
- Неверный/отсутствующий Basic → `401`.
- Bearer-токен для legacy-записи **не принимается** (только Basic).
- Rate-limit и lockout отсутствуют.

```bash
# чтение — без авторизации
curl -s 'http://10.10.100.254/read_json.php?device=map'

# запись — Basic
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
```

## 2. Привязка к МАП

Legacy-модуль закрепляется за **одним МАП** по числовому серийнику (буква серийника
не учитывается). `read_json.php`, `read_memory.php` и `write_eeprom.php` работают
именно с ним. При потере МАП модуль ждёт 60 секунд, затем подхватывает первый
доступный. Если привязки нет, запись возвращает `error`.

## 3. `GET /read_json.php?device=map`

Снимок полей МАП в формате старого `mapd`. Обновляется примерно раз в секунду.

- `device=map` — единственное поддерживаемое значение для МАП.
- Другие значения → `400 {"error":"unsupported device"}`.
- Все значения — **строки** (исторический формат).
- `timestamp` — unix-время снимка, `time` — `ЧЧ:ММ:СС`.

### 3.1. Пример

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

Наша реализация отдаёт **фиксированный** набор ключей; отсутствующие в примере
ключи перечислены в §3.4.

### 3.2. Ключевые поля и формулы

| Ключ | Смысл | Формула/примечание |
|---|---|---|
| `_UID` | серийник МАП | `SerialNum0 + SerialNum1×256` |
| `_MODE` | композитный режим | 0..18, см. §3.3 |
| `_Status_Char` | биты заряда | 1 — малый ток, 2 — нужен заряд, 4 — 2-я ступень, 8 — дозаряд |
| `_Uacc` | напряжение АКБ, В | `(H×256+L)/10` |
| `_Uch_T` / `_Ubuf_T` | уставки заряда, В | `(V << UACC)/10` |
| `_Iacc` | ток АКБ, А | `IAcc_med_A_2 × 2`, беззнаковая магнитуда |
| `_IAcc_med_A_u16` | ток АКБ со знаком, А | `+` заряд, `−` разряд |
| `_PLoad` | мощность нагрузки, Вт | `PLoad_L × 100`, беззнаковая магнитуда |
| `_PLoad_calc` | мощность АКБ, Вт | `Uacc × _IAcc_med_A_u16` |
| `_UNET` | напряжение сети, В | `0` — сети нет, иначе `V+100` |
| `_INET` | ток сети, А | магнитуда |
| `_INET_16_4` | ток сети со знаком, А | знак по `PNET_Sign_P`: потребление `+`, отдача `−` |
| `_PNET` | мощность сети, Вт | `int16(PNET_8) × 10` (знаковый) |
| `_PNET_calc` | мощность сети, Вт | `UNET × |INET_16_4|`, знак по `PNET_Sign_P` |
| `_TFNET` | частота сети, Гц | `6250/V` |
| `_UOUTmed` | напряжение нагрузки, В | `0` — нет, иначе `V+100` |
| `_Temp_Grad0/1/2` | температуры, °C | `V−50` |
| `_I2C_Err` | ошибка I²C | `(RSErrDop>>2) & 0x7` |
| `_I_mppt_avg` | **суммарный** ток КЭС, А | историческое имя; в оригинале было среднее |
| `_P_mppt_avg` | мощность КЭС, Вт | `Uacc × _I_mppt_avg` |
| `_I_ph1..3` | фазные токи, А | `((L+(H&0x7F)×256)/10)`, знак — бит `0x80` |
| `_I_acc_3ph` | сумма фазных токов, А | `I_ph1+I_ph2+I_ph3` |
| `_Relay1/2` | реле | биты 0/1 `Status_RELEdop` |
| `fw` | версия ПО | `(V&0x1F).(V>>5)` |

**Важно (2026-10-14):** `_I_mppt_avg`/`_P_mppt_avg` — теперь **сумма** по КЭС, а не
среднее. Потребителям, ожидавшим среднее, нужно делить на число активных КЭС.

**Важно (2026-10-18):** `_INET_16_4` отдаётся со знаком: потребление `+`, отдача `−`.
Для магнитуды берите `abs(_INET_16_4)`.

### 3.3. Режимы `_MODE`

| Код | Текст |
|---|---|
| 0 | МАП выключен и нет сети на входе |
| 1 | МАП выключен. Сеть на входе |
| 2 | МАП включён. Генерация от АКБ. Нет сети |
| 3 | МАП включён и транслирует сеть |
| 4 | Трансляция + заряд |
| 5 | Буферный заряд |
| 6 | Заряд, 1-я ступень |
| 7 | Заряд, 2-я ступень |
| 8 | Дозаряд, 3-я ступень |
| 9 | Прочие зарядные режимы (`StatusCh` dec/bin) |
| 10 | Принудительная генерация |
| 11 | Тарифная сеть, максимальный тариф, принудительная генерация |
| 12 | Тарифная сеть, минимальный тариф |
| 13 | Трансляция + эко-подкачка |
| 14 | Трансляция + продажа в сеть |
| 15 | Ожидание внешнего полного заряда |
| 16 | Тарифная сеть, трансляция + эко-подкачка |
| 17 | Тарифная сеть, трансляция + продажа в сеть |
| 18 | Подкачка Pmax |

### 3.4. Прочие ключи

Дополнительно всегда отдаются: `_ThFMAP`, `_TFNET_Limit`, `_UNET_Limit`,
`_RSErrDop`, `Temp_off`, `_I_acc_avg`, `_MPPTs_mode`, `_P_acc_3ph`, `_P_ph1..3`,
`_E_NET`, `_E_ACC`, `_E_ACC_CHARGE`, `_E_NET_SIGN` и их `_B`-версии,
`_I_MPPT_WIND`, `_P_MPPT_WIND`, `_fl_UAccChBUF_24h`.

`_F_Acc_Over`, `_F_Net_Over` — битфилды перегрузок; `_UNET_Limit`, `_TFNET_Limit` —
уставки; `_RSErr*`, `_RSWarning` — ошибки/предупреждения; `_Flag_ECO`,
`_flagUnet2`, `_MPPTs_mode`, `_Inet_flag` — служебные; `_CoolerSpeed`, `Temp_off` —
служебные.

## 4. `GET /read_json.php?device=mppt`

Снимок всех найденных КЭС. Ответ — JSON-массив объектов (по одному на КЭС).
Все значения — **строки**. Пустые слоты (длина ≤ 10) отбрасываются.

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

| Поле | Смысл |
|---|---|
| `UID` | серийник КЭС |
| `Vc_PV`, `Ic_PV` | напряжение/ток панелей |
| `V_Bat` | напряжение АКБ |
| `P_PV`, `P_Out`, `P_Load`, `P_curr` | мощности панелей/выхода/нагрузки/заряда, Вт |
| `I_Ch`, `I_Out` | токи заряда/выхода |
| `Temp_Int`, `Temp_Bat` | температуры (`x−50`) |
| `Pwr_kW` | энергия за сутки, кВт·ч (дробное) |
| `I_EXTS0/1`, `P_EXTS0/1` | внешние датчики тока/мощности |
| `Relay_C` | биты реле (0/1/2) |
| `RSErrSis` | ошибки контроллера |
| `Sign_C0`, `Sign_C1` | знаки датчиков; всегда `0`, в контракт КЭС не публикуются |
| `Mode` | режим заряда (`i`/`v`/`B`/`b`/`S`), ASCII |
| `Sign` | знак тока (`+`/`-`) |
| `MPP` | точка МРР (`R`/`L`/`0`) |
| `windspeed` | частота ВГ; `65535` — нет данных |
| `FW` | версия ПО |
| `R1..R3` | состояние реле |

При `device` вне `{map, mppt, bat}` — `400 {"error":"unsupported device"}`.

## 5. `GET /read_json.php?device=bat`

Эмуляция `batmon` для «Мастер-Узла». Ответ — массив из **двух** объектов.
Все значения — строки.

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

- `C_100_remain` — оценка заряда, % (аппаратный `SoC`, иначе OCV).
- `mppt_day_E` — суммарная выработка КЭС за сутки, Вт·ч
  (`Σ(Pwr_kW×1000 + Pwr_W)`).
- `est_soc` — оценка заряда, %.
- Остальные накопительные поля — нули: соответствующих счётчиков у нас нет.

## 6. `GET /read_memory.php?offset=&count=`

Байты образа памяти закреплённого МАП.

- `offset` ≥ 0, `count` ≥ 0.
- Границы обрезаются по размеру образа (`0x000..0x7FF`).
- Ответ — JSON-массив байт `[0..255]`, `text/html; charset=UTF-8`.

```http
GET /read_memory.php?offset=0&count=16
[3,141,30,0,0,7,2,1,225,1,0,0,13,5,4,0]
```

## 7. `GET /check_daemons.php`

Статусы демонов в формате старого `check_daemons.php`.

```json
{
  "0": 0, "1": 0, "2": 0, "3": 0, "4": 1, "5": 1, "6": 1, "7": 1,
  "mapd": 0,
  "mpptd0": 0, "mpptd1": 0, "mpptd2": 0, "mpptd3": 1,
  "chargerd": 1, "dbed": 1, "batmond": 0, "emd": 1, "emon": 1,
  "dbe_flash": 1, "bms": 0
}
```

| Ключ | Значение |
|---|---|
| `mapd` | `0` — МАП доступен (найден или есть снимок), `1` — нет |
| `mpptd0..3` | `0` — КЭС присутствует, `1` — нет (лимит 4 слота) |
| `batmond` | `0` — есть КЭС (эмуляция batmon), `1` — нет |
| `bms` | `0` — в снимке МАП есть BMS-банки, `1` — нет |
| `"0".."7"` | статусные байты shm 5000: 0 — mapd, 1..4 — mpptd0..3, 5 — chargerd, 6 — emon, 7 — dbe_flash |
| `chargerd`, `dbed`, `emd`, `emon`, `dbe_flash` | у нас отсутствуют → `1` |

## 8. `GET /read_bms.php`

Stateless-снимок ANT BMS, собранный из текущего снимка МАП (первый доступный).
Формат демона `bmslistener`.

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

- `devices` — не более одного элемента.
- Нет МАП/снимка → `{"updated":0,"devices":[]}`.
- `cells_v`, `temperatures_c` — массивы переменной длины (`cell_count`).
- `charge_mos`/`discharge_mos`/`balancer`/`frames` — всегда `0` (в снимке МАП нет
  этих признаков).

## 9. `GET /read_sec.php?id=&offset=&count=`

Байты виртуального адресного пространства КЭС.

- `id` — **1-based** (`1..N`).
- `offset` ≥ 0; `count` необязателен (по умолчанию 1), `offset+count ≤ 0xE80`.
- При `offset=0` — JSON-массив байт; при `offset>0` — JSON-объект
  `{"<dec-адрес>":<byte>, ...}`.
- Ошибка (нет `id`/`offset`, выход за диапазон) → `error`.

```http
GET /read_sec.php?id=1&offset=0&count=16
[5,1,168,0,0,50,0,0,0,0,0,0,1,1,0,0]

GET /read_sec.php?id=1&offset=32&count=4
{"32":0,"33":0,"34":0,"35":0}
```

Виртуальное пространство собирается из снимков `KESE+KESR+KESS`: массив нулей
размером `0xE80`, в который значения раскладываются по исходным RAM-адресам.

## 10. `POST /write_eeprom.php`

Запись в закреплённый МАП. Авторизация — **Basic**.

Тело: `application/x-www-form-urlencoded`, поле `data`:

```
data=[[<addr>,<val>], ...]
```

- `addr`, `val` — **десятичные**.
- `addr == 0` → команда МАП, `val` — код команды (1..9).
- `addr != 0` → запись байта: `addr` 0..0x7FF, `val` 0..255.
- Порядок пар сохраняется; подряд идущие регистровые записи объединяются в один
  запрос, каждая команда идёт отдельно.
- Лимит одной заявки к `map-reader` — **≤64 операций**; длинные серии режутся
  автоматически.
- Таймаут ответа клиенту — **10 с**. При длительной записи клиент может получить
  `error`, а операция завершится позже.

Ответ: `text/html; charset=UTF-8`, текст `Ok` или `error`.

```bash
# включить МАП (команда ON=2)
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[0,2]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok

# записать часы 09:54 (0x1B6=438, 0x44B=1099)
curl -s -u 'malina:ПАРОЛЬ' --data 'data=[[438,77],[1099,4]]' \
  'http://10.10.100.254/write_eeprom.php'
# → Ok
```

Коды команд и регистры — `docs/map-write.md`. После записи модуль перечитывает МАП
и пересобирает снимки `read_json.php`/`read_memory.php`.

## 11. `GET|POST /write_sec.php`

Запись в КЭС. Авторизация — **Basic**.

### 11.1. Реле

```http
GET /write_sec.php?id=1&relay=2&mode=on
```

- `id` — 1-based.
- `relay` — 1..3.
- `mode` — `on|off|1|0`.
- Текущее состояние читается из `Relay_C` (0x3E3); если реле уже в нужном
  состоянии — ранний `Ok` без записи.
- Иначе RMW битов и запись байта состояния в `0x0E3D` (3645).

### 11.2. Произвольные ячейки

```http
GET /write_sec.php?id=1&data=[[cell,val],[cell,val]]
```

- `cell`, `val` — десятичные.
- Если передан `relay`, `data` игнорируется.

Ответ: `Ok`, `error` или `unread`. `unread` (очередь 2200) в нашем API заменяется
на `error`.

```bash
curl -s -u 'malina:ПАРОЛЬ' \
  'http://10.10.100.254/write_sec.php?id=1&relay=1&mode=on'
# → Ok
```

## 12. Что из старого API не поддерживается

Перенесены только ручки выше. Остальные legacy-эндпоинты не реализованы; часть
относится к подсистемам, которых у нас нет (MPPT-история, batmon, Emoncms, облако,
USB-бэкап, апдейтер).

| Группа | Не поддерживается |
|---|---|
| Система/сеть | `get_cpu_mem.php`, `get_lan_state.php`, `get_wlan_state.php`, `eth_set.php`, `wifi_set.php`, `hostname_set.php`, `time_set.php`, `switch_lang.php`, `reboot_do.php`, `shutdown_do.php`, `service_control.php`, `devices.php`, `find_devices.php`, `errors_counter.php` |
| Обновление/бэкап | `check_update.php`, `update_main.php`, `update_start.php`, `usb_backup.php`, `get_usbhd_info.php`, `db_backup.php`, `db_restore.php`, `get_db_status.php`, `clear_events.php` |
| История/энергия | `get_data.php`, `get_map_e.php`, `get_mppt_e.php`, `map_errors.php`, `map_errors_history.php`, `map_counters_history.php`, `mppt_errors.php`, `mppt_errors_history.php`, `mppt_counters_history.php`, `batmon_history.php` |
| Меню/настройки | `load_menu.php`, `get_nodes.php`, `master_node_load.php`, `master_node_save.php`, `load_ab_set.php`, `save_ab_set.php`, `wind_mppts_set.php`, `events_get.php`, `events_set.php`, `email_set.php`, `email_test.php` |
| Облако | `cloud_switch.php`, `get_cloud_enable.php`, `get_cloud_status.php`, `set_cloud_settings.php`, `emoncms_switch.php`, `get_emoncms_enable.php`, `get_emoncms_status.php`, `get_emoncms_settings.php`, `set_emoncms_settings.php` |
| Служебные | `write_shm.php`, `get_eeprom_state.php`, `batmon_100.php`, `batmon_reset.php`, `request_node.php` |

Актуальные альтернативы: состояние системы — `/api/system`, журналы — `/api/log`,
службы — `/api/services`, сеть — веб-страница `/network`, история — `/api/stats/*`
(см. [api-native.md](api-native.md)).

## 13. Ошибки и коды

| Ситуация | Ответ |
|---|---|
| Неверный `device` в `read_json.php` | `400 {"error":"unsupported device"}` |
| Выход за диапазон `read_memory`/`read_sec` | `error` или обрезанный массив |
| Нет/неверный Basic в записи | `401` (`WWW-Authenticate: Basic`) |
| Плохой `data`, нет привязки к МАП, отказ/таймаут | текст `error` |
| Метод не GET/POST | `405` |

## 14. Мини-декодер на Python

```python
import json, urllib.request

def read_map(host):
    with urllib.request.urlopen(f"http://{host}/read_json.php?device=map") as r:
        return json.loads(r.read())

m = read_map("10.10.100.254")
print(m["_MODE"], m["_UNET"], m["_INET_16_4"])
```

## 15. Ссылки

- [api-native.md](api-native.md) — новый API (lists/raws, токены).
- `docs/api-auth.md` — матрица авторизации.
- `docs/legacy/legacy_read_json_map.md` — полный мэппинг `read_json.php?device=map`.
- `docs/legacy/kes-legacy-malina2.md` — разбор legacy-ручек КЭС.
- `docs/legacy/batmon-api.md`, `docs/legacy/bms-api.md` — контракты `bat`/`read_bms`.
- `docs/map-write.md`, `docs/map-write-api.md` — запись в МАП.
- `docs/legacy/legacy-endpoints.md` — инвентаризация неперенесённых ручек.
