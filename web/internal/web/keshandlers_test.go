package web

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galiy/malina-26/web/internal/auth"
)

// TestKesRmwOps — RMW-матрица 3 реле: включение/выключение из разных состояний,
// ранний Ok (пустой ops) и невалидный relay.
func TestKesRmwOps(t *testing.T) {
	// cur=0b000: все выключены.
	// relay1 on → из 0 в 001 → "01"; relay1 off → уже off → ранний Ok (пусто).
	cases := []struct {
		name      string
		cur       byte
		relay     int
		on        bool
		wantEmpty bool // ранний Ok
		wantData  string
		wantOk    bool
	}{
		{"r1 on из 0", 0x00, 1, true, false, "01", true},
		{"r2 on из 0", 0x00, 2, true, false, "02", true},
		{"r3 on из 0", 0x00, 3, true, false, "04", true},
		{"r1 off уже off", 0x00, 1, false, true, "", true},
		{"r1 on уже on", 0x01, 1, true, true, "", true},
		{"r1 on из 011", 0x06, 1, true, false, "07", true}, // r2,r3 вкл, r1 выкл
		{"r2 off из 011", 0x07, 2, false, false, "05", true},
		{"r3 off из 101", 0x05, 3, false, false, "01", true},
		{"relay 0", 0x00, 0, true, false, "", false},
		{"relay 4", 0x00, 4, true, false, "", false},
		{"r1 off из 001", 0x01, 1, false, false, "00", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops, ok := kesRmwOps(c.cur, c.relay, c.on)
			if ok != c.wantOk {
				t.Fatalf("ok=%v, ожидалось %v", ok, c.wantOk)
			}
			if !c.wantOk {
				return
			}
			if c.wantEmpty {
				if len(ops) != 0 {
					t.Fatalf("ожидался ранний Ok (пусто), got %v", ops)
				}
				return
			}
			if len(ops) != 1 || ops[0]["addr"] != "E3D" || ops[0]["data"] != c.wantData {
				t.Fatalf("ops=%v, ожидалось addr=E3D data=%s", ops, c.wantData)
			}
			if ops[0]["op"] != "reg" {
				t.Fatalf("op=%v, ожидалось reg", ops[0]["op"])
			}
		})
	}
}

// TestWriteSecMethod — write_sec принимает GET|POST, но не другие методы.
func TestReadSecMethodAndBounds(t *testing.T) {
	s := &Server{}
	// не GET → 405
	rec := httptest.NewRecorder()
	s.handleReadSec(rec, httptest.NewRequest(http.MethodPost, "/read_sec.php", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: code=%d, ожидалось 405", rec.Code)
	}
	// без id → error
	rec = httptest.NewRecorder()
	s.handleReadSec(rec, httptest.NewRequest(http.MethodGet, "/read_sec.php?offset=0", nil))
	if rec.Body.String() != "error" {
		t.Fatalf("без id: %q", rec.Body.String())
	}
	// id вне диапазона (нет shm → Count=0) → error
	rec = httptest.NewRecorder()
	s.handleReadSec(rec, httptest.NewRequest(http.MethodGet, "/read_sec.php?id=1&offset=0&count=6", nil))
	if rec.Body.String() != "error" {
		t.Fatalf("id вне диапазона: %q", rec.Body.String())
	}
}

// TestReadSecSuccess — success-path read_sec: валидный КЭС возвращает байты
// виртуального пространства. Формат — как legacy (docs/legacy/kes-legacy-malina2.md
// §20): offset=0 → JSON-массив, offset>0 → объект {addr:byte}.
func TestReadSecSuccess(t *testing.T) {
	prev := kesShmPath
	kesShmPath = writeKesShmSegTest(t, buildKesPayloadTest(testKesFields()))
	t.Cleanup(func() { kesShmPath = prev })
	s := &Server{}

	// offset=0&count=6 → массив из байтов VA (в т.ч. 0x2C2 Mode='B').
	rec := httptest.NewRecorder()
	s.handleReadSec(rec, httptest.NewRequest(http.MethodGet, "/read_sec.php?id=1&offset=0&count=6", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("offset=0: code=%d", rec.Code)
	}
	var arr []int
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("offset=0: не массив: %v (%q)", err, rec.Body.String())
	}
	if len(arr) != 6 {
		t.Fatalf("offset=0: len=%d, ожидалось 6", len(arr))
	}

	// offset>0 → объект {addr:byte}; ключ — десятичный абсолютный адрес.
	// legacy intval($_REQUEST['offset']) парсит только десятичное, поэтому 706 (=0x2C2).
	rec = httptest.NewRecorder()
	s.handleReadSec(rec, httptest.NewRequest(http.MethodGet, "/read_sec.php?id=1&offset=706&count=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("offset>0: code=%d", rec.Code)
	}
	var obj map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &obj); err != nil {
		t.Fatalf("offset>0: не объект: %v (%q)", err, rec.Body.String())
	}
	// 0x2C2 = 706; VA[0x2C2] = Mode → testKesFields() Mode='b' → 0x62 = 98.
	if obj["706"] != 98 {
		t.Fatalf("offset>0: obj[706]=%d, ожидалось 98 (Mode 'b')", obj["706"])
	}
}

// TestWriteSecMethod — write_sec принимает GET|POST, но не другие методы.
func TestWriteSecMethod(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleWriteSec(rec, httptest.NewRequest(http.MethodPut, "/write_sec.php?id=1&relay=1&mode=on", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT: code=%d, ожидалось 405", rec.Code)
	}
	if rec.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("Allow=%q, ожидалось 'GET, POST'", rec.Header().Get("Allow"))
	}
}

// writeKesShmSegTest записывает временный shm-сегмент kes с одной активной записью
// КЭС (id=0) и заданным payload слота. Возвращает путь сегмента (в t.TempDir()).
func writeKesShmSegTest(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kes-shm")
	seg := make([]byte, kesShmHeader+kesSlots*kesListItemLen+kesSlots*kesSlotSize)
	binary.LittleEndian.PutUint32(seg[0:4], kesShmMagic)
	binary.LittleEndian.PutUint32(seg[4:8], kesShmVersion)
	// запись списка: слот 0 активен (state=1, seqlock чётный).
	off := kesShmHeader
	binary.LittleEndian.PutUint32(seg[off:off+4], 1)
	// payload слота 0: state=1, seqlock=0, plen=len(payload).
	so := kesShmHeader + kesSlots*kesListItemLen
	binary.LittleEndian.PutUint32(seg[so:so+4], 1)
	binary.LittleEndian.PutUint32(seg[so+24:so+28], uint32(len(payload)))
	copy(seg[so+kesSlotHdr:so+kesSlotHdr+len(payload)], payload)
	if err := os.WriteFile(path, seg, 0o644); err != nil {
		t.Fatalf("write shm: %v", err)
	}
	return path
}

// startKesControlSockTest поднимает unix-сокет, отвечающий на любую команду
// заданной строкой (по соглашению controlCall: запрос + '\n', ответ + '\n').
func startKesControlSockTest(t *testing.T, resp string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "kes.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte(resp + "\n"))
				c.Close()
			}(conn)
		}
	}()
	return sock
}

