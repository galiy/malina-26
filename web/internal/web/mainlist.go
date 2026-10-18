package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/web/internal/mapmode"
	"github.com/galiy/malina-26/web/internal/measure"
)

// mainlist.go — API `GET /api/mainlist`: сводка по всем подключённым устройствам
// (МАП + КЭС). Сервер отдаёт только значения в единых физических единицах
// (В, А, Вт, Гц, °C, %) и флаг «нет данных» (nd) — БЕЗ цветов и предокругления.
// Раскраска, округление и перевод в другие единицы — на клиенте (home.js).
// Источники — loadMapList()/mapRawPayload()+decodeMapFields
// и loadKesList()/kesRawPayload()+parseKesPayload (см. docs/mainpage.md).
//
// Формулы значений сверены с референсом panel-1: web/assets/static/map.js.

// mainOnlineWindow — порог «устройство онлайн» по LastSeen (2 минуты).
const mainOnlineWindow = 2 * time.Minute

// mainValue — значение ячейки плитки в единых физ. единицах (сырое, без
// округления; раскраску и округление делает клиент).
type mainValue struct {
	V  float64 `json:"v"`  // значение (В, А, Вт, Гц, °C, %), без округления
	ND bool    `json:"nd"` // true = нет данных → показывать «—»
}

// mainBlock — блок из 5 значений (столбец плитки).
type mainBlock struct {
	U mainValue `json:"u"` // напряжение
	F mainValue `json:"f"` // частота (для МАП «Сеть»/«Нагрузка»)
	I mainValue `json:"i"` // ток
	P mainValue `json:"p"` // мощность
	T mainValue `json:"t"` // температура
}

// mainMapTile — плитка МАП.
type mainMapTile struct {
	ID      int       `json:"id"`
	Gen     uint64    `json:"gen"`      // поколение слота (для ссылки /map?id&gen)
	Title   string    `json:"title"`    // строка устройства (§5.1)
	Mode    string    `json:"mode"`     // текст режима (§5.2)
	ModeNum uint32    `json:"mode_num"` // композитный код 0..18
	Soc     *float64  `json:"soc"`      // % (nullable)
	Grid    mainBlock `json:"grid"`     // колонка «Сеть»
	Batt    mainBlock `json:"batt"`     // колонка «АКБ»
	Load    mainBlock `json:"load"`     // колонка «Нагрузка»
	Online  bool      `json:"online"`   // LastSeen свежий
}

// mainKesTile — плитка КЭС.
type mainKesTile struct {
	ID     int       `json:"id"`
	Gen    uint64    `json:"gen"`   // поколение слота (для ссылки /kes?id&gen)
	Title  string    `json:"title"` // строка устройства (§5.1)
	Mode   string    `json:"mode"`  // строка режима (§5.4)
	Soc    *float64  `json:"soc"`   // % (nullable)
	Panel  mainBlock `json:"panel"` // колонка «Панели» (U/I/P/T, без F)
	Batt   mainBlock `json:"batt"`  // колонка «АКБ» (U/I/P/T, без F)
	Online bool      `json:"online"`
}

// mainlistResp — тело ответа /api/mainlist.
type mainlistResp struct {
	Updated int64         `json:"updated"`
	Maps    []mainMapTile `json:"maps"`
	Kes     []mainKesTile `json:"kes"`
	Bms     []mainBmsTile `json:"bms"`
}

