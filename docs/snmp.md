# SNMP — требования (агент и trap)

Статус: **реализовано** (см. §4). Разбор legacy —
`docs/event-manager.md`, `docs/events.md`, `hardware.md`.

> **Артефакт:** официальный `MICROART-MIB.txt` (1545 строк, md5
> `5f3fff51e82d7c4996e5c6690a50ecf5`) перенесён в репозиторий — `docs/snmp/MICROART-MIB.txt`.
> Копии на legacy-устройстве идентичны (`/usr/share/snmp/mibs/`, `/settings/html/`).
> Структура MIB и OID-нумерация разобраны в §0.

В проекте SNMP покрывает две задачи. Их следует различать:

1. **SNMP-агент** — внешний **опрос** телеметрии МАП/КЭС по SNMP (мониторинг, напр. Zabbix).
   Реализуется **внутри `malina-web`** (Go), внешний пакет `snmpd` не используется.
2. **SNMP-trap** — активная **отправка** уведомлений движком событий (`malina-web`).

Ниже — отдельно по каждой. Обе функции сейчас **не реализованы** и в образ/деплой
по умолчанию не входят (включаются при необходимости).

---

## 0. MICROART-MIB — структура и нумерация

Файл: `docs/snmp/MICROART-MIB.txt`. Автор — Osolemio (28/5/17),
`NuDesign Visual MIBuilder 4.7`. База OID — **`enterprises 50330`**
(`microart ::= { enterprises 50330 }`), т.е. корень `1.3.6.1.4.1.50330`.

### 0.1. Дерево `mac` (МАП) — `{ microart 1 }`

```
mac  ::= { microart 1 }
├─ input   (mac 1)  — AC вход
│   ├─ inputVoltage(1)    Integer32  В
│   ├─ inputCurrent(2)    FloatValue А     (×0.1)
│   ├─ inputPower(3)      Integer32  ВА
│   └─ inputFrequency(4)  FloatValue Гц    (×0.1)
├─ output  (mac 2)  — AC выход
│   ├─ outputFrequency(1) FloatValue Гц    (×0.1)
│   └─ outputVoltage(2)   Integer32  В
├─ battery (mac 3)  — DC (АКБ)
│   ├─ batteryVoltage(1)    FloatValue В   (×0.1)
│   ├─ batteryCurrent(2)    FloatValue А   (×0.1)  [UNITS "W" — опечатка MIB]
│   ├─ batteryPower(3)      Integer32  Вт
│   ├─ batteryMpptCurrent(4) FloatValue А  (×0.1)  [суммарный ток КЭС по I2C]
│   └─ batteryMpptPower(5)    Integer32  Вт
├─ counters (mac 4)  — счётчики кВт·ч (KwhValue ×0.01)
│   ├─ countersENET(1) / countersEACC(2) / countersECHG(3)
├─ status   (mac 5)
│   ├─ statusMODE(1)  Integer32 (0..20)  — режим работы
│   ├─ statusModeText(2) DisplayString
│   ├─ statusRelay1(3) / statusRelay2(4)  OCTET(3) "on/off"
│   ├─ statusCooler(5)  OCTET(3) "off-med-hi"
│   ├─ statusTACC(6)  Integer32 °C  — темп. АКБ
│   └─ statusTMAC(7)  Integer32 °C  — темп. МАП
├─ info   (mac 6)
│   ├─ fwRev(1) / hwRev(2)  DisplayString
│   ├─ macPower(3)  OCTET(2..3) кВт
│   └─ dcVoltage(4)  Integer32 В  (read-write)
├─ batteryMonitor (mac 7)  — SoC(1) DoD(2) C20(3) TTG(4) ConsumedAh(5) UsersCounter(6) Status(7)
└─ macObjectsGroup (mac 8)
```

### 0.2. Дерево `mppt` (КЭС) — `{ microart 2 }`

```
mppt  ::= { microart 2 }
├─ mppt1..mppt4 (mppt 1..4)  — каждый по 17 полей:
│    PVVoltage(1) PVPower(2) PVCurrent(3) BatteryVoltage(4,×0.1)
│    CurrentSensor1(5) CurrentSensor2(6) Energy(7,×0.01)
│    InternalTemperature(8,°C) BatteryTemperature(9,°C)
│    Mode(10) Sign(11) Mpp(12) Fw(13)  — OCTET
│    Relay1(14) Relay2(15) Relay3(16)  — OCTET(3)
│    Windspeed(17, 0..65535; 65535 = н/д)
├─ mpptNo(5) Integer32  — номер MPPT (read-write)   [общий селектор — НЕ реализуем]
├─ mpptPVVoltage(6) … mpptMode(15)  — «выбранный» MPPT (общие, без индекса) [НЕ реализуем]
└─ mpptObjectsGroup (mppt 17)
```

### 0.3. `notifications` (trap'ы) — `{ microart 0 }`

