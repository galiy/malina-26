# КЭС/MPPT в legacy ПАК «Малина-2» (разбор бэкапа .60)

Анализ боевого образа ПАК «Малина» (192.168.13.60) и человека-читаемых артефактов
внутри него. Источник — бэкап загрузочного диска:

    /u03/archive/solar/microart/malina_backup/titanator_malina_fixed_20261007
    ├─ part1_boot.vfat.img   (63 МиБ, /boot)
    ├─ part2_root.ext4.img   (4 ГиБ, /)         ← /usr/sbin/*, /etc
    └─ part3_settings.ext4.img (534 МиБ, /settings) ← /settings/daemons/*, /settings/html/*

Монтирование (только чтение):

    sudo mount -o loop,ro part2_root.ext4.img   /mnt/root
    sudo mount -o loop,ro part3_settings.ext4.img /mnt/settings

Связано: `docs/kes-reader.md` (§4 — контракт КЭС), `docs/events.md` (§11 — заглушки КЭС),
`docs/map-memory.md`. Общий обзор legacy — `docs/legacy/event-manager.md`,
`docs/legacy/master-node.md`.

Внимание: это **legacy-реализация на C/Perl/PHP поверх SysV IPC** — не наш стек.
Ниже фиксируем функциональность и форматы, а не код.

## 1. Как legacy находит и различает устройства (фактический probe)

Цепочка автообнаружения (hotplug):

- `/etc/udev/rules.d/108-usb.rules`: на `ttyUSB[0-9]` (add/change и remove) →
  `systemd` юнит **`add_tty.service` → `/usr/sbin/tty_scan.sh`**.
- `tty_scan.sh`: `sleep 10` (защита от «быстрого перевтыкания»); `find /dev/ttyUSB*`
  → `/var/map/devices_available.txt`; снимает флаги и убивает `mapd/mpptd/batmond/
  dbed/mserver/emd`; запускает **`/usr/sbin/usbscanner`**; затем `fill_shm.pl`;
  по найденным типам стартует нужные демоны (`mapd`/`mpptd`/`chargerd`).
- **`usbscanner`** (ELF ARM, stripped) — собственно определение типа устройства.
  Пишет строки в `/var/map/devices_found` формата **`<порт>D<тип>R<baudIdx>`**.
  Строки бинарника (из `strings`): `Checking device at port %s, and baudrate: %i`,
  `Device type %i found on baudrate: %i`, `%sD%dR%d`; спец-случай порта
  `/dev/serial0`/`/dev/ttyS0` → метка `S0` (= UART, «UART:»).

**Коды типа устройства (`D`):**

| D | Устройство | В нашей теме |
|---|---|---|
| `D0` | не распознано (пропуск) | — |
| `D3` | **MAC** (МАП) | МАП |
| `D5` | **MPPT** | **КЭС** |
| `D7`/`D9` | CHARGER (зарядник) | вне темы |

`get_devices.sh` считает `grep "D3|D5|D7" -c` → `MAC:n`, `MPPT:n`, `CHARGER:n`.
`/usr/sbin/devices_found` — симлинк на `/settings/daemons/devices_found.pl`
(человекочитаемый вывод: `MPPT on USB0:19200 bps, …`).

**Индекс скорости (`R`):** `R0=19200`, `R1=57600`, `R2=115200` (автоскан бодов,
совпадает с `docs/map/map/malina2.pdf`: «автоопределение скорости 19200–115200»).

> **Вывод для нашей задачи:** критерий «КЭС vs МАП» зашит внутри `usbscanner`
> (в скриптах только код `D5`). Точные байты probe оттуда — отдельная задача дизасма.

### 1a. Probe `usbscanner` — РАЗОБРАН (дизасм ARM)

`usbscanner` (ELF ARM, stripped) проанализирован статически (radare2/ARM). Логика:

- `main` (`0x11470`) проверяет кандидатов: `/dev/serial0` (метка «Checking MAC on
  UART...») и порты из `/var/map/devices_available.txt` (это `find /dev/ttyUSB*`).
  Для каждого порта перебирает **3 скорости** из таблицы `.data 0x21c54`:
  `B19200(0x0E)`, `B57600(0x1001)`, `B115200(0x1002)` → индекс `R0/R1/R2`.
- `check_device(port, baud)` (`0x111f0`): `open(port, O_RDWR|O_NOCTTY|O_NONBLOCK)`,
  `tcgetattr`/`cfmakeraw`/`cfsetospeed`/`cfsetispeed`/`tcsetattr`/`tcflush`,
  `isatty`-проверка (не tty → отказ).
- Шлёт **штатный кадр MicroArt-чтения**: `'r'(0x72)`, `P=0x00` (страница 1 байт),
  адрес `A=0x0000`, CRC (сумма mod 256, аккумулятор `0x21cc8`), `\n`. Экранирование —
  как в протоколе: `0x0A → DB DC`, `0xDB → DB DD`; каждый байт с эхо-подтверждением
  (`fn 0x10954` — отправка байта + сверка эха; `fn 0x10a18` — байт с эскейпом).
- Приём ответа (`fn 0x10f0c`): читает поток, эхо-подтверждает, расэкранирует,
  проверяет CRC, кладёт в буфер `.bss 0x21c64`.
- **Тип устройства = байт данных ответа = значение ячейки `0x0000` (`_Device`)**:
  `ldrb r3,[buf,1]` (buf[0]=`'o'`=0x6F, buf[1]=данные). Провал/нет ответа → `0`.

**Коды `_Device` (0x0000) — подтверждено двумя местами** (`usbscanner` возвращает код
как есть; `tty_scan.sh` мапит `D<code>` на демона):

| `_Device` | Буква | Устройство |
|---|---|---|
| `0` | `D0` | не распознано |
| **`3`** | `D3` | **МАП (MAC)** |
| **`5`** | `D5` | **КЭС / MPPT** |
| `7` | `D7` | CHARGER |
| `9` | `D9` | CHARGER (вариант) |

Запись строки: `%sD%dR%d` (порт, код, индекс скорости); провал → `%sD0R0`;
для UART — `/dev/serial0D%dR%d`.

**ИТОГО (снимает GAP §7.6 по маркеру):** **маркер КЭС = значение ячейки `0x0000`
равно `5`**; probe = MicroArt-чтение 1 байта по адресу `0x0000` (маркер МАП — `3`).
Серийник в `usbscanner` не читается (только ячейка 0).

### 1b. `mpptd` — РАЗОБРАН (дизасм ARM): окна чтения и карта ячеек

`mpptd` (ELF ARM, stripped) — один процесс на все КЭС. `main` читает
`/var/map/devices_found`, берёт строки `D5` (КЭС) и опрашивает их штатным
MicroArt-протоколом (в бинарнике — те же функции, что в `usbscanner`: отправка кадра
`0x11290`, приём `0x11610`, байт с эхо/эскейпом `0x1110c`; запись `'w'` — `0x11290`).

**Окна чтения (адрес → длина), функция `0x11984` и `0x11b0c`:**

| Адрес (dev) | Длина | Куда (RAM) | Что там |
|---|---|---|---|
| `0x0000` | 190 | `0x000..0x0BD` | ID/версии/`_Device`/SerialNum (читается как есть) |
| `0x02C2` | 240 | `0x0D2..0x1C1` | служебные/настройки (парсинг не подтверждён) |
| `0x04B2` | 240 | `0x2C2..0x3B1` | измерения + `UID` + `Mode` |
| `0x05A3` | 240 | `0x3B3..0x4A2` | ошибки/реле/знаки/датчики тока |

Ключ: **`image_offset == RAM-адрес`**, а **адрес устройства = RAM-адрес + `0x1F0`**
(в точности правило «+0x1F0» из карты `mppdt`!). Образ складывается в буфер (2048 Б на КЭС,
`idx<<11`).

**Отображение ячеек → поля JSON (подтверждено дизасмом `main`):**

| Поле JSON | Ячейка RAM | Примечание |
|---|---|---|
| **`UID`** | **`0x2C9`** (LE, 16 бит; `0x2C9` мл., `0x2CA` ст.) | при значении `0` → `idx+1` (совпадает с PDF: «UID при нулевом значении не определён»); берётся по модулю |
| **`Mode`** | **`0x2C2`** | ASCII-символ; mapper `fn 0x11d18`: если `0x2A < c ≤ 0x7C` → `c`, иначе `'U'` |
| `Sign` / `MPP` | `0x37A` / `0x379` | через тот же mapper (`'U'` если вне диапазона) |
| `Vc_PV` | 0x2D9 | |
| `Ic_PV` | 0x2DF (≈) | |
| `V_Bat` | 0x2E5 | |
| `P_PV` | 0x2EB | |
| `P_Out` (`P_MPPT`) | 0x2ED | |
| `P_Load` | 0x2F5 | |
| `P_curr` | 0x2F9 | |
| `I_Ch` | 0x2FB | |
| `I_Out` | 0x2FF | |
| `Temp_Int` / `Temp_Bat` | 0x31D / 0x31E | |
| `Pwr_kW` | 0x3A1 | + `Pwr_W` 0x39F |
| `Sign_C0` / `Sign_C1` | 0x404 / 0x405 | |
| `I_EXTS0` / `I_EXTS1` | 0x406 / 0x408 | ток датчиков |
| `P_EXTS0` / `P_EXTS1` | 0x40A / 0x40C | мощность датчиков |
| `Relay_C` | **0x3E3** | биты 0/1/2 = реле 1/2/3 |
| `RSErrSis` | **0x3B6** | биты см. §5 |
| **`windspeed`** | **`0x3F8`** (LE, 16 бит) | `65535` = н/д |
| `FW`, `R1`,`R2`,`R3` | — | источники в дизасме не привязаны (кандидат: `FW` из `_VerPO` 0x02) |

**Формат JSON** (строка в `mpptd`, `.rodata`): `{"timestamp","time","UID","Vc_PV","Ic_PV",
"V_Bat","P_PV","P_Out","P_Load","P_curr","I_Ch","I_Out","Temp_Int","Temp_Bat","Pwr_kW",
"Sign_C0","Sign_C1","I_EXTS0","I_EXTS1","P_EXTS0","P_EXTS1","Relay_C","RSErrSis","Mode",
"Sign","MPP","windspeed","FW","R1","R2","R3"}`. Пишется в shm и в
`/settings/logs/mpptN_energy.json` (формат `%d-%02d-%02d:%.3f`), ошибки —
`/settings/logs/mppt_errors.log` (`date:...,time:...,mppt:%d,error:%d`), метка живости —
`/var/map/mpptd_time_label`.

**Вывод для контракта:** карта `sunReceiver/docs/map/mppt` подтверждена и **дополнена**
тремя недокументированными ячейками: `Mode=0x2C2`, `UID=0x2C9`, `windspeed=0x3F8`,
`Sign/MPP=0x37A/0x379`. «Серийник» КЭС в legacy — это **`UID` из `0x2C9` (16 бит)**, а не
`SerialNum0/1` (те в ID-окне 0x18/0x19 есть, но `mpptd` их не использует).

### 1c. `mpptd` — запись/реле (дизасм)

Запись в КЭС: `mpptd` в хвосте `main` (`0x14dcc` `msgrcv` из очереди **2200**) принимает
строку, `strtok(":")` разбирает **`idx:cell:value:ts`**, проверяет `idx ≤ N-1`, затем:
1. пишет байт **`3` (`ComMAP_EEPromWR`)** в адрес `0` (команда «разрешение записи»);
2. пишет байт `value` страницей 1 по адресу **`cell`** (`uxth(cell)` — **как есть, без
   `+0x1F0`**).

**Реле КЭС:**
- **Чтение состояния** — из образа по смещению **`0x3E3` (`Relay_C`)** (см. §1b), биты
  0/1/2 = реле 1/2/3; это же значение уходит в JSON `"Relay_C"`.
- **Запись (legacy-клиент `write_sec.php`)**: шлёт в очередь 2200 сообщение с ячейкой
  **`3645` = `0x0E3D`** и новым байтом состояния (после чтения `0x3E3` из shm 1995 и
  пересборки битов). Т.е. legacy пишет состояние реле в **`0x0E3D`**, а читает из
  **`0x3E3`** — это **разные** ячейки.
- `0x0E3D` в карте `mppt/` **не описан** (по карте в районе `0xE30` — `I_Akb_MPPT[16]`).
  **Гипотеза:** `0x0E3D` — командная ячейка реле КЭС (write-регистр), тогда как `0x3E3` —
  их текущее состояние. **Требует подтверждения на железе** (при отсутствии — трактовать
  relay-запись КЭС как «сначала проверить на реальном устройстве», иначе не писать).

> Для нашего `kes-reader`: состояние реле публикуем из `0x3E3`; команду реле формируем так
> же, как откроется на железе (кандидат — запись байта в `0x0E3D`); до подтверждения —
> в событиях/API держать relay-запись как «не реализовано/под вопросом».

## 2. Демоны, флаги, watchdog

- Демоны: `mapd` (МАП), **`mpptd` (КЭС/MPPT)**, `batmond` (BMS/батарейный монитор),
  `dbed`, `mserver`/`mserverups`, `chargerd`, `scd`, `emd` (менеджер событий).
- Флаги-включатели в `/var/map/`: `mapd`, `mpptd`, `batmond`, `dbed`, `mserver`
  (`touch` файла = сервис разрешён).
- Watchdog: `check_daemons.sh` периодически зовёт `check_mapd.pl` / **`check_mpptd.pl`** /
  `check_emd.pl` / `check_ms.pl`. `check_mpptd.pl`: если `/var/map/mpptd_time_label`
  старше **60 с** → `kill -9 mpptd` (перезапуск делает `tty_scan`/сервис-логика).
  `check_daemon.sh <name>` → `pidof` (rc 0 = работает).
- `mpptd_start.sh`: `touch /var/map/mpptd`; если `mpptd` уже есть — выход; иначе старт
  `/usr/sbin/mpptd`; **и убивает `batmond`** (конфликт BMS↔MPPT — учесть при портировании).
- `mpptd_stop.sh`: снимает флаг и гасит `mpptd` и `batmond`.
- `mpptd_restart.sh`: `kill` → `sleep 1` → старт, если не поднялся.

## 3. IPC и shared memory (legacy-контракт)

SysV объекты (ключи и размеры — из `emd.pl`, `read_json.php`, `write_sec.php`,
`read_sec.php`, `read_bms.php`, `shm_init.pl`):

| Ключ | Размер | Назначение |
|---|---|---|
| **2016** | 4096 | **JSON-снимки КЭС**: 4 слота по 1024, разделитель `#EOF` в каждом |
| **1995** | 8192 | **сырой образ ячеек КЭС**: 4 блока по **2048 байт** (по 1 на КЭС) |
| 2015 | 2048 | JSON-снимок МАП |
| 1996 | 2048 | образ ячеек/меню МАП (EEPROM) |
| 2017 | 2048 | JSON батарейного монитора (2 слота × 1024) |
| 2018 | 1024/32768 | CHARGER/BMS (расхождение: `emd.pl` — 1024, `read_bms.php` — 32768) |
| 5000 | 1024 | статус-биты (`fill_shm.pl`: 20 байт = 1) |
| 0x7CE/0x7DF/0x7CC/0x7D2/0x7CA/0x7E0/0x7CB/0xFA0/0x7D1/0x7E1 | — | `shm_init.pl` (старая схема инициализации, часть уже не используется) |

SysV message queues (ключи сообщений):

| Ключ | Направление |
|---|---|
| **2200** | **to-mppt** (команды/запись в КЭС) |
| **2201** | **from-mppt** (ответ) |
| 2100 / 2101 | to-map / from-map |
| 2300 | EMD (события) |

Формат записи (из `write_sec.php`): **`<id>:<cell>:<value>:<timestamp>\0`**,
где `id` — индекс КЭС (0-based), `cell` — адрес ячейки, `value` — значение.
Ответ — эхо `<timestamp>` (проверка доставки), иначе `error`/`unread`.

## 4. Веб/HTTP API КЭС (legacy)

- `GET /read_sec.php?id=N&offset=A&count=C` — чтение ячеек из shm 1995:
  байты `id*2048 + offset` (offset<2048), возврат JSON-массивом (decimal).
  `id` 1..N, где N = число `D5` из `get_devices.sh`.
- `GET /write_sec.php?id=N&relay=R&mode=on|off|1|0` — **реле КЭС**;
  текущее состояние читается из байта `id*2048+0x3E3` (`Relay_C`), биты 0/1/2 = реле 1/2/3.
  Формируется запись ячейки **`3645` (0x0E3D)** со новым байтом состояния и шлётся в 2200.
- `GET /write_sec.php?id=N&data=[[cell,value],…]` — запись произвольных ячеек КЭС
  (десятичные числа), приоритет у `relay`.
- `GET /read_json.php?device=mppt` — до 4 JSON-объектов КЭС из shm 2016.
- `GET /read_json.php?device=map|bat` — аналогично для МАП/БМ.
- `GET /devices.php` (→ `get_devices.sh`), `GET /find_devices.php`
  (→ `systemctl start add_tty` = пересканировать порты).
- `POST /wind_mppts_set.php` (`numbers`) — список индексов КЭС группы «ветер»
  (0-based, через запятую) в `/settings/web-data/wind_mppts` + `mapd_restart`.
  В JSON поле `T`: `0` = солнце, `1` = ветер.

## 5. Набор полей КЭС (JSON, shm 2016)

Из `js/home.js`, `read_json.php`, `emd.pl` (`@mppt_fields_array`),
`locale/RU/local.inc` (`$legend_mppt`):

| Поле | RU-подпись | Примечание |
|---|---|---|
| `time` | — | время |
| `Mode`, `Sign`, `MPP` | — | компоненты строки режима (как у МАП; точный алгоритм — в `mpptd`) |
| `UID` | — | ID/серийник КЭС (в UI «ID:<n>») |
| `Vc_PV` | Напряжение панелей (ВГ), В | |
| `Ic_PV` | Ток панелей (ВГ), A | |
| `V_Bat` | Напряжение АКБ, В | |
| `P_PV` | Мощность панелей (ВГ), Вт | |
| `P_Out` | Выходная мощность, Вт | ≈ `P_MPPT` (0x2ED) карты |
| `P_Load` | Мощность нагрузки, Вт | |
| `P_curr` | Мощность заряда, Вт | |
| `I_Ch` | Ток заряда, A | |
| `I_Out` | Выходной ток, A | |
| `Temp_Int` | Внутренняя температура, °C | |
| `Temp_Bat` | Температура батареи, °C | |
| `P_EXTS0` / `P_EXTS1` | Внешняя мощность нагрузки, ДТ 1/2, Вт | от датчиков тока |
| `I_EXTS0` / `I_EXTS1` | Ток внешних датчиков, A | |
| `Pwr_kW` | Энергия за сутки, кВт·ч | + `Pwr_W`, Вт |
| **`windspeed`** | Частота вращения ВГ, мин⁻¹ | ветровая модель КЭС; `65535` = н/д |
| `Relay_C` | — | состояние 3 реле (биты 0/1/2) |
| `RSErrSis` | — | ошибки контроллера (см. ниже) |

Дополнительные поля КЭС для событий (`emd.pl`): `Mode, Vc_PV, Ic_PV, P_PV, V_Bat,
I_EXTS0, I_EXTS1, Pwr_kW, Temp_Int, Temp_Bat`.
Поля для графиков (`history.php`, `live_graphs.php`): `Vc_PV, Ic_PV, V_Bat, P_PV,
P_Out, P_Load, P_curr, I_Ch, I_Out, Temp_Int, Temp_Bat, P_EXTS0, P_EXTS1, windspeed`.

**Раскладка `Relay_C`** (`home.js::mppt_relay`): `index=[_,1,2,4]` → реле 1 = бит 0,
реле 2 = бит 1, реле 3 = бит 2 (совпадает с `write_sec.php`).

**Биты `RSErrSis`** (RU `local.inc`): 0 Перенапряжение панелей, 1 Перенапряжение АКБ,
2 Переразряд АКБ, 3 Короткое замыкание АКБ, 4 Перегрев АКБ, 5 Ошибка связи I2C,
6 н/о, 7 н/о.
**Биты `I2C_Err`**: 0 I2C Err Ack, 1 I2C Err Sum, 2 I2C Err Data Size,
3 I2C Err Protocol, 4..7 н/о.
**Биты `_RSErrDop` (МАП, смежные КЭС):** 3 = «Ошибка I2C MPPT», 4 = «Устаревшая
версия ПО MPPT» — порог версии КЭС.

## 6. Логи/история/SNMP

- Ошибки КЭС: `/settings/logs/mppt_errors.log` — строки
  `date:YYYY-MM-DD,time:HH:MM:SS,mppt:<N>,error:<код>` (`RSErrSis`).
  Веб: `get_mppt_errors.php` → `get_mppt_errors.sh <N>` (tail 20).
- Энергия: `/settings/logs/mppt<N>_energy.json` для `N=0..3`
  (`<unixtime>:<Wh>` построчно), агрегируется в web.
- SNMP-trap КЭС (`emd.pl`, `@mppt_traps_array`): `trapMpptMode`, `trapMpptPVVoltage`,
  `trapMpptPVPower`, `trapMpptBatteryVoltage`, `trapMpptCurrentSensor1/2`,
  `trapMpptEnergy`, `trapMpptInternalTemperature`, `trapMpptBatteryTemperature`.
- Лимит: **до 4 КЭС** (`mppt 0..3`, `read_json` — 4 слота, `/read_sec` id 1..4).

## 7. Итог по карте памяти из скриптов

Чего **не** нашлось в скриптах/вебе: полной таблицы адресов ячеек КЭС (она живёт в
самом `mpptd` и в карте `sunReceiver/docs/map/mppt`). Из скриптов подтверждены только:

- **`0x3E3` (`Relay_C`)** — состояние 3 реле (биты 0/1/2). ✔ совпадает с картой.
- **`3645` = `0x0E3D`** — ячейка, в которую `write_sec.php` пишет новое состояние
  реле (назначение в карте напрямую не указано — **уточнить при дизасме `mpptd`**;
  не путать с `Relay_C=0x3E3`, это `0x0E3D`).
- Размер блока КЭС в shm = **2048 байт**, отсюда чтение `id*2048+offset`.
- Поля/подписи/биты ошибок/реле — см. §5.

Остальные адреса (`Vc_PV=0x2D9` и т.д.) — **из карты `sunReceiver/docs/map/mppt`**,
в скриптах не дублируются.

## 8. Что это добавляет к нашему плану

1. Подтверждено независимо: КЭС = MPPT; **один процесс на все КЭС (до 4)** — прямое
   подтверждение схемы «`kes-reader`/`kes-sim` — один процесс, много портов»;
   один порт на устройство; MicroArt; скорости 19200/57600/115200 (дефолт 19200).
2. Формат записи в КЭС — по ячейкам (`<id>:<cell>:<value>:<ts>`), реле — через
   `Relay_C` (0x3E3) / запись 3645.
3. Готовые форматы ошибок (`RSErrSis`), энергии (кВт·ч/Вт за сутки), групп «солнце/ветер».
4. Новые поля контракта, которых не было в `docs/kes-reader.md`: **`P_Out`** и
   **`windspeed`** (ветровая модель).

## 9. Кандидаты на дизасм (по цене/пользе)

1. **`usbscanner`** (≈9 КБ) — **РАЗОБРАН** (§1a): probe = чтение ячейки `0x0000`, код `5`=КЭС.
2. **`mpptd`** (≈24 КБ, ARM, stripped) — **РАЗОБРАН** (§1b): окна чтения, карта ячеек,
   `UID`/`Mode`/`windspeed`, формат JSON.
3. `mapd` — сравнение probe/протокола МАП (опционально, не делали).
4. **`MAP2Gui_.exe`** (сборка `20260924`, Delphi PE32, stripped) — **частично**: из строк —
   `VerDevice: MAP SIN / Stab / Veter / **SUN** (КЭС) / SUN 2Ch / GridCharge / InvertStab`,
   `I2C MPPT`, `MAP / MPPT`, `MPPT (C)mART`, `BMS+MPPT mART`. Полный дизасм не требовался —
   probe уже получен из `usbscanner`/`mpptd`.
