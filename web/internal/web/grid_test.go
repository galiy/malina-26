package web

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

func TestParseGridForm(t *testing.T) {
	form := url.Values{}
	form.Set("grid_enabled", "on")
	form.Set("grid_log_level", "debug")
	form.Set("grid_period", "20")
	form.Add("placement_name", "Дом")
	form.Add("placement_name", "Гараж")
	form.Add("name", "Deye Right")
	form.Add("ip", "192.0.2.10")
	form.Add("port", "8899")
	form.Add("type", "deye")
	form.Add("placement", "Гараж")
	form.Add("logger_sn", "123456")
	form.Add("name", "Sofar")
	form.Add("ip", "192.0.2.11")
	form.Add("port", "8899")
	form.Add("type", "sofar")
	form.Add("placement", "Дом")
	form.Add("logger_sn", "654321")
	r := httptest.NewRequest("POST", "/grid/settings", nil)
	r.Form = form
	c, msg := parseGridForm(r)
	if msg != "" {
		t.Fatal(msg)
	}
	if !c.Enabled || c.PollPeriod != 20 || len(c.Inverters) != 2 {
		t.Fatalf("config: %+v", c)
	}
	if c.Inverters[0].LoggerSN != 123456 || c.Inverters[1].Type != "sofar" {
		t.Fatalf("inverters: %+v", c.Inverters)
	}
}

func TestParseGridFormRejectsUsedPlacement(t *testing.T) {
	form := url.Values{}
	form.Add("placement_name", "Дом")
	form.Add("name", "Deye")
	form.Add("ip", "192.0.2.10")
	form.Add("port", "8899")
	form.Add("type", "deye")
	form.Add("placement", "Гараж")
	form.Add("logger_sn", "1")
	r := httptest.NewRequest("POST", "/grid/settings", nil)
	r.Form = form
	if _, msg := parseGridForm(r); msg == "" {
		t.Fatal("ожидалась ошибка: размещение не из списка")
	}
}

func TestReadGridSHM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid")
	b := make([]byte, gridSHMHeader+gridMaxInverters*gridSHMItemSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], 32+64)
	binary.LittleEndian.PutUint32(b[12:16], gridMaxInverters)
	it := b[gridSHMHeader : gridSHMHeader+gridSHMItemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN123")
	copy(it[66:130], "192.0.2.10:8899")
	it[130] = 0
	it[131] = 1
	binary.LittleEndian.PutUint64(it[136:144], 7)
	binary.LittleEndian.PutUint64(it[144:152], 123456789)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got := readGridSHM(path)
	if len(got) != 1 || got[0].Serial != "SN123" || got[0].Status != 1 || got[0].ID != 0 {
		t.Fatalf("items: %+v", got)
	}
}

func TestParseGridFormInvalid(t *testing.T) {
	base := func() url.Values {
		v := url.Values{}
		v.Add("placement_name", "Дом")
		v.Add("name", "x")
		v.Add("ip", "192.0.2.10")
		v.Add("port", "8899")
		v.Add("type", "deye")
		v.Add("placement", "Дом")
		v.Add("logger_sn", "1")
		return v
	}
	cases := []struct{ key, val string }{
		{"ip", "not-an-ip"},
		{"port", "70000"},
		{"type", "other"},
		{"logger_sn", "0"},
		{"logger_sn", "abc"},
	}
	for _, c := range cases {
		v := base()
		v.Set(c.key, c.val)
		r := httptest.NewRequest("POST", "/grid/settings", nil)
		r.Form = v
		if _, msg := parseGridForm(r); msg == "" {
			t.Fatalf("%s=%q: ожидалась ошибка", c.key, c.val)
		}
	}
}

func TestParseGridFormLimit(t *testing.T) {
	v := url.Values{}
	v.Add("placement_name", "Дом")
	for i := 0; i < 17; i++ {
		v.Add("name", "x")
		v.Add("ip", "192.0.2."+strconv.Itoa(i+1))
		v.Add("port", "8899")
		v.Add("type", "deye")
		v.Add("placement", "Дом")
		v.Add("logger_sn", "1")
	}
	r := httptest.NewRequest("POST", "/grid/settings", nil)
	r.Form = v
	if _, msg := parseGridForm(r); msg == "" {
		t.Fatal("ожидалась ошибка лимита 16")
	}
}

func TestSaveLoadGridConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid-reader.json")
	s := &Server{opts: Options{GridConfigPath: path}}
	c := gridConfig{
		Enabled: true, LogLevel: "debug", PollPeriod: 20,
		Placements: []string{"Дом"},
		Inverters:  []gridInverter{{Name: "x", IP: "192.0.2.1", Port: 8899, Type: "deye", Placement: "Дом", LoggerSN: 1}},
	}
	if err := s.saveGridConfig(c); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", fi.Mode().Perm())
	}
	got := s.loadGridConfig()
	if !got.Enabled || got.PollPeriod != 20 || len(got.Inverters) != 1 {
		t.Fatalf("got: %+v", got)
	}
}

func TestReadGridSHMBadVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid")
	b := make([]byte, gridSHMHeader+gridMaxInverters*gridSHMItemSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 2)
	binary.LittleEndian.PutUint32(b[8:12], 32+64)
	binary.LittleEndian.PutUint32(b[12:16], gridMaxInverters)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readGridSHM(path); got != nil {
		t.Fatalf("ожидался пустой список, got %+v", got)
	}
}

func TestAPIGridStatusAuthAndMapping(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	b := make([]byte, gridSHMHeader+gridMaxInverters*gridSHMItemSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], 32+64)
	binary.LittleEndian.PutUint32(b[12:16], gridMaxInverters)
	it := b[gridSHMHeader : gridSHMHeader+gridSHMItemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN123")
	copy(it[66:130], "192.0.2.10:8899")
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[{"name":"Deye","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":1}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}

	// без авторизации — 401 JSON
	w := httptest.NewRecorder()
	s.handleAPIGridStatus(w, httptest.NewRequest("GET", "/api/grid/status", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}

	w = httptest.NewRecorder()
	s.handleAPIGridStatus(w, authedReq("GET", "/api/grid/status", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Inverters []gridStatusItem `json:"inverters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Inverters) != 1 || resp.Inverters[0].Name != "Deye" || resp.Inverters[0].Placement != "Дом" || resp.Inverters[0].LoggerSN != 1 {
		t.Fatalf("resp: %+v", resp)
	}
	if !strings.Contains(w.Body.String(), `"logger_sn":1`) {
		t.Fatalf("literal logger_sn отсутствует: %s", w.Body.String())
	}
}

func TestAPIGridStatusLoggerSNByConfigIndex(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	b := make([]byte, gridSHMHeader+gridMaxInverters*gridSHMItemSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], 32+64)
	binary.LittleEndian.PutUint32(b[12:16], gridMaxInverters)
	// Запись только для второго слота: id=1 должен взять logger_sn второй
	// записи конфига, а не первой.
	it := b[gridSHMHeader+gridSHMItemSize : gridSHMHeader+2*gridSHMItemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN222")
	copy(it[66:130], "192.0.2.11:8899")
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[` +
		`{"name":"A","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":111},` +
		`{"name":"B","ip":"192.0.2.11","type":"deye","placement":"Дом","logger_sn":222}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}
	w := httptest.NewRecorder()
	s.handleAPIGridStatus(w, authedReq("GET", "/api/grid/status", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Inverters []gridStatusItem `json:"inverters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Inverters) != 2 {
		t.Fatalf("len=%d resp=%+v", len(resp.Inverters), resp.Inverters)
	}
	if resp.Inverters[0].ID != 0 || resp.Inverters[0].LoggerSN != 111 {
		t.Fatalf("id=0: %+v", resp.Inverters[0])
	}
	if resp.Inverters[1].ID != 1 || resp.Inverters[1].LoggerSN != 222 {
		t.Fatalf("id=1: %+v", resp.Inverters[1])
	}

	// Переходный период после смены настроек: адрес shm не совпадает с
	// конфигом — SN чужой записи подставлять нельзя.
	copy(it[66:130], "192.0.2.99:8899")
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.handleAPIGridStatus(w, authedReq("GET", "/api/grid/status", ""))
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Inverters[1].LoggerSN != 0 {
		t.Fatalf("stale shm addr: %+v", resp.Inverters[1])
	}

	// Пустой конфиг: работает fallback на shm, logger_sn остаётся нулевым.
	if err := os.WriteFile(cfgPath, []byte(`{"enabled":true,"poll_period":10,"inverters":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.handleAPIGridStatus(w, authedReq("GET", "/api/grid/status", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("fallback code=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Inverters) != 1 || resp.Inverters[0].ID != 1 || resp.Inverters[0].LoggerSN != 0 {
		t.Fatalf("fallback: %+v", resp.Inverters)
	}
}

func TestAPIGridDetectRejectsBadIP(t *testing.T) {
	s := &Server{}
	req := authedReq("POST", "/api/grid/detect_sn", `{"ip":"8.8.8.8","port":"8899"}`)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Form = url.Values{"ip": {"8.8.8.8"}, "port": {"8899"}, "type": {"deye"}}
	w := httptest.NewRecorder()
	s.handleAPIGridDetectSN(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestFieldConvGrid(t *testing.T) {
	cases := map[uint16]string{1: "В", 2: "А", 3: "Вт", 8: "вар", 9: "Гц", 16: "°C"}
	for id, unit := range cases {
		fc, ok := fieldConvFor("grid", 1, id)
		if !ok || fc.Unit != unit {
			t.Fatalf("id %d: ok=%v unit=%q", id, ok, fc.Unit)
		}
	}
}

func TestBuildMainGridTiles(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	const slots, itemSize, slotSize = 16, 160, 96
	b := make([]byte, 64+slots*itemSize+slots*slotSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], slotSize)
	binary.LittleEndian.PutUint32(b[12:16], slots)
	it := b[64 : 64+itemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN1")
	copy(it[66:130], "192.0.2.10:8899")
	it[130] = 0
	it[131] = 1
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))

	vals := make([]tscontract.Value, 0, 24)
	for id := uint16(1); id <= 24; id++ {
		v := tscontract.Value{ID: id, Type: tscontract.FTU16, Int: int64(id)}
		if id == 8 || id == 16 || id == 17 {
			v.Type = tscontract.FTI16
		}
		if id == 18 {
			v.Type = tscontract.FTU8
			v.Int = 2
		}
		if id >= 19 {
			v.Int = 0
		}
		vals = append(vals, v)
	}
	payload, err := gridMainContract.Encode(vals)
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload, 10, 0, 1, 2, 3) // today=10, total=0x030201
	sl := b[64+slots*itemSize : 64+slots*itemSize+slotSize]
	binary.LittleEndian.PutUint32(sl[0:4], 1)
	binary.LittleEndian.PutUint64(sl[8:16], 1)
	binary.LittleEndian.PutUint32(sl[24:28], uint32(len(payload)))
	binary.LittleEndian.PutUint32(sl[28:32], crc32.ChecksumIEEE(payload))
	copy(sl[32:], payload)
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[{"name":"Deye","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":1}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}
	tr := map[string]string{"grid.st1": "OK", "grid.inv_st2": "норма"}
	got := s.buildMainGridTiles(tr)
	if len(got) != 1 {
		t.Fatalf("tiles=%d", len(got))
	}
	g := got[0]
	if g.Name != "Deye" || g.Placement != "Дом" || !g.Online || g.Alarm || g.InverterStatusText != "норма" {
		t.Fatalf("tile: %+v", g)
	}
	if g.PVVoltage.V != 0.4 || g.PVCurrent.V != 0.07 || g.PVPower.V != 90 ||
		g.ACActive.V != 70 || g.ACReactive.V != 80 || g.Frequency.V != 0.09 {
		t.Fatalf("values: %+v", g)
	}
	if g.EnergyToday.V != 1 || math.Abs(g.EnergyTotal.V-19712.1) > 1e-6 {
		t.Fatalf("energy: %+v %+v", g.EnergyToday, g.EnergyTotal)
	}
}

func TestAPIGridStatusShowsAllConfigured(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	b := make([]byte, gridSHMHeader+gridMaxInverters*gridSHMItemSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], 32+64)
	binary.LittleEndian.PutUint32(b[12:16], gridMaxInverters)
	it := b[gridSHMHeader : gridSHMHeader+gridSHMItemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN1")
	it[131] = 1
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[` +
		`{"name":"A","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":1},` +
		`{"name":"B","ip":"192.0.2.11","port":8899,"type":"sofar","placement":"Дом","logger_sn":2}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}
	w := httptest.NewRecorder()
	s.handleAPIGridStatus(w, authedReq("GET", "/api/grid/status", ""))
	var resp struct {
		Inverters []gridStatusItem `json:"inverters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Inverters) != 2 {
		t.Fatalf("inverters=%d", len(resp.Inverters))
	}
	if resp.Inverters[0].Status != 1 || resp.Inverters[1].Status != 0 || resp.Inverters[1].Kind != 1 {
		t.Fatalf("resp=%+v", resp.Inverters)
	}
}

func TestBuildMainGridTilesDisabledAndND(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	const slots, itemSize, slotSize = 16, 160, 96
	b := make([]byte, 64+slots*itemSize+slots*slotSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], slotSize)
	binary.LittleEndian.PutUint32(b[12:16], slots)
	it := b[64 : 64+itemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN1")
	it[131] = 1
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))
	// Битый слот: state=1, длина 55, CRC неверный.
	sl := b[64+slots*itemSize : 64+slots*itemSize+slotSize]
	binary.LittleEndian.PutUint32(sl[0:4], 1)
	binary.LittleEndian.PutUint64(sl[8:16], 1)
	binary.LittleEndian.PutUint32(sl[24:28], 55)
	binary.LittleEndian.PutUint32(sl[28:32], 0)
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[{"name":"A","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":1}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}
	got := s.buildMainGridTiles(map[string]string{"grid.st1": "OK", "grid.inv_st0": "ожидание"})
	if len(got) != 1 || !got[0].ACActive.ND || !got[0].PVVoltage.ND || !got[0].EnergyToday.ND {
		t.Fatalf("ND-фолбэк: %+v", got)
	}
	// Выключенный опрос — пусто.
	cfgOff := `{"enabled":false,"poll_period":10,"inverters":[{"name":"A","ip":"192.0.2.10","port":8899,"type":"deye","logger_sn":1}]}`
	if err := os.WriteFile(cfgPath, []byte(cfgOff), 0o600); err != nil {
		t.Fatal(err)
	}
	s = &Server{opts: Options{GridSHMPath: shmPath, GridConfigPath: cfgPath}}
	if got := s.buildMainGridTiles(nil); len(got) != 0 {
		t.Fatalf("disabled: %+v", got)
	}
}

func TestAPIGridDevice(t *testing.T) {
	dir := t.TempDir()
	shmPath := filepath.Join(dir, "grid")
	cfgPath := filepath.Join(dir, "grid.json")
	const slots, itemSize, slotSize = 16, 160, 96
	b := make([]byte, 64+slots*itemSize+slots*slotSize)
	binary.LittleEndian.PutUint32(b[0:4], 0x31445247)
	binary.LittleEndian.PutUint32(b[4:8], 1)
	binary.LittleEndian.PutUint32(b[8:12], slotSize)
	binary.LittleEndian.PutUint32(b[12:16], slots)
	it := b[64 : 64+itemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN1")
	it[130] = 0
	it[131] = 1
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], uint64(time.Now().Unix()))
	vals := make([]tscontract.Value, 0, 24)
	for id := uint16(1); id <= 24; id++ {
		v := tscontract.Value{ID: id, Type: tscontract.FTU16, Int: int64(id)}
		if id == 8 || id == 16 || id == 17 {
			v.Type = tscontract.FTI16
		}
		if id == 16 {
			v.Int = -1000
		}
		if id == 18 {
			v.Type = tscontract.FTU8
		}
		if id == 19 {
			v.Int = 1
		} // W1
		if id > 19 {
			v.Int = 0
		}
		vals = append(vals, v)
	}
	payload, err := gridMainContract.Encode(vals)
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload, 1, 0, 2, 3, 4)
	sl := b[64+slots*itemSize : 64+slots*itemSize+slotSize]
	binary.LittleEndian.PutUint32(sl[0:4], 1)
	binary.LittleEndian.PutUint64(sl[8:16], 1)
	binary.LittleEndian.PutUint32(sl[24:28], uint32(len(payload)))
	binary.LittleEndian.PutUint32(sl[28:32], crc32.ChecksumIEEE(payload))
	copy(sl[32:], payload)
	if err := os.WriteFile(shmPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"enabled":true,"poll_period":10,"placements":["Дом"],"inverters":[{"name":"Deye","ip":"192.0.2.10","port":8899,"type":"deye","placement":"Дом","logger_sn":1}]}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{AssetsDir: "../../assets", AuthStorePath: filepath.Join(dir, "auth"), GridSHMPath: shmPath, GridConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.handleAPIGridDevice(w, authedReq("GET", "/api/grid/device?serial=SN1", ""))
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var doc gridDeviceDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Fields) != 24 || len(doc.Alarms) == 0 || doc.Alarms[0].NameKey != "grid.alm.deye.w1" {
		t.Fatalf("doc: %+v", doc)
	}
	if doc.Fields[0].UnitKey != "home.grid.v" || doc.Fields[7].UnitKey != "home.grid.var" || !doc.Fields[15].ND || doc.Fields[18].Value != "0x0001" {
		t.Fatalf("fields: %+v", doc.Fields)
	}
	// Страница: без id — редирект, с id — 200.
	w2 := httptest.NewRecorder()
	srv.handleGridPage(w2, authedReq("GET", "/grid", ""))
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("no id code=%d", w2.Code)
	}
	w3 := httptest.NewRecorder()
	srv.handleGridPage(w3, authedReq("GET", "/grid?id=SN1&gen=1", ""))
	if w3.Code != http.StatusOK {
		t.Fatalf("page code=%d body=%s", w3.Code, w3.Body.String())
	}
}