// handleAPIMainList — сводка по всем устройствам. Под авторизацией (cookie || Bearer).
func (s *Server) handleAPIMainList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	tr := s.bundle.Translations(s.lang(r))
	resp := mainlistResp{Updated: time.Now().Unix(), Maps: []mainMapTile{}, Kes: []mainKesTile{}, Bms: []mainBmsTile{}}

	for _, m := range loadMapList().Maps {
		payload := mapRawPayload(m.ID)
		if payload == nil {
			continue // слот не активен — пропуск
		}
		f := decodeMapFields(payload)
		if f == nil {
			continue // битый снимок — пропуск
		}
		resp.Maps = append(resp.Maps, buildMapTile(tr, m, f))
	}

	for _, k := range loadKesList().Kes {
		payload := kesRawPayload(k.ID)
		if payload == nil {
			continue
		}
		kf, ok := parseKesPayload(payload)
		if !ok {
			continue
		}
		resp.Kes = append(resp.Kes, buildKesTile(tr, k, kf))
	}

	for _, b := range loadBmsList().Bms {
		payload := bmsRawPayload(b.ID, b.Gen)
		if payload == nil {
			continue
		}
		tel, err := bmscontract.Decode(payload)
		if err != nil {
			continue
		}
		resp.Bms = append(resp.Bms, buildBmsTile(tr, b, tel))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// modeText — текст режима МАП из перевода (локализованное «как на панели 1»).
// Для кода 9 — префикс + десятичное и двоичное StatusCh.
func modeText(tr map[string]string, code, statusCh uint32) string {
	if code == 9 {
		return tr["p1.mode9_prefix"] + " " +
			strconv.FormatUint(uint64(statusCh), 10) + " (" +
			strconv.FormatUint(uint64(statusCh), 2) + ")"
	}
	key := "p1.mode" + strconv.FormatUint(uint64(code), 10)
	if m, ok := tr[key]; ok {
		return m
	}
	return "?"
}

// buildMapTile собирает плитку МАП из записи списка и распакованного снимка.
func buildMapTile(tr map[string]string, m MapInfo, f map[string]uint32) mainMapTile {
	tile := mainMapTile{
		ID:     m.ID,
		Gen:    m.Gen,
		Title:  m.MenuLabel(tr),
		Online: time.Since(time.Unix(m.LastSeen, 0)) <= mainOnlineWindow,
	}

	code := mapmode.Mode(func(nm string) uint32 { return f[nm] })
	tile.ModeNum = code
	tile.Mode = modeText(tr, code, f["StatusCh"])

	// --- Напряжение сети/нагрузки ---
	uinV, uinOK := voltage100From(f, "UNET")
	uoutV, uoutOK := voltage100From(f, "UOUTmed")

	// --- Ток сети (знак по PNET_Sign_P) ---
	iin, iinOK := gridCurrent(f)

	// --- Мощность сети: знак уже в токе ---
	pnet, havePnet := 0.0, false
	if uinOK && iinOK {
		pnet = uinV * iin
		havePnet = true
	}

	// --- АКБ (МАП): знак по MODE_RAM == 4 ---
	uacc, uaccOK := batteryVoltage(f)
	iacc, iaccOK := batteryCurrent(f)
	var pbat float64
	havePbat := uaccOK && iaccOK
	if havePbat {
		pbat = uacc * iacc
	}
	if soc, ok := batSocFromFields(f); ok {
		soc = clampSoc(soc)
		tile.Soc = &soc
	}

	// --- Нагрузка: Pload = Pnet + Pbat (в новых знаках) ---
	pout, havePout := 0.0, false
	if havePnet && havePbat {
		pout = measure.LoadPower(pnet, pbat)
		havePout = true
	}

	// ===== Колонка «Сеть» =====
	if uinOK {
		tile.Grid.U = mv(uinV)
	} else {
		tile.Grid.U = mvND()
	}
	if iinOK {
		tile.Grid.I = mv(iin)
	} else {
		tile.Grid.I = mvND()
	}
	if fv, ok := freqFrom(f["TFNET"]); ok {
		tile.Grid.F = mv(fv)
	} else {
		tile.Grid.F = mvND()
	}
	if havePnet {
		tile.Grid.P = mv(pnet)
	} else {
		tile.Grid.P = mvND()
	}
	ttor, ttorOK := tempFrom(f, "Temp_Grad1")
	if ttorOK {
		tile.Grid.T = mv(ttor)
	} else {
		tile.Grid.T = mvND()
	}

	// ===== Колонка «АКБ» =====
	if uaccOK {
		tile.Batt.U = mv(uacc)
	} else {
		tile.Batt.U = mvND()
	}
	if iaccOK {
		tile.Batt.I = mv(iacc)
	} else {
		tile.Batt.I = mvND()
	}
	if havePbat {
		tile.Batt.P = mv(pbat)
	} else {
		tile.Batt.P = mvND()
	}
	if v, ok := tempFrom(f, "Temp_Grad0"); ok {
		tile.Batt.T = mv(v)
	} else {
		tile.Batt.T = mvND()
	}

	// ===== Колонка «Нагрузка» =====
	if uoutOK {
		tile.Load.U = mv(uoutV)
	} else {
		tile.Load.U = mvND()
	}
	if fv, ok := freqFrom(f["ThFMAP"]); ok {
		tile.Load.F = mv(fv)
	} else {
		tile.Load.F = mvND()
	}
	if havePout {
		if iout, ok := measure.LoadCurrent(pout, uoutV); ok && uoutOK {
			tile.Load.I = mv(iout)
		} else {
			tile.Load.I = mvND()
		}
		tile.Load.P = mv(pout)
	} else {
		tile.Load.I = mvND()
		tile.Load.P = mvND()
	}
	if v, ok := tempFrom(f, "Temp_Grad2"); ok {
		tile.Load.T = mv(v)
	} else {
		tile.Load.T = mvND()
	}

	return tile
}

// gridCurrent — ток сети со знаком: INET_16_4 / (INET>=16 ? 4 : 16),
// знак по PNET_Sign_P (1 — потребление, 0 — отдача). Без флага — нет данных.
func gridCurrent(f map[string]uint32) (float64, bool) {
	in16, ok := f["INET_16_4"]
	if !ok {
		return 0, false
	}
	sign, okSign := f["PNET_Sign_P"]
	if !okSign {
		return 0, false
	}
	inet, okInet := f["INET"]
	if !okInet { // без INET неизвестен делитель (4 или 16)
		return 0, false
	}
	div := 16.0
	if inet >= 16 {
		div = 4
	}
	return measure.GridCurrentSigned(float64(in16)/div, sign), true
}

// batteryVoltage — измеренное напряжение АКБ МАП (В), приоритет точного источника.
func batteryVoltage(f map[string]uint32) (float64, bool) {
	v, ok := uaccFromFields(f)
	if ok {
		return v, true
	}
	if vh, ok1 := f["UAcc_med_VH"]; ok1 {
		if vl, ok2 := f["UAcc_med_VL"]; ok2 {
			x := float64(vh*256+vl) / 10
			if x > 0 {
				return x, true
			}
		}
	}
	return 0, false
}

// batteryCurrent — ток АКБ МАП (А): заряд (MODE_RAM==4) «−», разряд «+».
// Без MODE_RAM — нет данных.
func batteryCurrent(f map[string]uint32) (float64, bool) {
	il, ok1 := f["IAcc_med_A_u16_L"]
	ih, ok2 := f["IAcc_med_A_u16_H"]
	mode, okMode := f["MODE_RAM"]
	if !ok1 || !ok2 || !okMode {
		return 0, false
	}
	cur := float64(il+ih*256) / 16
	return measure.BatteryCurrentSigned(cur, mode), true
}

// voltage100From — напряжение вида «сырое + 100» (UNET/UOUTmed): 0 → 0.
func voltage100From(f map[string]uint32, key string) (float64, bool) {
	raw, ok := f[key]
	if !ok {
		return 0, false
	}
	if raw == 0 {
		return 0, true
	}
	return float64(raw) + 100, true
}

// freqFrom — частота 6250/x при x!=0.
func freqFrom(raw uint32) (float64, bool) {
	if raw == 0 {
		return 0, false
	}
	return 6250 / float64(raw), true
}

// tempFrom — температурный датчик «сырое − 50». Присутствие поля обязательно
// (иначе отсутствие датчика не отличить от нулевого значения).
func tempFrom(f map[string]uint32, key string) (float64, bool) {
	raw, ok := f[key]
	if !ok {
		return 0, false
	}
	return float64(raw) - 50, true
}

// kesBatCap — ёмкость АКБ КЭС (Ач): настройка Bat_Cap (индекс 33 в kf.Settings,
// адрес 0x244). 0/недоступно → поправка на ток не применяется.
func kesBatCap(kf *kesFields) float64 {
	if kf == nil || len(kf.Settings) < 34 {
		return 0
	}
	return float64(kf.Settings[33])
}

// kesModeString — строка режима КЭС «как на железе»: Mode + Sign + MPP (без
// разделителя, см. docs/mainpage.md §5.4). Пример 'B' + '-' + 'L' → "B-L".
func kesModeString(f *kesFields) string {
	return charField(f.Mode) + charField(f.Sign) + charField(f.MPP)
}

// buildKesTile собирает плитку КЭС из записи списка и распакованных полей слота.
func buildKesTile(tr map[string]string, k KesInfo, kf *kesFields) mainKesTile {
	tile := mainKesTile{
		ID:     k.ID,
		Gen:    k.Gen,
		Title:  k.MenuLabel(tr), // единый источник подписи (КЭС+серийник+порт+скорость)
		Mode:   kesModeString(kf),
		Online: time.Since(time.Unix(k.LastSeen, 0)) <= mainOnlineWindow,
	}

	// SOC КЭС — всегда LiFePO4 (тип 4), КЭС — зарядник (Charging=true).
	// Nil при отсутствии данных (ModelVolt=0 или VBat=0), как у МАП (§5.3.1).
	nomV := float64(kf.ModelVolt) * 12
	if nomV > 0 && kf.VBat > 0 {
		in := batInput{
			AccType:    accTypeLiyp39,
			NominalV:   nomV,
			Voltage:    float64(kf.VBat) / 100,
			Current:    float64(kf.ICh) / 100,
			Charging:   true,
			CapacityAh: kesBatCap(kf),
		}
		soc := clampSoc(socEstimated(in))
		tile.Soc = &soc
	}

	// --- Панели ---
	tile.Panel.U = mv(float64(kf.VcPV) / 100)
	tile.Panel.I = mv(float64(kf.IcPV) / 100)
	tile.Panel.P = mv(float64(kf.PPV))
	tp := float64(kf.TempInt) - 50
	tile.Panel.T = mv(tp)

	// --- АКБ (КЭС) ---
	tile.Batt.U = mv(float64(kf.VBat) / 100)
	tile.Batt.I = mv(float64(kf.ICh) / 100)
	tile.Batt.P = mv(float64(kf.PCurr))
	tb := float64(kf.TempBat) - 50
	tile.Batt.T = mv(tb)

	return tile
}

// mv инициализирует mainValue с измеренным значением (сырое, без округления).
func mv(v float64) mainValue {
	return mainValue{V: v}
}

// mvND инициализирует mainValue «нет данных».
func mvND() mainValue {
	return mainValue{ND: true}
}