Пары «основное условие» + «Ok» есть **не для всех** объектов: основная серия
`notifications 1..18` и Ok-серия `notifications 19..28` покрывают **парами** лишь
`inputVoltage/Current/Power`, `outputVoltage`, `batteryVoltage/Current/Power/MpptCurrent`,
`statusTacc`, `statusTmac`. Для `countersENET/EACC/ECHG` (9/10/11) и
`batteryMonitorDoD/SoC/UsersCounter/ConsumedAh` (15/16/17/18) есть **только основной**
трап (Ok-нотификации в MIB нет — см. §2.5 п.5, §2.10). Далее — MPPT-трапы с `mpptNo`
(29..47), `trapBatteryMpptPower(49)` / `…PowerOk(50)`, и `notificationsGroup(48)`.
У части MPPT-объектов тоже нет пары (см. ниже).

| OID | Trap | OID | Trap (Ok) |
|---|---|---|---|
| .0.1 | trapInputVoltage | .0.19 | trapInputVoltageOk |
| .0.2 | trapInputCurrent | .0.20 | trapInputCurrentOk |
| .0.3 | trapInputPower | .0.21 | trapInputPowerOk |
| .0.4 | trapOutputVoltage | .0.22 | trapOutputVoltageOk |
| .0.5 | trapBatteryVoltage | .0.23 | trapBatteryVoltageOk |
| .0.6 | trapBatteryCurrent | .0.24 | trapBatteryCurrentOk |
| .0.7 | trapBatteryPower | .0.25 | trapBatteryPowerOk |
| .0.8 | trapBatteryMpptCurrent | .0.26 | trapBatteryMpptCurrentOk |
| .0.9 | trapCountersENET | .0.27 | trapStatusTaccOk |
| .0.10 | trapCountersEACC | .0.28 | trapStatusTMacOk |
| .0.11 | trapCountersECHG | .0.29 | trapMpptMode |
| .0.12 | trapStatusMode | .0.31 | trapMpptPVVoltageOk |
| .0.13 | trapStatusTacc | .0.32 | trapMpptPVPower |
| .0.14 | trapStatusTMac | .0.33 | trapMpptPVPowerOk |
| .0.15 | trapBatteryMonitorDoD | .0.34 | trapMpptPVCurrent |
| .0.16 | trapBatteryMonitorSoC | .0.35 | trapMpptPVCurrentOk |
| .0.17 | trapBatteryMoitorUsersCounter | .0.36 | trapMpptBatteryVoltage |
| .0.18 | trapBatteryMonitorConsumedAh | .0.37 | trapMpptBatteryVoltageOk |
| .0.49 | trapBatteryMpptPower | .0.38 | trapMpptCurrentSensor1 |
| .0.50 | trapBatteryMpptPowerOk | .0.39 | trapMpptCurrentSensor1Ok |
| .0.42 | trapMpptEnergy | .0.40 | trapMpptCurrentSensor2 |
| .0.43 | trapMpptInternalTemperature | .0.41 | trapMpptCurrentSensor2Ok |
| .0.44 | trapMpptInternalTemperatureOk | .0.45 | trapMpptBatteryTemperature |
| .0.46 | trapMpptBatteryTemperatureOk | .0.47 | trapMalinaTime |

> **Нумерация не сплошная**: `.0.30` и `.0.48` пропущены (48 занят
> `notificationsGroup`), у MPPT-трапов нет Ok-варианта для `Mode`/`Energy`. Это
> важно для числового маппинга (см. §2.6/§2.7).

### 0.4. Особенности/опечатки (важно при порте)

- `batteryCurrent` объявлен с `UNITS "W"` — фактически ток в **А** (опечатка MIB).
- `batteryMonitorCurrentCapacity` (mib) против имени в `macObjectsGroup`.
- В `macObjectsGroup` нет `batteryMpptPower` — но сам объект `batteryMpptPower(5)`
  существует (просто не в группе).
- MPPT `mppt1..mppt4` — **фиксированные** экземпляры; «общие» `mpptPVVoltage(6)…`
  задают «выбранный» MPPT через `mpptNo(5)`. **Используем только фиксированные
  `mpptN`; общий путь `mpptNo(5)`/`.2.6..2.15` — НЕ реализуем** (решено, см. §2.7).
- `FloatValue` — `DISPLAY-HINT "d-1"` (значение ×10), `KwhValue` — `"d-2"` (×100):
  значения в трапах приходят **целыми с множителем**, как и заявлено в
  `docs/events.md` §6.5.

---

## 1. SNMP-агент (в `malina-web`)

### 1.1. Что это и зачем в legacy

- В legacy-Малине работал дистрибутивный `snmpd` (`5.7.2.1+dfsg-1+deb8u1` на Raspbian 8),
  см. `hardware.md` §14 (`/settings/snmp/`) и §15.7 (роль сервисов: `snmpd` остаётся на `.60`).
