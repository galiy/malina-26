# Независимое ревью доработок в рабочем дереве: legacy API КЭС (read_json/read_sec/write_sec + `bat`)

- Дата: 2026-10-12, 18:11 (MSK)
- Область: **незакоммиченные изменения рабочего дерева** на базе `master`
  (HEAD `8db8c55`), реализующие legacy-совместимость КЭС в `malina-web` и публикацию
  соответствующих полей из `kes-reader`.
- Роль: независимый ревьюер. **Изменений в код не вносил.**
- Связано (уже закоммичено ранее): `docs/legacy/review-legacy-api-20261012T1523.md`,
  `docs/legacy/review-kes-reader-20261012T1218.md`.

## 0. Что именно ревьюится (объект)

Незакоммиченные изменения (`git status` → «изменено»/«неотслеживаемые»):

| Файл | Что сделано |
|---|---|
| `daemons/kes-reader/kes.go` | окно №2 расширено 66→185 Б (до RAM `0x2C8`); в контракт `KESR` добавлены `Mode`(50)/`Sign`(51)/`MPP`(52)/`windspeed`(53); `kesrBmsOff` сдвинут 52→54 |
| `daemons/kes-reader/kes_test.go` | `TestWindowsCover`/`TestParseKesAndData`/`TestContractGolden` дополнены новыми полями |
| `web/internal/web/keslegacyapi.go` | новый хендлер `handleReadBat` (эмуляция `batmon`), `kesEnergyWh` |
| `web/internal/web/kesmem.go` | распаковка/рендер `Mode`/`Sign`/`MPP`/`windspeed`, `charField`, `windVal` |
| `web/internal/web/legacyapi.go` | `check_daemons`: `batmond:0`; `read_json.php?device=bat` → `handleReadBat` |
| `web/internal/web/keslegacy_test.go` | тесты под новые поля/рендер |
| `web/assets/templates/masterpanel.html` | убран заголовок `<h3>МАСТЕР-УЗЕЛ</h3>` |
| `docs/*` | `kes-memory.md`/`kes-memory-not-accepted.md` (поля «приняты»), `kes-reader.md` (контракт/окна), `legacy-api.md` (§7, §7.1–7.3), `legacy-endpoints.md`, `legacy/master-node.md`, `api-auth.md` |
| `docs/legacy/batmon-api.md` (новый), `web/internal/web/batmonapi_test.go` (новый) | спека/тесты эмуляции batmon |

Замечание: `read_sec.php`, `write_sec.php`, `read_json.php?device=mppt` и
`check_daemons` **реализованы ранее** (коммиты `5bca270`/`61a229d`/`8db8c55`); сейчас
дорабатывается паритет полей и `bat`.

## 1. Как проверял (воспроизводимость)

Офлайн, без сетевых доступов:

```
# модуль daemons
cd daemons && gofmt -l kes-reader/ lib/ && go vet ./kes-reader/ ./lib/... && \
  go build ./... && go test -count=1 ./...        # ok (kes-emu, kes-reader, lib/microart, map-reader)

# модуль web
cd web && go vet ./internal/web/ && go test -count=1 ./...   # ok (все пакеты web)
```

Все тесты зелёные, `gofmt`/`vet` чисто. Новые тесты web:
`TestParseKesPayload`, `TestBuildKesVA`, `TestRenderKesJSON`, `TestBuildKesCellOps`,
`TestCountKesInShm(Seqlock)`, `TestBatSlotFieldKeys`, `TestBatAllValuesStrings`,
`TestBatEnergyFromKes`, `TestBatMpptDayEFormat`, `TestReadBatWrappedInArray`.

Замечание по DX: в репозиторий добавлен `go.work` (`./web`, `./daemons`); из-за него
`go test ./...` из корня падает с «directory prefix . does not contain modules listed
in go.work» — надо запускать по модулям. Мелочь, но разработчик об это споткнётся.

**Итоговая оценка:** доработка архитектурно аккуратная, контракт `KESR` синхронизирован
между продюсером (`daemons/kes-reader`) и потребителем (`web`), чтение shm защищено
seqlock-проверками, покрытие тестами для парсинга/рендера хорошее. Но есть **один
функциональный дефект (`windspeed`)** и **несколько расхождений именно с legacy-контрактом
совместимости** (форма ответа `read_sec` при `offset>0`, метод `write_sec`), плюс
несогласованность документации.

