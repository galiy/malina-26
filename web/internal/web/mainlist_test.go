package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/web/internal/i18n"
)

// testTr — минимальный перевод для modeText в юнит-тестах (без шм/бандла).
func testTr() map[string]string {
	return map[string]string{
		"p1.mode9_prefix": "МАП включен. Трансляция + заряд. Режим",
		"p1.mode3":        "МАП включен и транслирует сеть",
		"p1.mode4":        "МАП включен. Трансляция + заряд. Трансляция",
	}
}

// newMainServer — сервер с загруженным бандлом переводов (для /api/mainlist,
// который переводит режимы на языке клиента).
func newMainServer(t *testing.T) *Server {
	t.Helper()
	b, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "assets")), "i18n", "ru", []string{"ru"})
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	return &Server{bundle: b, opts: Options{DefaultLang: "en"}}
}

// TestMainListEmpty — при пустых списках /api/mainlist отдаёт maps=[] и kes=[].
func TestMainListEmpty(t *testing.T) {
	prev := kesShmPath
	kesShmPath = filepath.Join(t.TempDir(), "empty-kes")
	_ = os.WriteFile(kesShmPath, []byte("not a shm segment"), 0o644)
	t.Cleanup(func() { kesShmPath = prev })

	s := newMainServer(t)
	rec := httptest.NewRecorder()
	s.handleAPIMainList(rec, authedReq(http.MethodGet, "/api/mainlist", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, ожидалось 200", rec.Code)
	}
	var resp mainlistResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON: %v (%q)", err, rec.Body.String())
	}
	if resp.Maps == nil || resp.Kes == nil {
		t.Fatalf("ожидались не-nil массивы (не null): maps=%v kes=%v", resp.Maps, resp.Kes)
	}
	if len(resp.Kes) != 0 {
		t.Fatalf("kes=%d, ожидалось 0 (пустой сегмент)", len(resp.Kes))
	}
	if resp.Updated == 0 {
		t.Fatal("updated должен быть ненулевым")
	}
}