- Назначение — **внешний мониторинг** параметров устройства по SNMP-опросу
  (GET/GETNEXT/WALK), как альтернатива/дополнение к веб-API.
- В новом ПАК пакет **не входит** в образ (`image/README.md`) и в деплой
  (`requirements.md` «Осталось»).

### 1.2. Цель (для нового ПАК)

**SNMP-агент встраивается в `malina-web`** (Go, свой BER/ASN.1-код, UDP-слушатель),
внешний пакет `snmpd` **не нужен**. Цель — экспортировать телеметрию МАП/КЭС по
SNMP v2c, чтобы внешняя система мониторинга (Zabbix/Nagios) опрашивала устройство
по SNMP-протоколу, не заходя в веб. Тот же код (маппинг §2.6/§2.7) используется и
для trap-ов (§2).

> Агент, как и trap, включается по необходимости — но при включении это **наш
> код в web**, а не дистрибутивный `snmpd`.

### 1.3. Требования

| # | Требование | Комментарий |
|---|---|---|
| 1 | Агент **встраивается в `malina-web`** (Go, свой BER/ASN.1-код) | внешний пакет `snmpd` **не ставим**: нет лишнего пакета/конфликтов; источник — shm наших демонов, без legacy-API |
| 2 | Отдавать значения МАП/КЭС | источник — `map-reader`/`kes-reader` shm, единый с веб-API |
| 3 | Определить **OID-схему** | **MICROART-MIB** (см. §0, маппинг §2.6/§2.7) |
| 4 | Параметры к экспорту | курированный список из `docs/events.md` §4 (напряжения/токи/мощности сети, АКБ, MPPT, температуры) + энергии + режим |
| 5 | Протокол/community | v2c, community `public` (см. §1.4.5, §1.6) |
| 6 | Аутентификация/доступ | только чтение (ro); при необходимости — ограничить списком адресов опрашивающих |
| 7 | Конфиг агента | версионируемый файл на `/malina` вкл/порт/community; применение без пересборки (перезапуск web или реконфиг) |
| 8 | Юнит/systemd | **без отдельного юнита** — часть `malina-web.service`; порт 161 <1024 (web работает от root, привилегии не ограничиваются) |
| 9 | Данные при отсутствии устройства | возвращать «нет значения»/`noSuch` корректно, не ошибку агента |
| 10 | Единицы/множители | значения в фиксированных физ. единицах (как в `mainlist`/`map-contract`), множители — явно в MIB/схеме |

### 1.4. Решения (агент)

Статус по каждому вопросу: **решено** (вынесено из §0/§2).

| # | Вопрос | Решение |
|---|---|---|
| 1 | Нужен ли агент, или достаточно trap+API? | **Агент встраивается в `malina-web`** (Go-код, тот же маппинг что у trap). Внешний `snmpd` **не ставим**. Включается по необходимости (если внешняя система мониторинга опрашивает по SNMP); в противном случае — trap + веб-API. |
| 2 | OID-базис | **MICROART-MIB** (§0) — перенесён в `docs/snmp/MICROART-MIB.txt`. Собственная схема не нужна. |
| 3 | Какие параметры экспортировать | **Курированный список `docs/events.md` §4** (U/I/P сети, АКБ, MPPT, темп.) + **энергии** (`countersENET/EACC/ECHG`) + **режим** (`statusMODE`) — см. маппинг §2.6/§2.7. |
| 4 | Читать из shm или через `/api/…` | **Напрямую из shm** демонов (`map-reader`/`kes-reader`) — без промежуточного HTTP (один источник правды, меньше задержка). |
| 5 | v2c `public` или v3 | **v2c `public`** (см. §3 «Безопасность») — локальная сеть. При необходимости — ограничить агента списком адресов; переход на v3 — только по явному требованию. |
| 6 | КЭС в SNMP? | **МАП + КЭС** (маппинг §2.7 готов). Trap и агент покрывают оба. |
| 7 | Несколько МАП на хост? | **Ограничение — 1 МАП на хост** (как legacy/master-node). При нескольких МАП в shm берём **первый** в списке (по порядку слота `map-list`). Отдельная SNMP-схема с индексами **не вводится** — канонический MICROART-MIB (§0) рассчитан на 1 МАП. Разрыва с legacy-моделью нет. |

### 1.5. Параметры экспорта (агент)

Минимальный согласованный набор (для МАП — §2.6, для КЭС — §2.7):

- **МАП**: `inputVoltage/Current/Power/Frequency`, `outputFrequency/Voltage`,
  `batteryVoltage/Current/Power/MpptCurrent/MpptPower`, `countersENET/EACC/ECHG`,
  `statusMODE/ModeText/Relay1/Relay2/Cooler/TACC/TMAC`, `info.fwRev/macPower/dcVoltage`,
  `batteryMonitorSoC`.