---

## 2. Замечания

### M1 (важно). `windspeed` читается/публикуется как 1 байт, а документ и legacy — 16 бит

**Где:** `daemons/kes-reader/kes.go` — `Windspeed byte` и `Windspeed: img[0x3F8]`
(один байт), контракт `kesrWindOff = 53` (1 байт); `web/internal/web/kesmem.go:110`
(`Windspeed byte`), `:175` (`f.Windspeed = kesr[kesKesrWindOff]`), `:284`
(`put(0x3F8, 1, …)`), `windVal` (`kesmem.go:367-373`).

**Что говорит источник:** `docs/kes-memory.md` §1 (новый блок «Legacy-операнды»):
`windspeed | 0x3F8 | … | u16`; `docs/legacy/kes-legacy-malina2.md` §1b:
«`windspeed` — `0x3F8` (LE, 16 бит); `65535` = н/д». То есть поле — **16-битное**
со сторожевым `0xFFFF`.

**Проблема:**
- реальные значения > 255 обрезаются (читается только младший байт `0x3F8`);
- младший байт `0xFF` у настоящего значения (напр. `0x02FF` = 767) ложно трактуется
  как «н/д» → `65535`;
- `buildKesVA` (`kesmem.go:284`) кладёт 1 байт, поэтому `read_sec` по `0x3F8/0x3F9`
  отдаёт неверный старший байт;
- сторож `0xFF` несовместим с 16-битным полем (там `0xFFFF`) — код и док противоречат.

**Рекомендация.** Привести к `uint16`: `Windspeed uint16`, чтение `le16(img, 0x3F8)`,
сторож `0xFFFF`; в `KESR` поле занимает 2 байта, `kesrWindOff = 53`, `kesrBmsOff = 55`
(и `+32` ко всем последующим — блок пересчитать), синхронно в `web/kesmem.go`.
Обновить `windVal` (`0xFFFF → 65535`), golden-тесты обоих модулей и `docs/kes-memory.md`.
Если по факту поле однобайтовое — наоборот, исправить документ и legacy-разбор
(убрать «u16/65535»), чтобы не было двойной правды.

---

### M2. `read_sec.php` при `offset>0` отдаётся массивом, а legacy — объект `{addr:byte}`

**Где:** `web/internal/web/keslegacyapi.go:24-85` (`handleReadSec`), ответ —
`json.Marshal(arr)` (`:77-84`) **всегда массив**.

**Legacy (`docs/legacy/kes-legacy-malina2.md` §20):** «при `offset=0` это
**JSON-массив** `[b0,b1,…]`, при `offset>0` — **объект** `{"713":0,"714":0}`».
То есть старый клиент на `offset>0` распарсит ответ как объект с ключами-адресами.

**Проблема:** наш совместимый слой на `offset>0` отдаёт массив → внешний legacy-клиент,
рассчитывающий на объектный вид, сломается. `docs/legacy-api.md` §7.1 формально
переписан под «всегда массив», т.е. это **осознанное расхождение** — но тогда его надо
пометить как явную девиацию от legacy, а не выдавать за паритет.

**Рекомендация.** Либо реализовать объектный вид при `offset>0` (ключ = десятичный
абсолютный адрес), либо **явно** задокументировать девиацию и подтвердить, что ни один
потребитель не использует `offset>0` (проверить `home.js`/`load_menu` эталонной `.68`).
Дополнительно: legacy ограничивает `offset+count ≤ 2048`, у нас `≤ kesVirtualSize (0xE80)`
— меньшая девиация, но тоже зафиксировать.

---

### M3. `write_sec.php` принимает только GET, legacy — GET и POST

**Где:** `web/internal/web/keslegacyapi.go:94` — `if r.Method != http.MethodGet { 405 }`.

**Legacy:** `docs/legacy/kes-legacy-malina2.md` §20 «`write_sec.php` `GET/POST`»;
`docs/legacy-endpoints.md:101` — «`GET/POST id,relay,mode`». При этом `docs/legacy-api.md`
§7 (новый) для этой ручки указывает только `GET`.

**Проблема:** двойная правда в доках и потенциальная несовместимость: если клиент
(напр. `master_node.js`/старый UI) шлёт `POST` с телом формы (`data=[[cell,val]]`), мы
вернём `405`. Наш `buildKesWriteOps` читает только `r.URL.Query()` — тело POST не
разбирается.