// TestWriteSecSuccess — success-path write_sec: валидная запись реле КЭС проходит
// через auth, RMW и control-сокет, отвечая "Ok". Покрывает regression: параметры
// обязаны читаться из r.Form после ParseForm (раньше r.Form был пуст → всегда "error").
func TestWriteSecSuccess(t *testing.T) {
	f := testKesFields() // RelayC=0x05 (R1 и R3)
	prev := kesShmPath
	kesShmPath = writeKesShmSegTest(t, buildKesPayloadTest(f))
	t.Cleanup(func() { kesShmPath = prev })

	dev := auth.NewDev("malina", "pw")
	sock := startKesControlSockTest(t, `{"ok":true}`)
	s := &Server{
		opts:  Options{User: "malina", KesControlSock: sock},
		basic: auth.NewBasicCache(dev, "malina", time.Minute, 8),
	}

	cases := []struct {
		name        string
		relay, mode string
	}{
		{"reле R2 из 001 → запись", "2", "on"},            // R2 выкл → пишем маску
		{"reле R1 из 001 → ранний Ok (пусто)", "1", "on"}, // R1 уже вкл
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			form := url.Values{"id": {"1"}, "relay": {c.relay}, "mode": {c.mode}}
			req := httptest.NewRequest(http.MethodPost, "/write_sec.php",
				strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetBasicAuth("malina", "pw")
			rec := httptest.NewRecorder()
			s.handleWriteSec(rec, req)
			if rec.Code != http.StatusOK || rec.Body.String() != "Ok" {
				t.Fatalf("code=%d body=%q, ожидалось 200 \"Ok\"", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestWriteSecSuccessGetParams — write_sec принимает параметры и в query (GET),
// что раньше ломалось из-за невызова ParseForm (r.Form пуст даже для GET).
func TestWriteSecSuccessGetParams(t *testing.T) {
	f := testKesFields() // RelayC=0x05 (R1 и R3)
	prev := kesShmPath
	kesShmPath = writeKesShmSegTest(t, buildKesPayloadTest(f))
	t.Cleanup(func() { kesShmPath = prev })

	dev := auth.NewDev("malina", "pw")
	sock := startKesControlSockTest(t, `{"ok":true}`)
	s := &Server{
		opts:  Options{User: "malina", KesControlSock: sock},
		basic: auth.NewBasicCache(dev, "malina", time.Minute, 8),
	}

	req := httptest.NewRequest(http.MethodGet, "/write_sec.php?id=1&relay=2&mode=on", nil)
	req.SetBasicAuth("malina", "pw")
	rec := httptest.NewRecorder()
	s.handleWriteSec(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "Ok" {
		t.Fatalf("code=%d body=%q, ожидалось 200 \"Ok\"", rec.Code, rec.Body.String())
	}
}