- **КЭС**: `mpptN.PVVoltage/PVPower/PVCurrent/BatteryVoltage/CurrentSensor1/2`,
  `Energy/InternalTemperature/BatteryTemperature/Mode/Relay1..3/Windspeed`.

### 1.6. Слушатель/порт (агент)

| Параметр | Решение |
|---|---|
| Транспорт | UDP (SNMP-агент). Слушатель — отдельный goroutine внутри `malina-web`. |
| Порт | **UDP 161** — канонический SNMP, Zabbix по умолчанию. |
| Привилегия | Порт <1024 (SNMP UDP 161, HTTP 80) и системные операции (chpasswd/`mount` в `malina-rw`) — web-админка работает от root, набор привилегий в юните **не ограничивается** (`CapabilityBoundingSet` ломал `pam_chauthtok` при смене пароля — см. `manuals/ui.md`). |
| Ответы | GET / GETNEXT / WALK по одному и тому же маппингу §2.6/§2.7; типы — по MIB (`Integer32`, `OCTET STRING`, `Counter32/64` для счётчиков кВт·ч). |
| Community | `public`, v2c — по §1.4.5. |
| Отсутствие устройства | `noSuchInstance`/`noSuchName` — корректный ответ, не ошибка. |
| Конфиг | вкл/порт/community — флаг или версионируемый конфиг на `/malina` (применение без пересборки). |
| Взаимодействие | агент НЕ мешает HTTP-серверу; один процесс — общий источник shm и общий маппинг с trap. |
| Состояние | **stateless**: без накопления статистики/trap-истории/счётчиков в памяти между запросами. Каждый запрос → чтение актуального снимка из shm → ответ. |
| Активация | **лентяй** (для опроса): SNMP-агент (GET/GETNEXT/WALK) работает **только** при поступлении внешнего запроса. Вне запросов опрос shm не идёт. |
| Фоновая работа | **только при включённой периодической отправке трапов** (§2.9) — опциональный таймер, по умолчанию **выключен**. В выключенном состоянии фоновой работы/опроса нет. |

> Zabbix-совместимость: `snmpget`/`snmpwalk` по OID из §2.6/§2.7; на стороне Zabbix
> подключить `docs/snmp/MICROART-MIB.txt` для человеческих имён.

---

## 2. SNMP-trap (движок событий)

### 2.1. Что это

Действие `snmp` в «Менеджере событий»: при срабатывании правила (фронт условия)
отправляется **SNMP-trap** на заданный приёмник. Обратного действия («Ok») для
SNMP — тоже слать при выходе условия в норму, как в legacy.

Сейчас в `docs/events.md` действие `snmp` **объявлено в контракте, но не реализовано**
(§6, §11, §12): `eventsActions.SNMPTrap → errNotImpl`.

### 2.2. Требования (из `docs/events.md`, §4/§5/§6/§11)

| # | Требование | Источник |
|---|---|---|
| 1 | Действие `action:"snmp"` — в контракте правила | `docs/events.md` §5 |
| 2 | `trap_host` — IP приёмника trap, поле **видно только** для `snmp` | §5, §8 |
| 3 | Протокол **v2c**, community `public`, порт **162** | `docs/event-manager.md` §5, `docs/events.md` §6.5, §11 |
| 4 | OID-схема — **MICROART-MIB** (как в legacy) | `docs/events.md` §6.5, §11 |
| 5 | В trap кладётся **значение параметра с множителем** (×10/×100) и **номер MPPT** | `docs/event-manager.md` §5 |
| 6 | OID события + OID параметра со значением | `docs/events.md` §12 |
| 7 | Фронт: срабатывание при входе в условие; «Ok» при выходе (если действие `email`/`trap`) | `docs/events.md` §6, `docs/event-manager.md` §8.3 |
| 8 | Кулдаун на правило (по умолчанию 30 c) — общий для всех действий | `docs/events.md` §5.9 |
| 9 | Логировать отправку trap в отдельный лог (как ключевые события web) | `docs/events.md` §7 |

### 2.3. Требования к данным (из legacy, §5 event-manager)

- Событие параметра — тип значения `param` (число), сравнивается с порогом
  (`≤/≥/=/≠`), множители ×10/×100 — как в источниках.
- Номер MPPT/КЭС — передаётся в trap (`mppt_no`).
- Источник события — серийник МАП (`uint16`, без буквы); поле — курированное имя
  (`field`), не сырой индекс.

### 2.4. Ограничения/заглушки (пока)

| Ограничение | Что это значит |
|---|---|
| `eventsActions.SNMPTrap` → `errNotImpl` (для МАП) | ~~перенести MIB/OID~~ — **готово** (MIB §0, маппинг §2.6), остаётся реализация отправки |
| SNMP-trap для **КЭС** | маппинг готов (§2.7); источник на `.60` уже есть (три КЭС); реализация — в общем SNMP-блоке |
| **1 МАП на хост** (для trap) | движок событий **остаётся мульти-МАП** (правила адресуют источник по серийнику), **НО** SNMP-trap шлётся только от «первого» МАП (как агент, §1.4.7). События от прочих МАП с действием `snmp` — отбрасываются (лог + пропуск). |

