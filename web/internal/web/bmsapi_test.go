package web

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// writeBmsShmSegTest создаёт временный shm-сегмент BMS с одним устройством
// (slot 0, gen 0) и возвращает путь.
func writeBmsShmSegTest(t *testing.T, payload []byte) string {
	t.Helper()
	size := bmsShmHeader + bmsSlots*bmsListItemLen + bmsSlots*bmsSlotSize
	buf := make([]byte, size)
	binary.LittleEndian.PutUint32(buf[0:4], bmsShmMagic)
	binary.LittleEndian.PutUint32(buf[4:8], bmsShmVersion)

	it := buf[bmsShmHeader : bmsShmHeader+bmsListItemLen]
	binary.LittleEndian.PutUint32(it[0:4], 1) // state != 0
	copy(it[bmsOffID:], "AntBMS-DSP-320")
	copy(it[bmsOffBase:], "AntBMS-DSP-320")
	copy(it[bmsOffSource:], "serial:/dev/ttyUSB0")
	it[bmsOffTypeID] = 1 // AntBMS-DSP
	it[bmsOffState] = 1
	it[bmsOffFraming] = 0
	binary.LittleEndian.PutUint64(it[bmsOffGen:], 0)
	binary.LittleEndian.PutUint64(it[bmsOffLastSeen:], 1700000000)

	slotOff := bmsShmHeader + bmsSlots*bmsListItemLen
	slot := buf[slotOff : slotOff+bmsSlotSize]
	binary.LittleEndian.PutUint32(slot[0:4], 1) // state != 0
	binary.LittleEndian.PutUint64(slot[8:16], 0)
	binary.LittleEndian.PutUint32(slot[24:28], uint32(len(payload)))
	copy(slot[bmsSlotHdr:], payload)

	path := filepath.Join(t.TempDir(), "bms-state")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBmsDevices(t *testing.T) {
	prev := bmsShmPath
	bmsShmPath = writeBmsShmSegTest(t, make([]byte, bmsSlotData))
	t.Cleanup(func() { bmsShmPath = prev })

	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIBmsDevices(rec, authedReq(http.MethodGet, "/api/bms/devices", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var arr []struct {
		Type    string `json:"type"`
		ID      int    `json:"id"`
		Gen     uint64 `json:"gen"`
		Serial  string `json:"serial"`
		BmsType string `json:"bms_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("JSON: %v (%q)", err, rec.Body.String())
	}
	if len(arr) != 1 {
		t.Fatalf("bms=%d, ожидалось 1", len(arr))
	}
	if arr[0].Type != "bms" || arr[0].ID != 0 || arr[0].Serial != "AntBMS-DSP-320" {
		t.Fatalf("запись: %+v", arr[0])
	}
	if arr[0].BmsType != "AntBMS-DSP" {
		t.Fatalf("bms_type=%q", arr[0].BmsType)
	}
}

func TestBmsDevicesAuth(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIBmsDevices(rec, httptest.NewRequest(http.MethodGet, "/api/bms/devices", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, ожидалось 401", rec.Code)
	}
}

func TestBmsRaw(t *testing.T) {
	payload := make([]byte, bmsSlotData)
	for i := range payload {
		payload[i] = byte(i)
	}
	prev := bmsShmPath
	bmsShmPath = writeBmsShmSegTest(t, payload)
	t.Cleanup(func() { bmsShmPath = prev })

	s := &Server{}

	rec := httptest.NewRecorder()
	s.handleAPIBmsRaw(rec, authedReq(http.MethodGet, "/api/bms/raw?id=0&gen=0", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("валидный: code=%d", rec.Code)
	}
	if rec.Body.Len() != bmsSlotData {
		t.Fatalf("длина payload=%d, ожидалось %d", rec.Body.Len(), bmsSlotData)
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("Content-Type=%q", rec.Header().Get("Content-Type"))
	}

	rec = httptest.NewRecorder()
	s.handleAPIBmsRaw(rec, authedReq(http.MethodGet, "/api/bms/raw?id=0&gen=99", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("gen: code=%d, ожидалось 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleAPIBmsRaw(rec, authedReq(http.MethodGet, "/api/bms/raw?id=0", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("без gen: code=%d, ожидалось 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleAPIBmsRaw(rec, authedReq(http.MethodGet, "/api/bms/raw?id=abc&gen=0", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("id=abc: code=%d, ожидалось 400", rec.Code)
	}
}

// TestBmsTileFields — плитка BMS: единицы, метки типа/режима, online.
func TestBmsTileFields(t *testing.T) {
	tr := map[string]string{"bms.type.ant": "ANT BMS", "bms.mode.charge": "Заряд"}
	tel := bmscontract.Telemetry{CellCount: 2, TempCount: 1, SocDP: 500, TotalCV: 5300, CurrentDA: 123, PowerW: 65, Mode: bmscontract.ModeCharge}
	tel.TempsDC[0] = 250
	b := BmsInfo{ID: 0, Gen: 7, Serial: "AntBMS-DSP-160", TypeID: 1, LastSeen: nowSec()}
	tile := buildBmsTile(tr, b, tel)

	if tile.Title != "AntBMS-DSP-160" {
		t.Fatalf("title=%q", tile.Title)
	}
	if tile.Serial != "AntBMS-DSP-160" {
		t.Fatalf("serial=%q", tile.Serial)
	}
	if tile.Mode != "Заряд" {
		t.Fatalf("mode=%q", tile.Mode)
	}
	if tile.Pack.U.V != 53 || tile.Pack.I.V != 1.23 || tile.Pack.P.V != 65 || tile.Pack.T.V != 25 {
		t.Fatalf("pack: %+v", tile.Pack)
	}
	if tile.Soc == nil || *tile.Soc != 50 {
		t.Fatalf("soc=%v", tile.Soc)
	}
	if !tile.Online {
		t.Fatal("online должен быть true")
	}
}

// TestFindBmsForPageGen — страница не открывает другое устройство при
// отсутствующем/устаревшем gen; fallback по serial остаётся (первое совпадение).
func TestFindBmsForPageGen(t *testing.T) {
	prev := bmsShmPath
	bmsShmPath = writeBmsShmSegTest(t, make([]byte, bmsSlotData))
	t.Cleanup(func() { bmsShmPath = prev })

	if _, ok := findBmsForPage(0, 0, "", false); ok {
		t.Fatal("без валидных id+gen запись не должна открываться")
	}
	if _, ok := findBmsForPage(0, 0, "AntBMS-DSP-320", false); !ok {
		t.Fatal("fallback по serial должен находить устройство")
	}
	if _, ok := findBmsForPage(0, 99, "OTHER", true); ok {
		t.Fatal("устаревший gen + чужой serial не должен открывать устройство")
	}
	if b, ok := findBmsForPage(0, 99, "AntBMS-DSP-320", true); !ok || b.ID != 0 {
		t.Fatalf("fallback по serial: ok=%v b=%+v", ok, b)
	}
}

// TestFindBmsForPageStaleSlot — старый id уже занят другим устройством, а
// исходный serial живёт в другом слоте: fallback должен вернуть именно его.
func TestFindBmsForPageStaleSlot(t *testing.T) {
	path := writeBmsShmSegTwo(t, make([]byte, bmsSlotData))
	prev := bmsShmPath
	bmsShmPath = path
	t.Cleanup(func() { bmsShmPath = prev })

	if b, ok := findBmsForPage(0, 20, "A-BASE", true); !ok || b.ID != 1 || b.Gen != 20 {
		t.Fatalf("fallback: ok=%v b=%+v", ok, b)
	}
	if _, ok := findBmsForPage(0, 20, "OTHER", true); ok {
		t.Fatal("чужой serial не должен открывать устройство")
	}
}

// writeBmsShmSegTwo — два устройства: slot0/B-BASE/gen10, slot1/A-BASE/gen20.
func writeBmsShmSegTwo(t *testing.T, payload []byte) string {
	t.Helper()
	path := writeBmsShmSegTest(t, payload)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	off0 := bmsShmHeader
	putStr := func(off int, s string) {
		for i := off; i < off+50; i++ {
			data[i] = 0
		}
		copy(data[off:], s)
	}
	putStr(off0+bmsOffID, "B-BASE")
	putStr(off0+bmsOffBase, "B-BASE")
	binary.LittleEndian.PutUint64(data[off0+bmsOffGen:], 10)

	off1 := off0 + bmsListItemLen
	copy(data[off1:off1+bmsListItemLen], data[off0:off0+bmsListItemLen])
	putStr(off1+bmsOffID, "A-BASE")
	putStr(off1+bmsOffBase, "A-BASE")
	binary.LittleEndian.PutUint64(data[off1+bmsOffGen:], 20)
	putStr(off1+bmsOffSource, "serial:/dev/ttyUSB1")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBmsDevicesDuplicateBase — два слота с одинаковым BASE/ID (принятый риск):
// API отдаёт обе записи, различающиеся id/слотом и gen.
func TestBmsDevicesDuplicateBase(t *testing.T) {
	path := writeBmsShmSegTest(t, make([]byte, bmsSlotData))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	item0 := append([]byte(nil), data[bmsShmHeader:bmsShmHeader+bmsListItemLen]...)
	item1 := append([]byte(nil), item0...)
	binary.LittleEndian.PutUint64(item1[bmsOffGen:], 2)
	copy(item1[bmsOffSource:], "serial:/dev/ttyUSB1")
	copy(data[bmsShmHeader+bmsListItemLen:], item1)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	prev := bmsShmPath
	bmsShmPath = path
	t.Cleanup(func() { bmsShmPath = prev })

	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIBmsDevices(rec, authedReq(http.MethodGet, "/api/bms/devices", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var arr []struct {
		ID     int    `json:"id"`
		Gen    uint64 `json:"gen"`
		Serial string `json:"serial"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("JSON: %v (%q)", err, rec.Body.String())
	}
	if len(arr) != 2 {
		t.Fatalf("bms=%d, ожидалось 2", len(arr))
	}
	if arr[0].ID != 0 || arr[0].Gen != 0 || arr[1].ID != 1 || arr[1].Gen != 2 {
		t.Fatalf("записи: %+v", arr)
	}
	if arr[0].Serial != "AntBMS-DSP-320" || arr[1].Serial != "AntBMS-DSP-320" {
		t.Fatalf("serial: %+v", arr)
	}
}

// TestParseBmsSlotQuery — слот доверяем только при валидных непустых id+gen.
func TestParseBmsSlotQuery(t *testing.T) {
	cases := []struct {
		id, gen string
		valid   bool
	}{
		{"0", "123", true},
		{"3", "123", true},
		{"", "123", false},
		{"abc", "123", false},
		{"0", "", false},
		{"0", "abc", false},
		{"0", "0", false},
	}
	for _, c := range cases {
		if _, _, ok := parseBmsSlotQuery(c.id, c.gen); ok != c.valid {
			t.Errorf("id=%q gen=%q: valid=%v, ожидалось %v", c.id, c.gen, ok, c.valid)
		}
	}
}
