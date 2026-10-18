# Главная страница: сводка по устройствам (МАП + КЭС + BMS + сетевые инверторы)

> Статус: **реализовано**. Серверный эндпоинт `GET /api/mainlist`
> (`web/internal/web/mainlist.go`), SOC-ядро `socEstimated` (`ocv.go`),
> клиент (`web/assets/templates/home.html`, `web/assets/static/home.js`,
> CSS в `app.css`), локали ru/be/zh/en/ka/kk/uz/tg и юнит-тесты `mainlist_test.go`
> (без обращения к shm). Документ самодостаточен для изменения/регресса.

---

## 1. Цель

Пустой `home.html` (главная `/`) заменить на живую сводку по всем подключённым
устройствам двух типов — **МАП** и **КЭС**.

**Ключевые решения (обоснование):**

- Агрегация и **производные физ. величины** (P = U·I и т.п.) — **на сервере**.
  Клиент НЕ тащит бинарные контракты (`/api/map/raw`, `/api/kes/raw`) и считает
  только презентацию: цветовые пороги (§8) и десятичное округление. Это убирает
  историческое дублирование логики (панель 1 считает сама в `map.js`).
- Новый эндпоинт **`GET /api/mainlist`** — отдаёт сырые значения в единых физ.
  единицах (`v`) + флаг «нет данных» (`nd`); раскраску и округление делает клиент
  (`home.js`), сервер цветов **не** присылает.
- Период опроса клиента — **1 секунда** (§9.1 п.6).
- Разделы показываются только при наличии устройств.
- Ответ `/api/mainlist` содержит блоки `maps`, `kes`, **`bms`** и **`grid`**.
  Плитки `grid` — отдельные инверторы, сгруппированные по размещению, без сумм
  по группе; обновление раз в 5 с, остальная главная — раз в 1 с; плитка ведёт
  на `/grid?id=<serial>&gen=<gen>` (`docs/grid-reader-ui.md`).
- Плитки BMS:
  `type` — локализованная метка типа, `mode` — локализованный канонический режим,
  `soc`, `pack` (U/I/P/T), `online`); источник — `loadBmsList()` + `bmsRawPayload()` +
  `bmscontract.Decode`, см. `bms.go`.

---

## 2. Где что лежит (карта файлов)

### Сервер (Go, модуль `web`)

| Файл | Что здесь |
|---|---|
| `web/internal/web/kesapi.go` | `KesInfo`, `MenuLabel(tr)`, `findKes` |
| `web/internal/web/kesstate.go` | `KesList`, `loadKesList()`, `kesRawPayload(id)` |
| `web/internal/web/kesmem.go` | `parseKesPayload(payload) (*kesFields, bool)`, поля КЭС, `charField` |
| `web/internal/web/maps.go` | `handleMapPage`, `handleAPIMapDevices` |
| `web/internal/web/mapstate.go` | `MapInfo`, `MenuLabel(tr)`, `modelName`, `loadMapList()`, `mapRawPayload(id)` |
| `web/internal/web/mapfields.go` | `mapFieldNames` (id→имя поля снимка МАП) |
| `web/internal/web/events_provider.go:114` | `decodeMapFields(payload) map[string]uint32` |
| `web/internal/web/ocv.go` | `batSocFromFields(f)`, OCV-кривые, типы АКБ — **базовая SOC-функция** |
| `web/internal/web/mapmode` (пакет) | `Mode(reg func(string) uint32) uint32` — композитный режим МАП |
| `web/internal/web/legacy.go` | `handleReadJSON` (панель 1 legacy-снимок) |
| `web/internal/web/mode_text.go` | `modeLabelStatic` / `modeTextDynamic` |
| `web/internal/web/bmsstate.go` | `BmsInfo`, `loadBmsList()`, `bmsRawPayload(id, gen)` |
| `web/internal/web/bmsapi.go` | `handleAPIBmsDevices` (`bms_type_label`), `handleAPIBmsRaw` |
| `web/internal/web/bms.go` | `bmsTypeLabel`/`bmsModeLabel`, `mainBmsTile`, `buildBmsTile`; страницы `/bms/*` |
| `web/internal/web/server.go` | регистрация маршрутов (`mux.HandleFunc`) |
| `web/internal/web/handlers.go` | `handleIndex` (сейчас пустая главная), `baseData`, `render`, `tr` |

### Клиент (JS + шаблоны)

| Файл | Что здесь |
|---|---|
| `web/assets/templates/home.html` | главная — живая сводка (разделы `#homeMaps`/`#homeKes`, подключает `home.js`) |
| `web/assets/templates/layout.html` | общий каркас, `{{.Active}}`, `{{.T}}`, подключаемые скрипты |
| `web/assets/static/map.js` | панель 1 — **референс формул** (НЕ трогаем) |
| `web/assets/static/masterpanel.js` | мастер-панель — **референс отрисовки плиток/статуса** |
| `web/assets/static/nav.js` | динамическое меню **«МАП» и «КЭС»** (списки `/api/*/devices`, ссылки на страницу устройства) |
| `web/assets/i18n/{ru,be,zh,en,ka,kk,uz,tg}.json` | локали |

---

## 3. API `GET /api/mainlist`

### 3.1 Маршрут

В `web/internal/web/server.go` в блок `mux.HandleFunc` (рядом с `/api/map/*`,
`/api/kes/*`, строки ~312–317) добавить:

```go
mux.HandleFunc("/api/mainlist", s.handleAPIMainList)
```

### 3.2 Обработчик

Файл: `web/internal/web/mainlist.go` (новый). Сигнатура: `func (s *Server) handleAPIMainList(w http.ResponseWriter, r *http.Request)`.