### 2.5. Решения (trap)

1. ~~Переносить ли полный MICROART-MIB~~ — перенесён целиком (§0).
2. ~~Где хранить MIB/таблицу OID~~ — `docs/snmp/MICROART-MIB.txt` (+§0-§2.7).
3. ~~Использовать ли `snmptrap`~~ — **свой v2c-код в Go** (без внешнего `snmptrap`/net-snmp-бинарника): меньше зависимостей, отправка в горутине без блокировки движка, порт 162, community `public` (**решено**).
4. ~~Множитель значения в trap~~ — **по MIB** (§2.6/§2.7): `FloatValue` ×10, `KwhValue` ×100, целые — как есть; номер MPPT кладётся `mpptNo` (**решено**).
5. ~~Отправлять ли «Ok»-trap~~ — **для `param` при наличии Ok-нотификации в MIB**
   (как legacy: выход условия в норму → «Ok»-trap), общий движок фронтов (§2.2 п.7);
   тумблер на правило — не нужен (**решено**). **Исключение:** для полей **без**
   Ok-нотификации — `countersENET/EACC/ECHG`, `batteryMonitorDoD/SoC/UsersCounter/ConsumedAh`
   (и со стороны КЭС — `mpptMode`/`mpptEnergy`) — при выходе в норму Ok **не шлём**,
   отправляется только фронт-трап (нет OID для «Ok»).

### 2.6. Маппинг MIB → поля МАП (`{"map": …}` → OID `mac`)

Источник значений — снимок МАП (`map-reader` shm / `docs/map-contract.md`, `map.js`).
«Сырое» — целое в ячейке; «OID-значение» — приведённое к типу/множителю MIB.
Стрелка `→` = конверсия из ячейки МАП в значение MIB.

| OID (полное) | Объект MIB | Тип/множ. | Источник МАП | Конверсия |
|---|---|---|---|---|
| `.1.1.1` | inputVoltage | int, В | `UNET` | `+100` (0 → 0) |
| `.1.1.2` | inputCurrent | ×0.1, А | `INET_16_4` | `/4` или `/16` (по `INET≥16`) → ×10 |
| `.1.1.3` | inputPower | int, ВА | `UNET`,`INET_16_4` | `(UNET·INET_16_4)` **по модулю** (знак теряется — см. §2.8) |
| `.1.1.4` | inputFrequency | ×0.1, Гц | `TFNET` | `6250/TFNET` → ×10 |
| `.1.2.1` | outputFrequency | ×0.1, Гц | `ThFMAP` | `6250/ThFMAP` → ×10 |
| `.1.2.2` | outputVoltage | int, В | `UOUTmed` | `+100` (0 → 0) |
| `.1.3.1` | batteryVoltage | ×0.1, В | `UAcc_med_VH/VL` | `(VH·256+VL)/10` → **как есть** (целое ×0.1) |
| `.1.3.2` | batteryCurrent | ×0.1, А | `IAcc_med_A_u16_L/H` | `(L+H·256)/16`, знак по режиму → ×10 |
| `.1.3.3` | batteryPower | int, Вт | `UAcc_med_VH/VL`,`IAcc_med_A_u16_L/H` | `(batteryVoltage/10)·(batteryCurrent/10)` = U(В)·I(А) |
| `.1.3.4` | batteryMpptCurrent | ×0.1, А | `IAkbMPPT[16]` (КЭС, `0xE30`) | **сумма** `IAkbMPPT[]` → привести к ×0.1 А (**решено:** источник остаётся `IAkbMPPT`; на .60 массив пуст — параллельных MPPT нет, масштаб не проверен) |
| `.1.3.5` | batteryMpptPower | int, Вт | `Uacc·Imppt` | `Uacc·Imppt` |
| `.1.4.1` | countersENET | ×0.01, кВт·ч | `M_POWhourNET_L/H/HH` | `(L+H·256+HH·65536)/100` |
| `.1.4.2` | countersEACC | ×0.01, кВт·ч | `M_POWhourMAP_*` | та же |
| `.1.4.3` | countersECHG | ×0.01, кВт·ч | `M_POWhourMAPCharge_*` | та же |
| `.1.5.1` | statusMODE | int | `MODE`,`StatusCh` | `modeNum()` (общая функция) |
| `.1.5.2` | statusModeText | string | `modeNum()` | подпись `p1.modeN` |
| `.1.5.3` | statusRelay1 | OCTET "on/off" | `Status_RELEdop` | `bit0` |
| `.1.5.4` | statusRelay2 | OCTET "on/off" | `Status_RELEdop` | `bit1` |
| `.1.5.5` | statusCooler | OCTET "off/med/hi" | `CulerSpead` | `0→off,1→med,2→hi` |
| `.1.5.6` | statusTACC | int, °C | `Temp_Grad0` | `−50` |
| `.1.5.7` | statusTMAC | int, °C | `Temp_Grad2` | `−50` |
| `.1.6.1` | fwRev | string | `VerPO` | `(v&0x1f)+"."+(v>>5)` |
| `.1.6.3` | macPower | кВт | `POW` | `POWER_KW[POW]` |
| `.1.6.4` | dcVoltage | int, В | `UACC` | `{12,24,48,96}[UACC]` |
| `.1.7.1` | batteryMonitorSoC | int, % | `SoC` | `/100` (или н/д) |