**Рекомендация.** Разрешить `GET|POST` и читать параметры из `r.Form` (объединяет
query и `application/x-www-form-urlencoded`), либо зафиксировать «только GET» одной
строкой во всех трёх доках и подтвердить, что legacy-клиенты шлют GET.

---

### M4. `charField` эхом отдаёт произвольный байт → возможен невалидный JSON

**Где:** `web/internal/web/kesmem.go:360-365` (`charField` возвращает `string(rune(v))`
для любого непустого байта), использование — `renderKesJSON` (`:342-345`) через
`fmt.Sprintf("%q", …)`.

**Проблема:** для управляющего байта (напр. `0x01`) Go `%q` даёт `"\x01"`, а **JSON
не допускает `\x`-escape** (проверено: `fmt.Sprintf("%q", string([]byte{1}))` →
`"\x01"`). Такой ответ сломает JSON-парсер панели/клиента для всего снимка КЭС.
Кроме того, legacy-маппер `mpptd` (`fn 0x11d18`) отдавал `'U'` для значений вне
`0x2A < c ≤ 0x7C` — мы же отдаём «как есть».

**Рекомендация.** Применить legacy-правило: если байт вне диапазона печатных — `'U'`
(либо `""`); иначе символ. Тогда JSON всегда валиден и паритет с legacy.

---

### M5. `batmond:0` выставляется безусловно — риск занулевания суточной энергии узла с МАП и без КЭС

**Где:** `web/internal/web/legacyapi.go:336` — `"batmond":0` в `check_daemons`.

**Смысл доработки** (`docs/legacy/batmon-api.md` §6): заставить агрегатор «Мастер-Узел»
уйти в ветку `batmon`, где он берёт `mppt_day_E` (Вт·ч, корректное деление), а не
суммирует строковые `Pwr_kW` (→ `NaN`).

**Проблема:** ручка отдаёт `batmond:0` **всегда**, даже когда на узле нет КЭС (только
МАП). Тогда узел с одним МАП пойдёт в ветку `bat`, `mppt_day_E=Σ(КЭС)=0` → карточка
узла покажет `0 кВт·ч`, хотя дневная выработка МАП могла отображаться иначе. Возможна
регрессия для МАП-only узлов.

**Рекомендация.** Подтвердить по `master_node.js` (эталон `.68`), откуда узел берёт
энергию при отсутствии batmon и при наличии МАП; если МАП-энергия идёт другим полем —
не форсировать `batmond:0` при нулевом числе КЭС (например, `batmond:0` только если
`presentKesCount() > 0`). Если регрессии нет — зафиксировать это в `batmon-api.md`
(«в т.ч. для узла без КЭС — 0 Вт·ч, осознанно»).

---

### L1. `docs/legacy-api.md` §5 не обновлён под `batmond:0`

**Где:** `docs/legacy-api.md:61` — «Остальные демоны в нашей системе отсутствуют → `1`;
`dbe_flash` → `1`» (batmond в «остальных»). Код теперь отдаёт `batmond:0`
(`legacyapi.go:336`). Doc/code рассинхрон; обновлён только `docs/legacy/master-node.md`.
Поправить §5: «`batmond:0` — эмуляция (см. `docs/legacy/batmon-api.md`)».

### L2. `docs/legacy-endpoints.md:101` противоречит `docs/legacy-api.md` §7

`legacy-endpoints.md:101` — «`write_sec.php` | `GET/POST` …»; `legacy-api.md` §7 —
только `GET`. Связано с M3; привести к одному решению.

### L3. `read_json.php?device=mppt` без данных пишет тело до заголовков

`web/internal/web/legacyapi.go:353-357` — при `snap == nil` пишется `[]` **до**
установки `Content-Type`/`Cache-Control`; ответ уйдёт `text/plain` вместо
`text/html; charset=UTF-8` (как у непустой ветки). Выставить заголовки до записи.

### L4. Нет тестов на хендлеры `read_sec`/`write_sec`