- Авторизация: `if _, ok := s.currentUser(r); !ok { w.WriteHeader(http.StatusUnauthorized); return }` — **как** у `handleAPIMapDevices` (`maps.go:94`) и `handleAPIKesDevices` (`kesapi.go:24`).
- Метод: только `GET` (иначе `405` + `Allow: GET`) — как в `maps.go`.
- Заголовки: `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store`.
- Тело: `json.NewEncoder(w).Encode(resp)`.
- Ошибка чтения shm одного устройства **не должна ронять весь ответ** — такая
  плитка пропускается (см. §3.4).

### 3.3 Формат ответа

```json
{
  "updated": 1720000001,
  "maps": [ /* 0..N плиток МАП, порядок — как в loadMapList() */ ],
  "kes":  [ /* 0..M плиток КЭС, порядок — как в loadKesList() */ ],
  "bms":  [ /* 0..K плиток БМС, порядок — как в loadBmsList() */ ]
}
```

- `updated` — `time.Now().Unix()` (момент сборки).
- `maps` / `kes` / `bms` — **всегда** массивы (могут быть `[]`). `null` не отдаём.
  Инициализация: `resp := mainlistResp{Maps: []mainMapTile{}, Kes: []mainKesTile{}, Bms: []mainBmsTile{}}`
  (`mainlist.go`).
- `bms` — плитки BMS (`mainBmsTile`): `id`, `gen`, `title` (логический ID/BASE),
  `serial` (ID = BASE; может совпадать у нескольких BMS), `mode` (режим), `soc`, `pack` (U/I/P/T), `online`; источник — `loadBmsList()` +
  `bmsRawPayload()` + `bmscontract.Decode` (`bms.go`).

### 3.4 Логика сборки (сервер, псевдокод)

```
ml := loadMapList()
kl := loadKesList()
for each m in ml.Maps:
    payload := mapRawPayload(m.ID)
    if payload == nil:  continue            // слот не активен — пропуск
    f := decodeMapFields(payload)
    if f == nil:        continue            // битый снимок — пропуск
    maps = append(maps, buildMapTile(tr, m, f))
for each k in kl.Kes:
    payload := kesRawPayload(k.ID)
    if payload == nil:  continue
    kf, ok := parseKesPayload(payload)
    if !ok:             continue
    kes = append(kes, buildKesTile(k, kf))
```

- **Если раздел пуст** после фильтрации — в ответе `[]`, клиент раздел не рисует.
- Решение «пропустить слот» vs «плитка нет-данных»: принято **пропуск** (в shm
  неактивный слот и так не попадёт в список). Раздел скрывается при недоступности
  (см. §9 п.7).

---

## 4. Структуры данных (Go)

### 4.1 Плитка МАП

```go
type mainValue struct {
    V  float64 `json:"v"`  // значение в единых физ. единицах (В, А, Вт, Гц, °C, %), сырое
    ND bool    `json:"nd"` // true = нет данных → показывать «—»
}
```

> Контракт намеренно минимален: сервер отдаёт только числовое значение и флаг
> нет-данных. Раскраска (поле `color`) и предокругление удалены — клиент сам
> решает их по §8 (`home.js: fieldColor`, `fmt`). Для производных значений (напр.
> `P = U·I`) сервер отдаёт итог в `v`; клиент производные величины НЕ считает.

type mainBlock struct {          // блок из 5 значений (столбец плитки)
    U  mainValue `json:"u"`  // напряжение
    F  mainValue `json:"f"`  // частота (для МАП «Сеть»/«Нагрузка»)
    I  mainValue `json:"i"`  // ток
    P  mainValue `json:"p"`  // мощность
    T  mainValue `json:"t"`  // температура
}

type mainMapTile struct {
    ID      int       `json:"id"`
    Gen     uint64    `json:"gen"`     // поколение слота (для ссылки на /map?id&gen)
    Title   string    `json:"title"`   // строка устройства (см. §5.1)
    Mode    string    `json:"mode"`    // текст режима (см. §5.2)
    ModeNum uint32    `json:"mode_num"`// композитный код 0..18
    Soc     *float64  `json:"soc"`     // % (nullable)
    Grid    mainBlock `json:"grid"`    // колонка «Сеть»
    Batt    mainBlock `json:"batt"`    // колонка «АКБ»
    Load    mainBlock `json:"load"`    // колонка «Нагрузка»
    Online  bool      `json:"online"`  // LastSeen свежий
}
```

> `mainBlock` для МАП использует `U/F/I/P/T`; для нагрузки поле `F` — частота
> нагрузки. Для АКБ поля `F` не заполнено (0 + ND=true), для нагрузки — все пять.

### 4.2 Плитка КЭС

```go
type mainKesTile struct {
    ID     int       `json:"id"`
    Gen    uint64    `json:"gen"`     // поколение слота (для ссылки на /kes?id&gen)
    Title  string    `json:"title"`   // строка устройства (см. §5.1)
    Mode   string    `json:"mode"`    // строка режима (см. §5.4)
    Soc    *float64  `json:"soc"`     // % (nullable)
    Panel  mainBlock `json:"panel"`   // колонка «Панели» (U/I/P/T, без F)
    Batt   mainBlock `json:"batt"`    // колонка «АКБ» (U/I/P/T, без F)
    Online bool      `json:"online"`
}