> **Множитель `batteryVoltage`**: ячейка МАП `UAcc_med_VH/VL` уже даёт десятые
> (`(VH·256+VL)/10`), т.е. **готовое значение ×0.1** — в OID уходит **как есть**
> (не переводим в целые вольты), так `DISPLAY-HINT "d-1"` соответствует целому ×10.
> `batteryCurrent`/`inputCurrent` — наоборот, сырые → требуют приведения к ×0.1.

> **Трапы МАП** — OID из §0.3 (`notifications 1..28`), в тело trap кладётся
> соответствующий объект MIB (напр. `trapInputVoltage` → `inputVoltage`), значение с
> указанным множителем. «Ok»-трапы — для `param`-событий при выходе условия в норму.

### 2.7. Маппинг MIB → поля КЭС (`{"kes": …}` → OID `mpptN`)

Источник — снимок КЭС (`kes-reader` shm / `docs/kes-contract.md`, `KesData`).
**Номер экземпляра `mpptN` = порядковый индекс активного КЭС в `kes-list`
(`1..4`); при КЭС больше 4 берём первые 4** (схема MIB рассчитана на 4; на `.60`
сейчас три КЭС — серийники `623`/`30815`/`59235`). Значения из `KesData` уже в
«человеческих» единицах (×100/×10 — см. `docs/kes-contract.md`), поэтому к OID
приводим явно. Реализуется **только** фиксированный путь `mpptN`; общий путь
`mpptNo(5)`/`.2.6..2.15` — **не реализуем** (решено).

| OID (полное) | Объект MIB | Тип/множ. | Источник КЭС | Конверсия |
|---|---|---|---|---|
| `.2.N.1` | mpptPVVoltage | int, В | `VcPV` | `/100` |
| `.2.N.2` | mpptPVPower | int, Вт | `PPV` | как есть |
| `.2.N.3` | mpptPVCurrent | int, А | `IcPV` | `/100` |
| `.2.N.4` | mpptBatteryVoltage | ×0.1, В | `VBat` | `/100` → ×10 (готово ×0.1) |
| `.2.N.5` | mpptCurrentSensor1 | int, А | `IExts0` | `/100` |
| `.2.N.6` | mpptCurrentSensor2 | int, А | `IExts1` | `/100` |
| `.2.N.7` | mpptEnergy | ×0.01, кВт·ч | `PwrKW`,`PwrW` | `(PwrKW·1000+PwrW)/10` (Вт·ч/10 → ×0.01 кВт·ч) |
| `.2.N.8` | mpptInternalTemperature | int, °C | `TempInt` | `−50` (уже готово: x−50) |
| `.2.N.9` | mpptBatteryTemperature | int, °C | `TempBat` | `x−50` |
| `.2.N.10` | mpptMode | OCTET | `Mode` | как есть (`i`/`v`/`B`/`b`/`S`) |
| `.2.N.11` | mpptSign | OCTET | `Sign` | как есть (`+`/`-`) |
| `.2.N.12` | mpptMpp | OCTET | `MPP` | как есть (`R`/`L`/`0`) |
| `.2.N.13` | mpptFw | OCTET | `FW` | `major="v&0x0F", minor="v>>5"` |
| `.2.N.14` | mpptRelay1 | OCTET "on/off" | `RelayC` | `bit0` |
| `.2.N.15` | mpptRelay2 | OCTET "on/off" | `RelayC` | `bit1` |
| `.2.N.16` | mpptRelay3 | OCTET "on/off" | `RelayC` | `bit2` |
| `.2.N.17` | mpptWindspeed | int | `Windspeed` | как есть (`0xFFFF`→65535) |

> `KesData` уже хранит `VcPV`/`VBat`/`TempInt` в масштабе ×100/×10/°C — конверсия
> к синтаксису MIB (целое/×0.1/×0.01) нужна **явно**, см. колонку «Конверсия».
> **Масштаб токов КЭС (`VcPV`/`IcPV`/`VBat`/`ICh`/`IExts*`) = ×100 (А = сырое/100) —
> подтверждён на железе (.60, прочитан снимок `kes-state`)**. Сходится по мощности:
> `PPV ≈ VcPV·IcPV ≈ VBat·ICh`.
> `N` здесь — индекс КЭС в `kes-list` (см. вводный абзац); «общие» OID `.2.6..2.15`
> (через `mpptNo`) **не реализуем**.

