package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/galiy/malina-26/web/internal/auth"
)

func TestBuildEEPROMOps(t *testing.T) {
	ops, ok := buildEEPROMOps(`[[438,1],[1099,0],[0,2]]`)
	if !ok || len(ops) != 3 {
		t.Fatalf("ops=%v ok=%v", ops, ok)
	}
	if ops[0]["op"] != "reg" || ops[0]["addr"] != "1B6" || ops[0]["data"] != "01" {
		t.Errorf("pair0: %v", ops[0])
	}
	if ops[1]["op"] != "reg" || ops[1]["addr"] != "44B" || ops[1]["data"] != "00" {
		t.Errorf("pair1: %v", ops[1])
	}
	if ops[2]["op"] != "cmd" || ops[2]["code"] != "02" {
		t.Errorf("pair2 (addr 0 → cmd): %v", ops[2])
	}

	for _, bad := range []string{`bad`, `[[1]]`, `[["a",1]]`, `[]x`} {
		if _, ok := buildEEPROMOps(bad); ok {
			t.Errorf("%q должно быть отвергнуто", bad)
		}
	}
	// Пустой список — валидно (нет операций; сервис сам отклонит как bad_request).
	if _, ok := buildEEPROMOps(`[]`); !ok {
		t.Error("пустой список должен разбираться")
	}
}

func TestGroupOps(t *testing.T) {
	ops, _ := buildEEPROMOps(`[[438,1],[1099,0],[0,2],[435,5]]`) // reg, reg, cmd, reg
	reqs := groupOps(ops)
	if len(reqs) != 3 {
		t.Fatalf("ожидали 3 запроса, got %d: %v", len(reqs), reqs)
	}
	if len(reqs[0]) != 2 || reqs[0][0]["op"] != "reg" || reqs[0][1]["op"] != "reg" {
		t.Errorf("группа 0 (два регистра): %v", reqs[0])
	}
	if len(reqs[1]) != 1 || reqs[1][0]["op"] != "cmd" {
		t.Errorf("группа 1 (команда): %v", reqs[1])
	}
	if len(reqs[2]) != 1 || reqs[2][0]["op"] != "reg" {
		t.Errorf("группа 2 (регистр после команды): %v", reqs[2])
	}
}

func TestWriteEEPROMAuth(t *testing.T) {
	dev := auth.NewDev("malina", "pw")
	ls := newLegacyService("/nonexistent")
	go ls.run()
	s := &Server{
		opts:   Options{User: "malina"},
		basic:  auth.NewBasicCache(dev, "malina", time.Minute, 8),
		legacy: ls,
	}
	h := http.HandlerFunc(s.handleWriteEEPROM)

	form := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/write_eeprom.php",
			strings.NewReader(url.Values{"data": {`[[438,1]]`}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	// Без Basic — 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, form())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("без auth: got %d, want 401", rec.Code)
	}

	// С Basic, но модуль не закреплён за МАП — legacy-ответ "error" (200).
	r := form()
	r.SetBasicAuth("malina", "pw")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || rec.Body.String() != "error" {
		t.Fatalf("нет привязки: got %d %q, want 200 \"error\"", rec.Code, rec.Body.String())
	}
}