type mainlistResp struct {
    Updated int64         `json:"updated"`
    Maps    []mainMapTile `json:"maps"`
    Kes     []mainKesTile `json:"kes"`
    Bms     []mainBmsTile `json:"bms"`
}
```

> `mainBmsTile` (см. `bms.go`): `id`, `gen`, `title` (логический ID/BASE),
> `serial` (ID = BASE; может совпадать у нескольких BMS), `mode` (локализованный режим), `soc *float64`, `pack mainBlock`
> (U/I/P/T), `online`.

> `Soc` у обеих плиток — `*float64` (nullable). У КЭС `Soc=nil` при отсутствии
> данных (`ModelVolt=0` или `VBat=0`) — единое поведение с МАП (см. §5.3.1).
> `Gen` добавлен в обе плитки для кликабельных ссылок на страницу устройства
> (`/map?id&gen`, `/kes?id&gen`) — как пункты меню; см. §10.2.

> **Замечание к §4.3 (актуализировано в `mainpage-review-202610122329.md` Б1):**
> `colorRange`, `round1`, `roundSign` из ранней версии **удалены** — раскраска и
> округление уехали на клиент (`home.js`). В коде остались только:

```go
func mv(v float64) mainValue            // измеренное значение (сырое v)
func mvND() mainValue                   // нет данных (nd=true)
```

(+ формулы §6 в `buildMapTile`/`buildKesTile`; они производные значения считают
на сервере, а десятичное округление — на клиенте.)

---

## 5. Строки и заголовки (общие источники)

### 5.1 Строка устройства — заголовок плитки

Тот же источник, что и пункт меню:

- **МАП** — `m.MenuLabel(tr)` (`web/internal/web/mapstate.go:58`):
  `Модель · серийник · порт · скорость`. Имя модели берётся из перевода `tr`
  (ключ `model.<буква>`: `T`→«Титанатор»/Titanator, `D`→«Доминатор»/Dominator,
  `H`→«Гибрид»/Hybrid, `P`→Pro; fallback — русский `modelNames`); серийник =
  `SerialLabel()` = «число» (буква серийника в подписях не используется).
  `tr` — переводы языка страницы (`s.trFor(r)`
  / `s.bundle.Translations(s.lang(r))`).
- **КЭС** — `k.MenuLabel(tr)` (`web/internal/web/kesapi.go:160`):
  `КЭС · серийник · порт · скорость`, где «КЭС» — ключ `kes.dev` из перевода
  (fallback — «КЭС»).

**Реализация:** `tile.Title = m.MenuLabel(tr)` / `k.MenuLabel(tr)`, где `tr` —
переводы языка клиента (локаль страницы).

### 5.2 Строка статуса МАП (как на панели 1)

- **Код режима** — `mapmode.Mode(func(field string) uint32 { return f[field] })`
  (пакет `web/internal/mapmode`, файл `web/internal/mapmode/mapmode.go`).
  `f` — распакованный снимок (`map[string]uint32`). Добавить в `mainlist.go` он
  уже импортирован как `mapmode` (проверить имя пакета при импорте).
- **Текст** — **не** дублировать в Go таблицу. Взять из локали, как `mode_text.go`:
  - `code != 9` → `s.tr(lang, "p1.mode"+<code>)`.
  - `code == 9` → `s.tr(lang, "p1.mode9_prefix") + " " + <StatusCh dec> + " (" + <StatusCh bin> + ")"`.
  - `StatusCh` — `f["StatusCh"]` (для МАП).
- Язык для `mainlist` — **`s.lang(r)`** (кука `lang`, см. `handlers.go:lang`):
  сервер отдаёт готовые тексты `Mode`/`Title` на языке клиента. Вариант «отдать
  код + ключ, собирает клиент» — отвергнут (дублирует логику). `MenuLabel(tr)` /
  `mapModelFull(tr, code)` переводятся по языку страницы (см. §5.1).

> Конкретика для сборщика: функция `modeText(tr map[string]string, code, statusCh uint32) string`
> (`mainlist.go:117`); при отсутствии ключа `p1.mode<code>` возвращает `"?"`.

### 5.3 SOC — единая серверная функция (общая для МАП и КЭС)

**Вынести в общую функцию.** Сейчас аналог — `batSocFromFields(f map[string]uint32) (float64, bool)`
(`web/internal/web/ocv.go:119`). Она завязана на имена полей **МАП**
(`LCD_AccType`, `UACC`, `IAcc_med_A_u16_L/H`, `LCD_CAcc`, `UAcc_med_VH/VL`).

**Рефакторинг** — выделить из `batSocFromFields` чистое ядро, параметризованное
входными данными, не зависящее от имён полей МАП:

```go
type batInput struct {
    AccType    int     // тип АКБ (0..5), см. ocv.go accType*
    NominalV   float64 // номинальное напряжение сборки, В
    Voltage    float64 // измеренное напряжение АКБ, В
    Current    float64 // ток, А (знак: «+» заряд)
    Charging   bool    // сейчас заряд
    CapacityAh float64 // ёмкость АКБ, Ач (0 → без поправки на ток)
}