> **Trap'ы КЭС** — OID из §0.3 (`notifications 29..47`), в тело trap кладётся
> `mpptNo` + соответствующий объект (напр. `trapMpptPVPower` → `mpptNo`,`mpptPVPower`).
> Ок-варианты — где есть (§0.3, таблица).

### 2.8. Замечания по маппингу

- **Знак мощности сети** (`inputPower`): `PNET_Sign_P=0` → отдача в сеть (-), как в
  `map.js:150`. MIB-тип `Integer32 (0..20000)` беззнаковый. **Решение: отдаём по
  модулю** (|P|), знак теряется. **Вынужденное ограничение** — фиксируется как
  дефект/потеря информации: отдача в сеть и потребление из сети неразличимы по
  SNMP-значению `inputPower` (направление видно по `inputCurrent`, знак которого
  тоже ограничен типом `FloatValue`; фактически определить направление по SNMP
  нельзя). Зарегистрировать в известных ограничениях при реализации.
- **Реле**: у МАП 2–3 реле (по модели), у КЭС — 3. Маппинг битов `Status_RELEdop`/
  `RelayC` — как в `docs/events.md` §6.
- **Отсутствие устройства**: значение → `noSuch`/«н/д», не ошибка агента
  (`docs/events.md` §2, §6).

### 2.9. Периодическая отправка трапов («Мониторинг»)