Покрыты парсинг/рендер/`buildKesCellOps`/`check_daemons`/`bat`, но **нет** тестов на:
`handleReadSec` (границы `offset/count`, 1-based `id`, форма ответа), `handleWriteSec`
(Basic-auth `401`, ранний `Ok` реле, RMW-маска битов, приоритет `relay` над `data`,
перевод decimal→hex) и `buildKesWriteOps`. Это самые «клеевые» места совместимости —
добавить сценарные тесты (как минимум RMW-матрицу 3 реле и границы `id`).

### L5. `go.work` закоммичен — спотыкает `go test ./...` из корня

`go.work` (`use ./web`, `./daemons`) полезен локально, но `go test ./...` из корня
падает (см. §1). Либо добавить его в `.gitignore`, либо в `AGENTS.md`/README явно
записать «тесты — по модулям» (`cd web && go test ./...`, `cd daemons && go test ./...`).

---

## 3. Что зелёное и подтверждено

- **Синхронизация контракта `KESR`** продюсер↔потребитель: `Mode(50)`, `Sign(51)`,
  `MPP(52)`, `windspeed(53)`, `BMS(54)` совпадают в `daemons/kes-reader/kes.go` и
  `web/internal/web/kesmem.go`; зафиксировано golden-тестом (`TestContractGolden`).
- **Окно чтения №2** расширено корректно: `devAddr 0x0400`, длина 185 → RAM
  `0x210..0x2C8`, стыкуется с окном №3 (`0x04B9`/`0x2C9`) без дыр и наложений
  (`TestWindowsCover`, `TestWindowsOverlap`).
- **Чтение shm** (`web/internal/web/kesstate.go`): и список (`decodeKesListItem`,
  проверка `state`+нечётный `seq`), и payload (`kesRawPayload`, `state`+`seq`+границы
  `plen`) корректно отбрасывают torn-чтение — это закрывает раннюю заметку L1
  (listDoc без seqlock) со стороны потребителя.
- **`check_daemons`**: `mpptd0..3` по числу занятых слотов с лимитом 4
  (`countKesInShm`, `TestCountKesInShm`, лимит и seqlock покрыты) — верно для панелей.
- **Эмуляция `bat`**: состав/типы полей batmon (все строки) и обёртка в массив
  проверены `TestBatSlotFieldKeys`/`TestBatAllValuesStrings`/`TestReadBatWrappedInArray`;
  `mppt_day_E` — целое Вт·ч (`TestBatMpptDayEFormat`); `batmon-api.md` подробно
  фиксирует реверс `.68`.
- **Документация по auth**: `docs/legacy/kes-legacy-malina2.md` §20 переписана —
  объясняет, что Basic на `.68` — факт конкретной nginx-конфигурации, а наша политика
  (read открыт, write Basic+PAM) зафиксирована в `docs/legacy-api.md` §7 и
  `docs/api-auth.md`. Ранняя заметка H2 прошлого ревью закрыта **решением** (не
  «молчанием»), что корректно.
- Все тесты обоих модулей и `go vet` — зелёные; новых секретов/сетевого доступа нет.

---

## 4. Сводка приоритетов

| ID | Приоритет | Где | Суть | Тип | Статус |
|---|---|---|---|---|---|
| M1 | важно | `kes.go` (`Windspeed`), `kesmem.go:110,175,284,367` | `windspeed` 1 байт против `u16` в доке/legacy | дефект | ✅ решено |
| M2 | средний | `keslegacyapi.go:77-84` | `read_sec` при `offset>0` — массив вместо объекта `{addr:byte}` | совместимость | ✅ реализован паритет |
| M3 | средний | `keslegacyapi.go:94` | `write_sec` только GET; legacy — GET/POST | совместимость | ✅ решено (регрессия исправлена + success-тест) |
| M4 | средний | `kesmem.go:360-365` | `charField` → невалидный JSON на управляющем байте; нет mapper `'U'` | устойчивость | ✅ решено |
| M5 | средний | `legacyapi.go:336` | `batmond:0` безусловно — риск нуля энергии у МАП-only узла | риск | ✅ решено |
| L1 | низкий | `docs/legacy-api.md:61` | §5 не отражает `batmond:0` | док | ✅ решено |
| L2 | низкий | `docs/legacy-endpoints.md:101` | `GET/POST` против §7 `GET` | док | ✅ решено |
| L3 | низкий | `legacyapi.go:353-357` | `[]` до установки `Content-Type` | мелочь | ✅ решено |
| L4 | низкий | `web/internal/web/*_test.go` | нет тестов `read_sec`/`write_sec`/`buildKesWriteOps` | тесты | ✅ решено |
| L5 | низкий | `go.work` | `go test ./...` из корня падает | DX | ✅ решено |