// socEstimated — единая функция оценки SoC (перенос логики ocv.go:119..).
func socEstimated(in batInput) float64
```

- `batInput.Voltage` уже **компенсированного** (OCV-подобного) источника — эту
  компенсацию (`uaccFromFields`) делают вызывающие по своим полям.
- `batInput` для **МАП** строится из полей снимка (текущая логика `ocv.go`).
- `batInput` для **КЭС** строится из полей КЭС (см. §5.3.1).

**Кривые/константы** — остаются в `ocv.go`: `liFePO4OCV`, `leadOCV12`, `gelOCV12`,
`uaccNominalVolts`, `accType*`, `ocvKLiCell`, `ocvKLead12`, `clampSoc`,
`socFromOCV`, `liCellsForNom`.

> Не трогать публичное поведение `batSocFromFields` — обернуть в функцию-фасад,
> которая строит `batInput` по полям МАП и вызывает `socEstimated`. Существующие
> юнит-тесты `ocv_test.go`/`batmonapi_test.go` должны остаться зелёными.

#### 5.3.1 Входные данные SOC для КЭС — РЕШЕНО

**Принято:** SOC КЭС считается **всегда как LiFePO4** (тип 4/5), по нашей кастомной
кривой `liFePO4OCV` (`ocv.go`). Число ячеек — из номинала сборки LFP:
**12В=4S, 24В=8S, 48В=16S, 96В=32S** (`liCellsForNom`, `ocv.go:171`). Тип АКБ НЕ
берётся из настроек КЭС; заряд/разряд — КЭС всегда зарядник, поэтому `Charging=true`.

| Агрегат SOC | Источник (КЭС) | Значение |
|---|---|---|
| Тип АКБ | — | `accTypeLiyp39` / `accTypeLiyp37` (LiFePO4, `ocv.go`) |
| Номинал, В | `f.ModelVolt * 12` (`kesmem.go` kese `ModelVolt`) | `4`→`48 В`; далее `liCellsForNom` → ячейки |
| Напряжение | `f.VBat / 100` (В) | делить на число ячеек → на ячейку |
| Ёмкость | `f.Settings[33]` = `Bat_Cap` (`0x244`), Ач | >0 → токовая поправка OCV |
| Ток | `f.ICh / 100` (А) | оплата по `ocvKLiCell` |
| Заряд/разряд | — | `Charging = true` (КЭС — зарядник) |

**Реализация:**

```go
// построение batInput для КЭС (LiFePO4, всегда заряд)
nomV := float64(kf.ModelVolt) * 12          // В (4 → 48)
in := batInput{
    AccType:    accTypeLiyp39,               // LiFePO4 (ocv.go)
    NominalV:   nomV,
    Voltage:    float64(kf.VBat) / 100,      // В
    Current:    float64(kf.ICh) / 100,       // А
    Charging:   true,                        // КЭС — зарядник
    CapacityAh: float64(kesBatCap(kf)),      // Ач (настройка Bat_Cap, 0 → без поправки)
}
soc := socEstimated(in)
```

- `kesBatCap(kf)` — чтение настройки КЭС `Bat_Cap` (индекс 33 в `kf.Settings`,
    см. `kesSettingsAddr`/`docs/kes-contract.md` §KESS).
  - Кривая/константы — общие с МАП (`liFePO4OCV`, `liCellsForNom`, `ocvKLiCell`,
    `clampSoc`, `socFromOCV`).
  - **`nil` при отсутствии данных**: если `ModelVolt==0` или `VBat==0` — `Soc=nil`
    (не `0 %`), как у МАП. Это единое поведение (§4.2, ниты 4–5 ревью).

> Напряжение на ячейку = `(VBat/100) / ns`, `ns = liCellsForNom(nomV)` — та же
> логика, что ветка «литий» в `batSocFromFields` (`ocv.go:143..165`), но источник
> напряжения КЭС — `VBat` (без uaccFromFields-компенсации; у КЭС другие поля).

### 5.4 Строка режима КЭС (как на legacy-панели) — РЕШЕНО

Состоит из трёх опубликованных символов `Mode` + `Sign` + `MPP` (поля `kesFields`
`Mode`, `Sign`, `MPP`; см. `docs/kes-memory.md` §1 legacy-операнды). Конкатенация
трёх символов без разделителя. Пример `B-L`: `Mode='B'` + `Sign='-'` + `MPP='L'`.

- `Mode` — `i`/`v`/`B`/`b`/`S` (режим заряда, `0x2C2`).
- `Sign` — `+`/`-` (знак тока датчика, `0x37A`).
- `MPP` — `R`/`L`/`0` (точка МРР, `0x379`).

Символы уже доступны через `charField` (`kesmem.go:360`) — печатный символ
`0x2A < c ≤ 0x7C` отдаётся как есть, вне диапазона — `'U'`, `0` — пустая строка.
Символы приходят как `byte` из `kesFields`, валидные ASCII.

**Реализация:**

```go
func kesModeString(f *kesFields) string {
    mode := charField(f.Mode)   // 'B'
    sign := charField(f.Sign)   // '-'
    mpp  := charField(f.MPP)    // 'L'
    return mode + sign + mpp    // "B-L"
}
```

> Система текстов-режимов КЭС (`i`/`v`/`B`/`b`/`S`) — **не** `p1.modeN` (это МАП),
> поэтому без i18n-перевода: строка уже «как на железе», локаль не нужна.

---

## 6. Формулы значений (сервер)

Все значения — из поля снимка МАП (`f` = `decodeMapFields`, см. таблицу соответствий
`docs/legacy/legacy_read_json_map.md` §1) и полей КЭС (`kf` = `parseKesPayload`).

### 6.1 МАП — «Сеть»

| Показатель | Формула | Откуда |
|---|---|---|
| U сети | `unet=` f["UNET"]; `unet==0 ? 0 : unet+100` | `map.js:117` |
| F сети | `f["TFNET"] ? 6250/f["TFNET"] : ND` | `map.js:139` |
| I сети | `in16=f["INET_16_4"]; inet=f["INET"]; x=in16/(inet>=16?4:16)`; знак: `f["PNET_Sign_P"]==0 → −x` | `map.js` (`compute`) |
| P сети | `Uin * Iin` (знак уже в токе) | `map.js` (`compute`) |
| T тора | `f["Temp_Grad1"] - 50` | `map.js:137` (`Ttor`) |

> Если `PNET_Sign_P` отсутствует (исторические `map v1/v2`), ток и мощность
> сети — `ND`: направление не угадывается.

> **Маппинг TempGrad** (проверено `map.js:136..138`):
> `Temp_Grad0 = Tacc` (АКБ), `Temp_Grad1 = Ttor` (тор), `Temp_Grad2 = Ttr`
> (транзисторы). В колонку «Сеть» берём **тор** (`Temp_Grad1`), в «Нагрузку» —
> **транзисторы** (`Temp_Grad2`). Расцветка обеих — единая (<30/<50/<70), см. §8.2.

### 6.2 МАП — «АКБ»

| Показатель | Формула | Откуда |
|---|---|---|
| U АКБ | `(f["UAcc_med_VH"]*256+f["UAcc_med_VL"])/10` | `map.js:122` |
| SOC | §5.3 | `ocv.go` |
| I АКБ | `il=f["IAcc_med_A_u16_L"], ih=f["IAcc_med_A_u16_H"]; x=(il+ih*256)/16; MODE_RAM==4 (заряд) → −x, иначе +x` | `map.js` (`compute`) |
| P АКБ | `Uacc * Iacc` | `map.js` (`compute`) |
| T АКБ | `f["Temp_Grad0"] - 50` | `map.js:136` (`Tacc`) |

### 6.3 МАП — «Нагрузка»

| Показатель | Формула | Откуда |
|---|---|---|
| U нагрузки | `f["UOUTmed"]==0 ? 0 : f["UOUTmed"]+100` | `map.js:118` |
| F нагрузки | `f["ThFMAP"] ? 6250/f["ThFMAP"] : ND` | `map.js:140` |
| P нагрузки | `Pnet + Pbat` (в новых знаках) | `map.js` (`compute`) |
| I нагрузки | `Uout ? Pout/Uout : ND` (≤1 знак) | `map.js` (`compute`) |
| T (транзисторы) | `f["Temp_Grad2"] - 50` | `map.js:138` (`Ttr`) |

Порядок вычисления Pnet/Pbat — строго как `map.js` **до** использования Pout/Iout.

### 6.4 КЭС — «Панели»

| Показатель | Формула | Поле (`kesFields`) |
|---|---|---|
| U панелей | `kf.VcPV / 100` | `VcPV` |
| I панелей | `kf.IcPV / 100` | `IcPV` |
| P панелей | `kf.PPV` (Вт) | `PPV` |
| T внутренняя | `kf.TempInt - 50` | `TempInt` |

### 6.5 КЭС — «АКБ»

| Показатель | Формула | Поле (`kesFields`) |
|---|---|---|
| U АКБ | `kf.VBat / 100` | `VBat` |
| Ток | `kf.ICh / 100` (ток заряда АКБ; направление не кодируем — бежевый) | `ICh` |
| P | `kf.PCurr` (Вт, мощность заряда) | `PCurr` |
| T АКБ | `kf.TempBat - 50` | `TempBat` |

---

## 7. Колонки плитки и раскраска (итоговая сводка)

> **Замечание:** ниже описан итоговый вид. Значения приходят сырыми в `mainValue.v`;
> цвет каждой ячейки на клиенте выбирает `home.js: fieldColor(kind, field, v, soc)`
> (пороги — §8.2/§8.3), округление — `home.js: fmt` (десятичные заданы в `items`
> конкретной колонки). Сервер цвет не присылает.

### 7.1 МАП — «Сеть»

1. **Напряжение** — `grid.u`, цвет по §8.2 «U сети».
2. **Частота** — `grid.f`, **1 знак** после запятой (`v=round1`), цвет §8.2 «F сети».
3. **Ток** — `grid.i`, цвет §8.2 «ток/мощность сети».
4. **Мощность** — `grid.p`, цвет §8.2 «ток/мощность сети».
5. **Температура** — `grid.t`, цвет §8.2 «Temp транзисторов/тор».

### 7.2 МАП — «АКБ»

1. **Напряжение** — `batt.u`, цвет **по SOC** (§8.3).
2. **SOC** — `batt…soc` (корень плитки `Soc`), цвет §8.3.
3. **Ток** — `batt.i`, цвет §8.2 «ток/мощность АКБ».
4. **Мощность** — `batt.p`, цвет «ток/мощность АКБ».
5. **Температура** — `batt.t`, цвет §8.2 «Temp АКБ».

### 7.3 МАП — «Нагрузка»

1. **Напряжение** — `load.u`, цвет как «U сети».
2. **Частота** — `load.f`, 1 знак, цвет как «F сети».
3. **Ток** — `load.i`, цвет «ток/мощность нагрузки».
4. **Мощность** — `load.p`, цвет «ток/мощность нагрузки».
5. **Температура** — `load.t` (транзисторы `Temp_Grad2`), цвет как «Temp транзисторов/тор» (§8.2).

### 7.4 КЭС — «Панели»

1. **Напряжение** — `panel.u`, цвет **бежевый**.
2. **Ток** — `panel.i`, бежевый.
3. **Мощность** — `panel.p`, бежевый.
4. **Температура** — `panel.t`, цвет «Temp транзисторов/тор» (§8.2).

### 7.5 КЭС — «АКБ»

1. **Напряжение** — `batt.u`, цвет по SOC (§8.3).
2. **SOC** — `batt…soc` (корень плитки `Soc`, как у МАП), цвет §8.3; `nil` → «—».
3. **Ток** — `batt.i`, бежевый.
4. **Мощность** — `batt.p`, бежевый.
5. **Температура** — `batt.t`, цвет «Temp АКБ» (§8.2).

> **Изменено (ниты 4–5 ревью `202610122329`):** SOC КЭС **отображается** в колонке
> «АКБ» (ранее считался, но не выводился); при отсутствии данных — «—» (`nil`).

---

## 8. Цвета и пороги

### 8.1 Ключи цвета (клиент мапит в CSS-класс `c-*`)

| Ключ | Смысл | Hex в `app.css` (`.home-cell-v.c-*`) |
|---|---|---|
| `green` | салатовый (норма/отдача/разряд) | `#7ee36f` |
| `red` | красный (опасно/потребление/заряд) | `#e8837f` |
| `blue` | голубой | `#5bc0de` |
| `darkblue` | синий | `#2f6ea3` |
| `orange` | оранжевый | `#e0902f` |
| `beige` | бежевый (КЭС панели/АКБ) | `#f5debc` (см. §9.1 п.5) |
| `gray` | нет данных | `#999999` |