Отдельная функция (помимо trap'ов движка событий §2.1): **периодическая** отправка
SNMP-трапа на заданный приёмник — заведена как настройка в «Системных настройках».

#### Настройка (UI)

- **Раздел**: «Системные настройки» — рамка **«Мониторинг»** (`system/monitoring`).
- **Поля**:
  - **Включить периодическую отправку** — checkbox/переключатель, по умолчанию **выключено**;
  - **IP приёмника** — текстовое поле;
  - **Периодичность** — числовое поле (в секундах): **от 30 с до 1 часа** (30…3600).
- **Порт/community** — **фиксированные** `UDP 162` / `public`, в UI **не выводятся**
  (§2.10 №6), от настроек агента не зависят.
- **Валидация**: не давать **сохранить** при невалидных значениях:
  - IP — валидный **IPv4** (только IPv4, §2.10 №3);
  - при включённой отправке `IP` и `периодичность` — обязательны;
  - периодичность — целое **30…3600** с (§2.10 №4).
- **Аудит** изменения настройки — как у других настроек системы
  (`system_settings` event, `handleSystemLogging` — `internal/web/handlers.go`).
- **Локализация**: новые строки рамки/полей — ключи `sys.*` во **всех** локалях
  (ru/be/zh/en/ka/kk/uz/tg, паритет обязателен по правилам проекта), напр.
  `sys.monitoring`, `sys.monitoring_enable`, `sys.monitoring_ip`,
  `sys.monitoring_interval`, `sys.monitoring_hint`, `sys.err.monitoring_*`,
  `sys.msg.monitoring_ok`.

#### Хранение/применение

- Персистентность — по образцу `sysctl`-настроек или флаг-файла
  (`/malina/etc/malina/…`, как `map-time-sync.off` — `server.go:123`).
- Применение **без пересборки**: чтение настройки при каждом тике таймера
  (или перечитывание при изменении), реконфиг на лету.
- **Отключено** (по умолчанию) — таймер не создаётся / не тикает → фоновой работы нет
  (§1.6).

#### Логика отправки

- **Stateless** (§1.6): каждый тик — читаем актуальный снимок МАП/КЭС из shm,
  отправляем trap (значения по маппингу §2.6/§2.7), **без накопления состояния между
  тиками** (см. §2.10). Периодичность даёт только сам факт повторной отправки.
- За тик — **несколько** v2c-trap'ов, **по одному на каждый объект** по его
  notification- OID из §0.3 (МАП + КЭС), значения по маппингу §2.6/§2.7
  (§2.10 №2). Объекты **без** notification-OID в периодическую отправку **не входят**
  (доступны только через SNMP-агент GET).
- При **отсутствии устройства** — **не слать** (§2.10 №5).
- Ошибки отправки — в отдельный лог (§2.2 п.9).

### 2.10. Решения (периодическая отправка)

| # | Вопрос | Решение |
|---|---|---|
| 1 | ~~Какой OID периодического трапа~~ | **снят** — отдельного «дежурного» OID нет; каждый трап использует свой notification-OID из §0.3 (см. №2) |
| 2 | Полный набор или дежурный «жив»-трап | **несколько трапов за тик** — по одному на каждый объект по его notification-OID из §0.3 (МАП + КЭС). Объекты **без** notification-OID (`inputFrequency`, `outputFrequency`, `statusModeText`, `statusRelay1/2`, `statusCooler`, `info.*`, `batteryMonitorCurrentCapacity/TTG/Status`) в периодическую отправку **не входят** — только через SNMP-агент GET |
| 3 | IP: IPv4 / hostname / IPv6 | **только IPv4** |
| 4 | Верхний предел периодичности | **1 раз в час** (3600 c) — максимум; минимум 30 c |
| 5 | Отправлять ли при отсутствии устройства | **не слать** (молчать до появления устройства) |
| 6 | Порт/community | **фиксированные** `UDP 162` / `public` (не зависят от настроек агента §1.6; в UI не выводятся) |

> **Stateless подтверждается:** периодическая отправка НЕ вводит состояние процесса —
> каждый тик читает актуальный снимок из shm и отправляет несколько трапов (§2.10 №2);
> никаких счётчиков/истории/накопления между тиками нет (см. §1.6). Периодичность даёт
> только сам факт повторной отправки.

---

## 3. Общие положения

- **Источник значений** — наши демоны/снимки телеметрии (map-reader/kes-reader shm),
  без legacy-API (`docs/events.md` §10, §11).
- **Локализация** для UI (если появится настройка SNMP на странице) — ru/be/zh/en/ka/kk/uz/tg.
- **Безопасность**: v2c `public` передаёт данные в открытом виде — для локальной сети
  приемлемо; при необходимости — ограничить агента списком адресов или перейти на v3.
- SNMP-агент и SNMP-trap — **независимые** функции, но реализуются **в одном процессе**
  (`malina-web`) на общем маппинге §2.6/§2.7.
- Внешний пакет `snmpd` **не используется** — агент встраивается в web (Go).
- Проект **не входит** в деплой по умолчанию — обе функции включаются «при необходимости».

## 4. Статус

| Задача | Статус |
|---|---|
| MICROART-MIB (файл, структура, OID) | **перенесён** — `docs/snmp/MICROART-MIB.txt`; разбор в §0 |
| Решения по открытым вопросам | **приняты** — §1.4 (агент), §2.5 (trap) |
| Маппинг MIB → МАП | **составлен** — §2.6 |
| Маппинг MIB → КЭС | **составлен** — §2.7 |
| SNMP-trap (события, МАП) | **реализовано** — `web/internal/snmp` + `internal/web/snmptrap.go` (v2c, порт 162, community `public`, множитель по MIB, Ок-трапы при наличии Ok-нотификации) |
| SNMP-trap (события, КЭС) | маппинг §2.7 готов; события КЭС — заглушки (источник не реализован), trapping в общем блоке готов |
| SNMP-агент (в `malina-web`) | **реализовано** — `web/internal/snmp` (свой BER/ASN.1, UDP 161, ленивый GET/GETNEXT/WALK, `noSuchInstance`/`endOfMibView`); встроен в `malina-web`, общий маппинг с trap; порт <1024 (web от root, привилегии не ограничиваются) |
| Ограничение «1 МАП на хост» | **принято** — первый МАП в списке; §1.4.7, §2.4 |
| Несколько КЭС (`mpptN`) | **принято** — индексы КЭС в `kes-list` 1..4 (на .60 — три КЭС); >4 — первые 4; §2.7 |
| Общий путь `mpptNo(5)`/`.2.6..2.15` | **не реализуем** — только фиксированные `mpptN`; §0.4, §2.7 |
| Масштаб токов КЭС | **подтверждён на железе** (.60) — ×100; §2.7 |
| Stateless-слушатель + ленивая активация | **реализовано** — §1.6 (опрос shm только при запросе) |
| Знак мощности сети (`inputPower`) | **по модулю** — вынужденное ограничение (§2.8) |
| Ok-трапы покрывают не все `param` | **принято** — без Ok-нотификации Ok не шлём (§2.5 п.5, §2.6) |
| Периодическая отправка трапов («Мониторинг») | **реализовано** — §2.9: настройка в «Системных настройках» (включить/IP/периодичность 30…3600 с, порт 162/public фикс.), цикл `runMonitoring`, несколько трапов за тик по notification-OID; по умолчанию выключено |

Реализация в `malina-web`:
- `web/internal/snmp/` — BER/ASN.1-код, сообщения v2c, агент (UDP-слушатель), отправка trap.
- `web/internal/web/snmpmapping.go` — маппинг MIB → значения из shm МАП/КЭС (общий для агента и trap).
- `web/internal/web/snmptrap.go` — событийные trap (фронт + Ок) и периодическая отправка.
- `web/internal/web/snmpmonitoring.go` — настройка «Мониторинг» и цикл периодической отправки.
- `system/systemd/malina-web.service` — web от root, привилегии не ограничиваются (порт <1024 + системные операции).

Флаг/настройки: SNMP-агент (`-snmp-port`, `-snmp-community`) и периодические трапы
(`/malina/etc/malina/snmp-monitoring.json`) — включаются по необходимости.