// TestMainListAuth — без сессии → 401.
func TestMainListAuth(t *testing.T) {
	s := newMainServer(t)
	rec := httptest.NewRecorder()
	s.handleAPIMainList(rec, httptest.NewRequest(http.MethodGet, "/api/mainlist", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, ожидалось 401", rec.Code)
	}
}

// TestMainListMethod — не GET → 405.
func TestMainListMethod(t *testing.T) {
	s := newMainServer(t)
	rec := httptest.NewRecorder()
	s.handleAPIMainList(rec, authedReq(http.MethodPost, "/api/mainlist", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d, ожидалось 405", rec.Code)
	}
}

// TestMapTileFields — формулы §6.1–6.3 по фиктивному снимку МАП.
func TestMapTileFields(t *testing.T) {
	m := MapInfo{ID: 0, LastSeen: nowSec(), Model: "T", Serial: 1234, SerialLetter: "A", Port: "ttyUSB0", Baud: 115200}
	f := map[string]uint32{
		"UNET":             130, // → 230 В
		"INET":             20,  // >=16 → /4
		"INET_16_4":        80,  // → 20 А
		"PNET_Sign_P":      1,   // не 0 → знак «+»
		"TFNET":            125, // → 50 Гц
		"Temp_Grad1":       85,  // → 35°C (тор)
		"UAcc_med_VH":      2,
		"UAcc_med_VL":      3, // → 51.5 В
		"IAcc_med_A_u16_L": 10,
		"IAcc_med_A_u16_H": 0, // → 0.625 А
		"MODE":             4,
		"MODE_RAM":         4,
		"StatusCh":         0x00,
		"LCD_AccType":      5,
		"UACC":             2,
		"LCD_CAcc":         80,  // → 500 Ач
		"Temp_Grad0":       68,  // → 18°C (АКБ)
		"UOUTmed":          120, // → 220 В
		"ThFMAP":           125, // → 50 Гц
		"Temp_Grad2":       95,  // → 45°C (транзисторы)
	}
	tile := buildMapTile(testTr(), m, f)

	// Сеть — сервер отдаёт сырые v (без округления), nd=false.
	if tile.Grid.U.V != 230 || tile.Grid.U.ND {
		t.Fatalf("grid.U=%+v, ожидалось v=230, nd=false", tile.Grid.U)
	}
	if tile.Grid.I.V != 20 || tile.Grid.I.ND {
		t.Fatalf("grid.I=%+v, ожидалось v=20, nd=false", tile.Grid.I)
	}
	if tile.Grid.F.V != 50 || tile.Grid.F.ND {
		t.Fatalf("grid.F=%+v, ожидалось v=50, nd=false", tile.Grid.F)
	}
	if tile.Grid.P.V != 4600 || tile.Grid.P.ND { // 230*20
		t.Fatalf("grid.P=%+v, ожидалось v=4600, nd=false", tile.Grid.P)
	}
	if tile.Grid.T.V != 35 || tile.Grid.T.ND {
		t.Fatalf("grid.T=%+v, ожидалось v=35, nd=false", tile.Grid.T)
	}

	// АКБ — сырые значения (ток 2 знака и мощность без округления — дело клиента).
	if tile.Batt.U.V != 51.5 || tile.Batt.U.ND {
		t.Fatalf("batt.U=%+v, ожидалось v=51.5, nd=false", tile.Batt.U)
	}
	if tile.Batt.I.V != 0.625 || tile.Batt.I.ND { // +0.625 (заряд), без округления
		t.Fatalf("batt.I=%+v, ожидалось v=0.625, nd=false", tile.Batt.I)
	}
	if tile.Batt.P.V != 32.1875 || tile.Batt.P.ND { // 51.5*0.625, без округления
		t.Fatalf("batt.P=%+v, ожидалось v=32.1875, nd=false", tile.Batt.P)
	}
	if tile.Batt.T.V != 18 || tile.Batt.T.ND {
		t.Fatalf("batt.T=%+v, ожидалось v=18, nd=false", tile.Batt.T)
	}
	if tile.Soc == nil || *tile.Soc < 8 || *tile.Soc > 18 {
		t.Fatalf("soc=%v, ожидалось ~8..18 (OCV LFP)", tile.Soc)
	}

	// Нагрузка
	if tile.Load.U.V != 220 || tile.Load.U.ND {
		t.Fatalf("load.U=%+v, ожидалось v=220, nd=false", tile.Load.U)
	}
	if tile.Load.F.V != 50 || tile.Load.F.ND {
		t.Fatalf("load.F=%+v, ожидалось v=50, nd=false", tile.Load.F)
	}
	pout := 4600 - 32.1875 // pnet − pbat
	if tile.Load.P.V != pout || tile.Load.P.ND {
		t.Fatalf("load.P=%+v, ожидалось v=%v, nd=false", tile.Load.P, pout)
	}
	if tile.Load.I.V != pout/220 || tile.Load.I.ND { // pout/uout, без округления
		t.Fatalf("load.I=%+v, ожидалось v=%v, nd=false", tile.Load.I, pout/220)
	}
	if tile.Load.T.V != 45 || tile.Load.T.ND {
		t.Fatalf("load.T=%+v, ожидалось v=45, nd=false", tile.Load.T)
	}

	// Режим: MODE_RAM=4 + StatusCh=0x00 → код 4.
	if tile.ModeNum != 4 {
		t.Fatalf("mode_num=%d, ожидалось 4", tile.ModeNum)
	}
	if tile.Mode == "" {
		t.Fatal("mode не должен быть пустым")
	}
	if !tile.Online {
		t.Fatal("online должен быть true (свежий LastSeen)")
	}
}

// TestMapTileFieldsND — отсутствующее поле → nd=true + severый.
func TestMapTileFieldsND(t *testing.T) {
	m := MapInfo{ID: 1, LastSeen: nowSec()}
	tile := buildMapTile(testTr(), m, map[string]uint32{})
	if !tile.Grid.U.ND {
		t.Fatal("grid.U без UNET: ожидалось nd=true")
	}
	if !tile.Grid.F.ND {
		t.Fatal("grid.F без TFNET: ожидалось nd=true")
	}
	if !tile.Batt.U.ND {
		t.Fatal("batt.U без UAcc_med: ожидалось nd=true")
	}
	if !tile.Load.U.ND {
		t.Fatal("load.U без UOUTmed: ожидалось nd=true")
	}
	// Отсутствие температурных полей → nd (не «−50» из-за нулевой ячейки).
	if !tile.Grid.T.ND {
		t.Fatal("grid.T без Temp_Grad1: ожидалось nd=true")
	}
	if !tile.Batt.T.ND {
		t.Fatal("batt.T без Temp_Grad0: ожидалось nd=true")
	}
	if !tile.Load.T.ND {
		t.Fatal("load.T без Temp_Grad2: ожидалось nd=true")
	}
	// Пустой снимок → SOC нет.
	if tile.Soc != nil {
		t.Fatalf("soc=%v, ожидалось nil при пустом снимке", tile.Soc)
	}
}

// TestKesTileFields — формулы §6.4–6.5 по фиктивному КЭС.
func TestKesTileFields(t *testing.T) {
	kf := &kesFields{
		ModelVolt: 4, // → 48 В
		Serial:    2670,
		VcPV:      12580, // → 125.8 В
		IcPV:      1370,  // → 13.7 А
		VBat:      5000,  // → 50.0 В
		PPV:       1726,  // → 1726 Вт
		PCurr:     1726,  // → 1726 Вт
		ICh:       3450,  // → 34.5 А
		TempInt:   78,    // → 28°C
		TempBat:   65,    // → 15°C
		Mode:      'B',
		Sign:      '-',
		MPP:       'L',
		Settings:  make([]uint16, 40),
		LastSeen:  nowSec(),
	}
	kf.Settings[33] = 100 // Bat_Cap = 100 Ач
	k := KesInfo{ID: 0, Serial: 2670, Port: "ttyUSB1", Baud: 115200, LastSeen: nowSec()}
	tile := buildKesTile(testTr(), k, kf)

	// Панели
	if tile.Panel.U.V != 125.8 || tile.Panel.U.ND {
		t.Fatalf("panel.U=%+v, ожидалось v=125.8, nd=false", tile.Panel.U)
	}
	if tile.Panel.I.V != 13.7 || tile.Panel.I.ND {
		t.Fatalf("panel.I=%+v, ожидалось v=13.7, nd=false", tile.Panel.I)
	}
	if tile.Panel.P.V != 1726 || tile.Panel.P.ND {
		t.Fatalf("panel.P=%+v, ожидалось v=1726, nd=false", tile.Panel.P)
	}
	if tile.Panel.T.V != 28 || tile.Panel.T.ND {
		t.Fatalf("panel.T=%+v, ожидалось v=28, nd=false", tile.Panel.T)
	}

	// АКБ
	if tile.Batt.U.V != 50.0 || tile.Batt.U.ND {
		t.Fatalf("batt.U=%+v, ожидалось v=50.0, nd=false", tile.Batt.U)
	}
	if tile.Batt.I.V != 34.5 || tile.Batt.I.ND {
		t.Fatalf("batt.I=%+v, ожидалось v=34.5, nd=false", tile.Batt.I)
	}
	if tile.Batt.P.V != 1726 || tile.Batt.P.ND {
		t.Fatalf("batt.P=%+v, ожидалось v=1726, nd=false", tile.Batt.P)
	}
	if tile.Batt.T.V != 15 || tile.Batt.T.ND {
		t.Fatalf("batt.T=%+v, ожидалось v=15, nd=false", tile.Batt.T)
	}

	// Режим B-L и SOC LiFePO4 (48В = 16S): 50.0/16 ≈ 3.125 В → низкий SoC.
	if tile.Mode != "B-L" {
		t.Fatalf("mode=%q, ожидалось B-L", tile.Mode)
	}
	if tile.Soc == nil || *tile.Soc < 0 || *tile.Soc > 15 {
		t.Fatalf("soc=%v, ожидалось 0..15 (50В/16S LiFePO4)", tile.Soc)
	}
	if !tile.Online {
		t.Fatal("online должен быть true")
	}
	// Отсутствие данных (VBat=0 или ModelVolt=0) → Soc=nil, как у МАП (ниты 4–5).
	// Копия ЗНАЧЕНИЯ (не указателя), чтобы не мутировать общий kf.
	z := *kf
	z.VBat = 0
	if got := buildKesTile(testTr(), k, &z); got.Soc != nil {
		t.Fatalf("soc=%v при VBat=0, ожидался nil", got.Soc)
	}
	z = *kf
	z.ModelVolt = 0
	if got := buildKesTile(testTr(), k, &z); got.Soc != nil {
		t.Fatalf("soc=%v при ModelVolt=0, ожидался nil", got.Soc)
	}
}

// TestSocShared — единая socEstimated: одинаковые входы → одинаковый результат
// и клампинг в 0..100.
func TestSocShared(t *testing.T) {
	in := batInput{
		AccType:    accTypeLiyp39,
		NominalV:   48,
		Voltage:    51.5,
		Current:    0.625,
		Charging:   true,
		CapacityAh: 500,
	}
	a := socEstimated(in)
	b := socEstimated(in)
	if a != b {
		t.Fatalf("разные результаты для одинаковых входов: %v vs %v", a, b)
	}
	if a < 0 || a > 100 {
		t.Fatalf("soc=%v вне 0..100", a)
	}
	// Клампинг: очень низкое напряжение → 0; очень высокое → 100.
	if socEstimated(batInput{AccType: accTypeLiyp39, NominalV: 48, Voltage: 1}) != 0 {
		t.Fatal("низкое напряжение: ожидался 0")
	}
	if socEstimated(batInput{AccType: accTypeLiyp39, NominalV: 48, Voltage: 1000}) != 100 {
		t.Fatal("высокое напряжение: ожидался 100")
	}
}

// TestModeMap — композитный код режима по снимку (зеркало mapmode.Mode).
func TestModeMap(t *testing.T) {
	// MODE_RAM=4 + StatusCh=0x04 → 7 (заряд 2 ступень).
	f := map[string]uint32{"MODE_RAM": 4, "StatusCh": 0x04}
	tile := buildMapTile(testTr(), MapInfo{LastSeen: nowSec()}, f)
	if tile.ModeNum != 7 {
		t.Fatalf("mode_num=%d, ожидалось 7", tile.ModeNum)
	}
	// MODE_RAM=3, транслирует сеть → 3.
	tile = buildMapTile(testTr(), MapInfo{LastSeen: nowSec()}, map[string]uint32{"MODE_RAM": 3})
	if tile.ModeNum != 3 {
		t.Fatalf("mode_num=%d, ожидалось 3", tile.ModeNum)
	}
	// Код 9: префикс + StatusCh dec/bin.
	tile = buildMapTile(testTr(), MapInfo{LastSeen: nowSec()},
		map[string]uint32{"MODE_RAM": 6, "StatusCh": 0x10})
	if tile.ModeNum != 9 {
		t.Fatalf("mode_num=%d, ожидалось 9", tile.ModeNum)
	}
	if tile.Mode != "МАП включен. Трансляция + заряд. Режим 16 (10000)" {
		t.Fatalf("mode=%q", tile.Mode)
	}
}

// TestMapTilePnetSignAbsent — отсутствие PNET_Sign_P НЕ инвертирует знак Pсети
// (как map.js: инверсия только при наличии поля, равном 0).
func TestMapTilePnetSignAbsent(t *testing.T) {
	f := map[string]uint32{"UNET": 130, "INET": 20, "INET_16_4": 80} // нет PNET_Sign_P
	tile := buildMapTile(testTr(), MapInfo{LastSeen: nowSec()}, f)
	if tile.Grid.P.V != 4600 {
		t.Fatalf("grid.P=%v, ожидалось 4600 (без инверсии)", tile.Grid.P.V)
	}
}

// nowSec — текущее время Unix (свежий LastSeen для online=true).
func nowSec() int64 {
	return time.Now().Unix()
}