> **Изменено (Б1 + нит 8 ревью `202610122329`):** поле `color` из `mainValue`
> удалено — клиент сам выбирает ключ по §8.2/§8.3 и ставит класс `c-<key>`.
> Палитра зафиксирована по факту `app.css`; `green` намеренно **салатовый `#7ee36f`**
> (отклонение от раннего `#63b94e` — оговорено в ревью), `red`/`orange` осветлены
> (`#e8837f`/`#e0902f`) под тёмную тему. При `nd=true` клиент рисует `gray` + «—».

### 8.2 Пороги значений (МАП/КЭС)

> Реализованы в `home.js` (`colorVolts`, `colorFreq`, `colorGridPower`,
> `colorBatCur`, `colorLoadPower`, `colorTempTrans`, `colorTempBat`, `colorSoc`).
> Сервер эти пороги не знает — клиент считает по сырому `v`.

| Показатель | Порог → цвет |
|---|---|
| U сети/нагрузки (В) | `>235`→red; `<210`→blue; иначе→green |
| F сети/нагрузки (Гц) | `>50.1`→red; `<49.9`→blue; иначе→green |
| I/P сети | знач<0 (отдача)→green; знач>0 (потребление)→red; `0`→blue |
| I/P АКБ | знач>0 (разряд)→green; знач<0 (заряд)→red; `0`→blue |
| I/P нагрузки | знач>0 (потребление из МАП)→red; знач<0 (отдача в МАП)→green; `0`→blue |
| T транзисторов/тор/панели КЭС (°C) | `<30`→blue; `<50`→green; `<70`→orange; `≥70`→red |
| T АКБ (°C) | `<0`→blue; `<15`→darkblue; `<35`→green; `<50`→orange; `≥50`→red |

