package web

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
	it := b[gridSHMHeader : gridSHMHeader+gridSHMItemSize]
	binary.LittleEndian.PutUint32(it[0:4], 1)
	copy(it[16:66], "SN123")
	copy(it[66:130], "192.0.2.10:8899")
	binary.LittleEndian.PutUint64(it[136:144], 1)
	binary.LittleEndian.PutUint64(it[144:152], 123)
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
	if len(resp.Inverters) != 1 || resp.Inverters[0].Name != "Deye" || resp.Inverters[0].Placement != "Дом" {
		t.Fatalf("resp: %+v", resp)
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