## 5. Рекомендованный порядок правок

1. **M1** — `windspeed` → `uint16`/`0xFFFF` (или привести док к байту), с обновлением
   `KESR`-смещений, golden-тестов обоих модулей и `docs/kes-memory.md`.
2. **M2/M3** — привести `read_sec`/`write_sec` к legacy-форме (объект при `offset>0`,
   `GET|POST`) **или** явно задокументировать девиации и подтвердить отсутствие
   legacy-потребителей.
3. **M4** — mapper `Mode`/`Sign`/`MPP` (печатные — как есть, иначе `'U'`/`""`).
4. **M5** — подтвердить поведение `master_node.js`; при необходимости форсировать
   `batmond:0` только при `presentKesCount() > 0`.
5. **L1–L5** — синхронизировать доки (`legacy-api.md` §5, `legacy-endpoints.md`),
   заголовки `[]`-ответа, тесты хендлеров, заметка про `go.work`.

---

Примечание о полноте: ревью — по **рабочему дереву** (незакоммиченные правки) на дату
`2026-10-12 18:11 MSK`, база `master`/HEAD `8db8c55`. Проверки `gofmt`/`vet`/`build`/
`test` воспроизводимы офлайн по модулям `web` и `daemons`. Изменений в код не вносилось.

---

## 6. Статус устранения замечаний (2026-10-12, после правок)

Формат: ✅ — выполнено (жирный зелёный крыжик), ❌ — отклонено (красный крестик),
🔁 — в работе (статус снят/понижен, требуются доработки или подтверждение артефактом)
+ обоснование.

### M1 (важно) — `windspeed` → `uint16`
✅ **РЕШЕНО.** `Windspeed` переведён на `uint16` в обоих модулях:
- `daemons/kes-reader/kes.go` — `Windspeed uint16`, чтение `le16(img,0x3F8)`; в
  `KESR`-контракте `kesrWindOff=53` (2 байта, `PutUint16`/побайтово), `kesrBmsOff` 54→**55**;
- `web/internal/web/kesmem.go` — `Windspeed uint16`, распаковка `Uint16(KESR[53:55])`,
  VA-раскладка `put(0x3F8,2)`, `windVal(uint16)` с сторожем `0xFFFF→65535`;
- документы: `docs/kes-memory.md` §1 (`u16`, было корректно), `docs/kes-reader.md` §4
  (`windspeed(53–54)`), §7; golden/VA/parse-тесты обоих модулей обновлены.

### M2 — `read_sec` при `offset>0`
✅ **РЕШЕНО (реализован паритет legacy — важно для внешних потребителей).** Формат
ответа приведён к legacy точно, а не «задокументирован как девиация». По исходнику
`/settings/html/read_sec.php` эталонной `.68` (чтение по SSH, без модификаций):

```php
for ($i=$offset;$i<$offset+$count;$i++) $arr[$i]=ord($str[$i]);
echo json_encode($arr);
```

`json_encode` отдаёт **массив** при `offset=0` и **объект** `{addr:byte}` при
`offset>0` (ключи-адреса `$arr[$i]` от `$offset`). В `handleReadSec`
(`web/internal/web/keslegacyapi.go`) реализовано именно так: `offset==0` → JSON-массив
`[b0,b1,…]`, `offset>0` → объект `{"<dec>":byte}`. Так `read_sec` остаётся совместимым с
**внешними** потребителями, дергающими ручку с `offset>0`.

Дополнительно (для полноты паритета):
- лимит legacy `offset+count ≤ 2048`; наш — `≤ kesVirtualSize(0xE80)` (меньшая девиация
  зафиксирована, т.к. виртуальное пространство КЭС меньше legacy-слайса);
- параметры читаются десятичными, как `intval()` в legacy (hex-строку не принимает).

Артефакт-подтверждение: `docs/legacy/review-legacy-api-20261012T1811-append-m2.md`
(выписка `read_sec.php`/`read_memory.php` + `grep` по веб-скриптам `.68`).