> Уточнение «0 голубой»: применяется только там, где 0 — осмысленное состояние
> (нет тока/мощности). Для температуры 0 НЕ означает голубой по этой логике —
> порог «<0 голубой» отдельный. Требование из ТЗ: «Ток. 0 голубой» — это про
> ток/мощность, не про температуру.

### 8.3 Пороги SOC (%)

| SOC | Цвет |
|---|---|
| `<10` | darkblue |
| `<30` | blue |
| `<70` | green |
| `<90` | orange |
| `≥90` | red |

---

## 9. Решённые вопросы (решения зафиксированы)

1. **Ток АКБ КЭС** — `ICh / 100` (ток заряда АКБ). Направление не кодируем: по ТЗ
   колонка «АКБ» КЭС — ток/мощность **бежевые**, знаковой расцветки нет.
2. **Строка режима КЭС `B-L`** — конкатенация `Mode` + `Sign` + `MPP` (3 символа,
   без разделителя), `charField` из `kesmem.go`. Определено из полей КЭС
   (`docs/kes-memory.md` §1).
3. **Тор/транзисторы** — «Сеть» = тор (`Temp_Grad1`), «Нагрузка» = транзисторы
   (`Temp_Grad2`), обе расцветки — единые `<30/<50/<70` (§8.2 «Temp транзисторов/тор»).
4. **Тип АКБ / режим заряд-разряд КЭС для SOC** — **РЕШЕНО**: всегда **LiFePO4**
   (тип 4/5), `Charging=true` (КЭС — зарядник), номинал `ModelVolt*12`, число
   ячеек `liCellsForNom`, кривая `liFePO4OCV`. `Bat_Type`/`Bat_Cap` — только
   ёмкость для токовой поправки; тип НЕ из настроек.
5. **Единицы** — **Вт** для мощности, **А** для тока, **Гц** (1 знак) для частоты
   (как панель 1 `map.js`).
6. **Цвет «нет данных»** — `nd=true` → `gray` + `—` (без числа).
7. **Частичная недоступность устройства** — пропуск слота (§3.4): раздел
   скрывается, если после фильтрации пуст.
8. **Единицы/округление** — мощность всегда в **Вт** (целое); если появится
   отображение в кВт — 1 знак после запятой. Токи: сети и панелей — **1 знак**,
   АКБ — **2 знака**; напряжение/частота — 1 знак. Точные пороги форматирования
   §8.2.
9. **Заголовок КЭС** — `MenuLabel(tr)` («КЭС · серийник · порт · скорость»,
   префикс «КЭС» — ключ `kes.dev` перевода, fallback «КЭС»).

### 9.1 Уточнено (решения зафиксированы)

1. **Температура КЭС — `x−50` (подтверждено железе/legacy).** Легаси-панели
   (`/read_json.php?device=mppt` → `renderKesJSON`, `kesmem.go:331` `int(f.TempInt)-50`)
   отдают и показывают **корректную** температуру. Значит `TempInt`/`TempBat` в слоте
   хранятся со смещением `+50`, а в плитку берём `kf.TempInt - 50` / `kf.TempBat - 50`.
   > **Расхождение**: эмулятор `kes-emu` (`device.go:259` `byte(int8(t+0.5))`) пишет
   > °C **без** `+50` — это баг эмулятора. На реальном железе корректна конвенция
   > карты `x−50`. В тестах/эмуляции не полагаться на абсолютные значения `kes-emu` по
   > температуре (смещение не учитывает посекундно; правка эмулятора — не в этой задаче).
2. **Ток АКБ КЭС — `ICh/100`, всегда заряд (положительный).** Легаси-панель
   (`masterpanel.js:184`) суммирует `I_Ch` как есть, без знака; КЭС — зарядник.
   Знаковую расцветку не применяем (колонка «АКБ» КЭС бежевая по ТЗ).