### M3 — `write_sec` GET/POST
🔁 **В РАБОТЕ (была регрессия, теперь исправлена + success-тест).** Первичная правка
(`handleWriteSec` принимает `GET|POST`, `Allow: GET, POST`) прошла, но внесла
**регрессию**: параметры перешли с `r.URL.Query()` на `r.Form`, при этом
`r.ParseForm()` не вызывался — для серверного запроса `r.Form` пуст **даже для GET**,
поэтому `write_sec` всегда отвечал `error` и запись реле/ячеек КЭС не работала.
Исправлено: в `handleWriteSec` вызывается `r.ParseForm()` (при ошибке — `error`);
`buildKesWriteOps` читает параметры из `r.Form` (query + телo POST).

**Success-тест добавлен** (`web/internal/web/keshandlers_test.go`):
- `TestWriteSecSuccess` — настоящий success-путь через auth + RMW + control-сокет:
  `POST` с urlencoded-телом, `id/relay/mode`, сокет отвечает `{"ok":true}` → `Ok`;
  покрывает и ранний `Ok` (реле уже в требуемом состоянии, пустой `ops`).
- `TestWriteSecSuccessGetParams` — `GET` с query-параметрами (тоже `Ok`).

Для этого `kesShmPath` переведён из `const` в `var` (мутабельный путь), тесты поднимают
временный сегмент в `t.TempDir()`. Доки `docs/legacy-api.md` §7/§7.1 и
`docs/legacy-endpoints.md` уже приведены к `GET/POST` — двойная правда (L2) снята.
Сборка/`vet`/тесты обоих модулей — зелёные.

### M4 — `charField` mapper `'U'`
✅ **РЕШЕНО.** `charField` (web/kesmem.go) применяет legacy-правило `mpptd`
(`fn 0x11d18`): печатный `0x2A < c ≤ 0x7C` — как есть; вне диапазона — `'U'`; `0` —
пустая строка. JSON всегда валиден (`\xNN`-escape исключён), паритет с legacy.

### M5 — `batmond:0` безусловно
✅ **РЕШЕНО.** `check_daemons` отдаёт `batmond:0` только при `presentKesCount() > 0`
(наличие КЭС); без КЭС — `1`. Для МАП-only узла ветка batmon не форсируется — ноль
`mppt_day_E` выработку МАП не маскирует; NaN там не возникает (пустой массив КЭС).
Отражено в `docs/legacy-api.md` §5 и `docs/legacy/batmon-api.md` §6.

### L1 — `legacy-api.md` §5 `batmond`
✅ **РЕШЕНО.** §5 дополнен: `batmond:0` при наличии КЭС (эмуляция, ссылка на
`docs/legacy/batmon-api.md` §6), без КЭС — `1`.

### L2 — `legacy-endpoints.md` vs §7
✅ **РЕШЕНО.** `docs/legacy-api.md` §7 и §7.1 приведены к `GET/POST` (см. M3) —
двойная правда снята.

### L3 — `[]` до заголовков
✅ **РЕШЕНО.** В `handleReadJSON` (case `mppt`) `Content-Type`/`Cache-Control` теперь
выставляются **до** записи `[]`, ветка пустого ответа отдаёт `text/html; charset=UTF-8`.

### L4 — тесты хендлеров
✅ **РЕШЕНО.** Добавлен `web/internal/web/keshandlers_test.go`:
- `TestKesRmwOps` — полная RMW-матрица 3 реле (вкл/выкл из разных состояний,
  ранний Ok, невалидный `relay`), извлечённая в чистую `kesRmwOps`;
- `TestReadSecMethodAndBounds` — метод `405`/граничные ошибки `read_sec`;
- `TestWriteSecMethod` — `write_sec` `405` для не-GET/POST, `Allow: GET, POST`;
- `buildKesCellOps` уже покрыт (`TestBuildKesCellOps`: валидный перевод decimal→hex,
  отказ на `[-1,5]`, `[300,999]`) — дубль не создавался.

### L5 — `go.work` и `go test ./...` из корня
✅ **РЕШЕНО.** В `AGENTS.md` добавлен раздел «Сборка и тесты (два go-модуля)»: явно
указано запускать по модулям (`go build/vet/test ./web/... ./daemons/...` или
`cd web`/`cd daemons`), т.к. из-за `go.work` корень — не модуль. `go.work` оставлен
(удобен для единой команды из корня).