3. **Ёмкость КЭС `Bat_Cap` — использовать напрямую как Ач** (range `0..999`,
   дефолт `100`; карта `docs/kes-memory.md` §3). Если `0` или поле недоступно —
   токовую поправку SOC не применяем (как `ah<=0` в `ocv.go`). Масштаб `×1` Ач.
4. **`online`-порог** — `LastSeen` в пределах **2 минут** от `now` (константа
   `mainOnlineWindow = 2 * time.Minute`). Иначе `online=false` (плитка рисуется
   погашенной/серой, значение-статус помечается устаревшим).
5. **Hex**: бежевый — **`#f5debc`** (тёплый, в тон жёлтого акцента темы `#fec678`),
   «нет данных» — **`#999999`**. (Оба согласованы с темой microart; при
   необходимости — поправить в `app.css`.)
6. **Период опроса** — **1 секунда** (не 10 с, как ранее). Требование эксплуатации:
   сводка должна быть близкой к реальному времени.
7. **Стабильные позиции и «туман»** — при смене порядка / потере / stale устройства
   позиции плиток НЕ меняются; потерянные и устаревшие устройства гасятся
   полупрозрачным «туманом» (см. §10.2). Потерянные плитки не удаляются, пока
   устройство не вернётся.
8. **SOC КЭС — отображается и nullable** (ниты 4–5 ревью `202610122329`): ячейка
   SOC в колонке «АКБ» КЭС (как у МАП, §7.5); `Soc=nil` при `ModelVolt=0`/`VBat=0`
   → «—» вместо `0 %`.

## 10. Клиентская часть

### 10.1 `home.html`

Заменить содержимое:

```html
{{define "content"}}
<div class="home-wrap">
  <div class="home-error flash err" id="homeErr" hidden></div>
  <section class="home-section" id="homeMaps" hidden>
    <h2>{{index .T "home.sec_map"}}</h2>
    <div class="home-tiles" id="homeMapTiles"></div>
  </section>
  <section class="home-section" id="homeKes" hidden>
    <h2>{{index .T "home.sec_kes"}}</h2>
    <div class="home-tiles" id="homeKesTiles"></div>
  </section>
  <section class="home-section" id="homeBms" hidden>
    <h2>{{index .T "home.sec_bms"}}</h2>
    <div class="home-tiles" id="homeBmsTiles"></div>
  </section>
</div>
<script>
  window.I18N = {{json .T}};
</script>
<script src="{{asset "home.js"}}"></script>
{{end}}
```

- `{{asset "home.js"}}` — новый файл `web/assets/static/home.js`.
- Заголовок страницы (`<h1>`) не выводится; разделы подписаны ключами
  `home.sec_map`/`home.sec_kes`/`home.sec_bms`. Раздел «БМС» (плитки BMS) рисует
  `home.js` по тому же принципу (одна колонка U/I/P/T, SOC на строке статуса, ссылка
  `/bms?id&gen&serial`). Весь набор ключей `home.*` — во **всех** локалях
  `web/assets/i18n/*.json` (ru/be/zh/en/ka/kk/uz/tg).

### 10.2 `home.js` — логика клиента

- `setInterval(tick, 1000)`; первый вызов сразу (`tick()`). Обновление раз в секунду.
- `fetch("api/mainlist", { cache: "no-store", credentials: "same-origin" })`.
- На `401` — перенаправить на `/login` (как панели).
- По ответу:
  - `if (resp.maps.length === 0) hide(#homeMaps) else { show; renderTiles; }`
  - `if (resp.kes.length === 0) hide(#homeKes) else { show; renderTiles; }`
  - Разделы независимы.
- **Стабильные позиции и «туман» (stale/потерянные устройства)** — ключевое
  требование эксплуатации:
  - Плитки привязаны к устройству по стабильному ключу (`map:<id>` / `kes:<id>`),
    а не к порядку в ответе. Порядок в ответе НЕ влияет на позиции уже
    созданных плиток.
  - Новая плитка создаётся и добавляется в конец своего раздела только при
    **первом** появлении устройства. При смене порядка / исчезновении /
    восстановлении устройства позиция НЕ переезжает.
  - **lost** — устройство было в реестре клиента, но отсутствует в текущем
    ответе `/api/mainlist`: плитка НЕ удаляется, остаётся на своём месте, но
    гасится «туманом» (полупрозрачная заслонка + подпись `home.status.lost`).
  - **stale** — устройство присутствует в ответе, но `online=false` (см. §9.1 п.4):
    такая плитка гасится «туманом» с подписью `home.status.stale`. Последние
    известные значения остаются видны под заслонкой.
  - Когда устройство снова `online` и присутствует — «туман» снимается, значение
    обновляется, позиция сохраняется.
  - Раздел показывается, если в нём есть хотя бы одна плитка (в т.ч. погашенная);
    скрывается только если устройства в нём никогда не было.
- `renderTile(tile)` — строит DOM-плитку: заголовок `tile.title`, строка статуса
  `tile.mode`, тонкий отчерк, колонки (МАП — Сеть/АКБ/Нагрузка; КЭС — Панели/АКБ),
  разделённые вертикальным тонким **чёрным** разделителем.
- **Колонки — таблицы выравнивания** `.home-cols-table` (без заголовка и границ:
  `border:0`, `border-collapse:collapse`): каждая колонка — 3 колонки таблицы —
  наименование (влево) | значение (вправо) | единицы (влево). Строки —
  `.home-cell-l` / `.home-cell-v-wrap` / `.home-cell-u`.
- **Плашка кликабельна** (кроме тех, что в «тумане»): обёрнута в
  `<a class="home-tile-link" href="<page>?id=<id>&gen=<gen>">`, где
  `<page>` = `map`/`kes` по типу устройства. Пункты меню «МАП»/«КЭС» ведут туда же.
  В тумане (lost/stale) плашке добавляется класс `nolink` → `pointer-events:none`
  (плюс сам «туман» перекрывает клик).
- Для каждой ячейки значение `v` + класс цвета по §8 (клиент сам выбирает ключ
  `fieldColor(...)` → `c-<key>`); при `nd` — «—» (серый).
- CSS живёт в `web/assets/static/app.css` (или инлайн в шаблоне) — классы
  `.home-tile`, `.home-card`, `.home-col`, `.home-cell`, `.home-status`.

### 10.4 Меню «ДОМОЙ» и «КЭС» (в шапке)

- **«ДОМОЙ»** — пункт меню слева от «МАП» (иконка-домик + `menu.home`, стиль — общий
  `.navlink`), ведёт на `/`.
- **«КЭС»** — дропдаун справа от «МАП`, идентичный меню «МАП»: сервер рендерит
  `.Kes` (`baseData`), `nav.js` динамически обновляет список из `/api/kes/devices`
  (тип `kes`) и строит ссылки `/kes?id&gen`. Пункты ведут на страницу конкретного
  устройства.
- Подготовка: `baseData` отдаёт `Maps` и `Kes`; `handleIndex` ставит
  `Active="home"` (подсветка «ДОМОЙ»).

### 10.3 Вёрстка (конкретика)

- **Плитка** (`.home-tile`): фон фирменный зелёный (`#0d5637` — светлая кайма контрастная),
  кайма широкая светлая (напр. `2px solid #cbd7d0` + внутренняя тонкая обводка),
  `border-radius: 12px`, скруглённые углы.
- **Колонки**: `display:flex` (или grid), между ними `border-left: 1px solid #000`
  (тонкий чёрный вертикальный разделитель).
- **Строка статуса** — под заголовком; тонкий горизонтальный отчерк `border-bottom`.

---

## 11. i18n (ключи для добавления)

В `web/assets/i18n/*.json` (ru/be/zh/en/ka/kk/uz/tg), по аналогии с существующими ключами:

| Ключ | ru (пример) |
|---|---|
| `home.sec_map` | `МАП` |
| `home.sec_kes` | `КЭС` |
| `home.sec_bms` | `БМС` |
| `home.nd` | `н/д` |
| `home.err` | `Нет связи` |
| `home.col.grid` / `.batt` / `.load` | заголовки колонок МАП (Сеть/АКБ/Нагрузка) |
| `home.col.panel` / `.batt` | заголовки колонок КЭС (Панели/АКБ) |
| `home.cell.u` / `.i` / `.p` / `.f` / `.t` / `.soc` | подписи ячеек |
| `home.status.lost` / `.stale` | подписи «тумана» (потерян/устарел) |
| `kes.title` | заголовок страницы `/kes` («КЭС s/n %s порт %s») |
| `kes.gone` / `kes.connection_lost` | сообщения страницы КЭС |
| `kes.dev` | префикс метки КЭС в меню/заголовке плитки («КЭС»); fallback — «КЭС» |

> `p1.modeN` уже есть во всех языках — используется без изменений (через `modeText`).
> Все ключи обязаны присутствовать во **всех** локалях (ru/be/zh/en/ka/kk/uz/tg).

---

## 12. Тесты

Новый `web/internal/web/mainlist_test.go` (пакет `web`, как `kesapi_test.go`):

- `TestMainListEmpty` — `loadMapList`/`loadKesList` пустые → `maps=[]`, `kes=[]`.
- `TestMapTileFields` — для фиктивного `map[string]uint32` проверить формулы
  §6.1–6.3: U/F/I/P/T, `nd` когда поле отсутствует (сырые `v`, без округления).
- `TestMapTileFieldsND` — все отсутствующие поля → `nd=true`, `Soc=nil`.
- `TestKesTileFields` — для фиктивного `*kesFields` — формулы §6.4–6.5 (+ `Soc=nil`
  при `VBat=0`/`ModelVolt=0`, ниты 4–5).
- `TestSocShared` — единая `socEstimated` (`batInput`) переиспользуется МАП/КЭС;
  для одинаковых входов даёт одинаковый результат; `Clamp` в 0..100.
- `TestModeMap` — `mapmode.Mode` по фиктивному полю → коды режима и текст
  (`modeText`, `StatusCh` dec/bin для кода 9).

> `TestColorThresholds` из ранней версии **удалён** — пороговой раскраски
> (`colorRange`) на сервере больше нет (§4.3, Б1).

Все тесты — **без обращения к shm** (чистые функции на фикстурных `f`/`kf`).

---

## 13. Пошаговый план реализации

1. Вынести SOC-ядро (`socEstimated(batInput)`) в `ocv.go` + фасады МАП/КЭС; прогон
   существующих тестов (`go test ./web/internal/web/...`).
2. Добавить `mainlist.go`: структуры, `handleAPIMainList`, `buildMapTile`,
   `buildKesTile`, `colorRange`, хелперы `mainValue`.
3. Зарегистрировать `/api/mainlist` в `server.go`.
4. Написать `mainlist_test.go` (юнит, без shm).
5. `home.html` + `home.js` + CSS + i18n-ключи (все локали).
6. Сборка/тесты: `cd web && go build ./... && go vet ./... && go test ./...`
   (из корня — `go build ./web/... ./daemons/...`).
7. При изменении референсов (если менялась логика) — обновить
   `docs/legacy/legacy_read_json_map.md` / `docs/batmon-api.md` / `docs/kes-memory.md`.

---

## 14. Связанные артефакты (правило проекта)

Код считается готовым, когда синхронно обновлены: серверный эндпоинт, тесты,
документация (`docs/mainpage.md` актуализировать «статус: реализовано»),
локали (ru/be/zh/en/ka/kk/uz/tg), `web/README.md` (упомянуть `/api/mainlist`), при изменении
контрактов — `docs/map-contract.md` / `docs/kes-contract.md`.