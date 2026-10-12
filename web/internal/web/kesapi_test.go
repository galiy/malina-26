package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authedReq — запрос с подтверждённой сессией (authKey в контексте).
func authedReq(method, target, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	return req.WithContext(context.WithValue(req.Context(), authKey, true))
}

// TestKesDevices — /api/kes/devices отдаёт список КЭС (тип "kes", id/gen/label).
func TestKesDevices(t *testing.T) {
	prev := kesShmPath
	kesShmPath = writeKesShmSegTest(t, buildKesPayloadTest(testKesFields()))
	t.Cleanup(func() { kesShmPath = prev })

	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIKesDevices(rec, authedReq(http.MethodGet, "/api/kes/devices", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, ожидалось 200", rec.Code)
	}
	var arr []struct {
		Type  string `json:"type"`
		ID    int    `json:"id"`
		Gen   uint64 `json:"gen"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("JSON: %v (%q)", err, rec.Body.String())
	}
	if len(arr) != 1 {
		t.Fatalf("kes=%d, ожидалось 1", len(arr))
	}
	if arr[0].Type != "kes" || arr[0].ID != 0 || arr[0].Gen != 0 {
		t.Fatalf("первый КЭС: %+v", arr[0])
	}
	if !strings.Contains(arr[0].Label, "КЭС") {
		t.Fatalf("label=%q", arr[0].Label)
	}
}

// TestKesDevicesAuth — без сессии → 401.
func TestKesDevicesAuth(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIKesDevices(rec, httptest.NewRequest(http.MethodGet, "/api/kes/devices", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, ожидалось 401", rec.Code)
	}
}

// TestKesAPIKesDevicesMethod — не GET → 405.
func TestKesDevicesMethod(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIKesDevices(rec, authedReq(http.MethodPost, "/api/kes/devices", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d, ожидалось 405", rec.Code)
	}
}

// TestKesRaw — /api/kes/raw отдаёт бинарный payload слота по id+gen.
func TestKesRaw(t *testing.T) {
	payload := buildKesPayloadTest(testKesFields())
	prev := kesShmPath
	kesShmPath = writeKesShmSegTest(t, payload)
	t.Cleanup(func() { kesShmPath = prev })

	s := &Server{}

	// валидный id=0&gen=0 → 200, тело — payload.
	rec := httptest.NewRecorder()
	s.handleAPIKesRaw(rec, authedReq(http.MethodGet, "/api/kes/raw?id=0&gen=0", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("валидный: code=%d", rec.Code)
	}
	if rec.Body.String() != string(payload) {
		t.Fatalf("валидный: тело не совпадает с payload (len %d vs %d)", rec.Body.Len(), len(payload))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type=%q", ct)
	}

	// неверный gen → 404.
	rec = httptest.NewRecorder()
	s.handleAPIKesRaw(rec, authedReq(http.MethodGet, "/api/kes/raw?id=0&gen=99", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("gen: code=%d, ожидалось 404", rec.Code)
	}

	// без gen → 400.
	rec = httptest.NewRecorder()
	s.handleAPIKesRaw(rec, authedReq(http.MethodGet, "/api/kes/raw?id=0", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("без gen: code=%d, ожидалось 400", rec.Code)
	}
}

// TestKesWrite — /api/kes/write проксирует в kes-reader (ок + статусы по коду).
func TestKesWrite(t *testing.T) {
	cases := []struct {
		resp string
		want int
	}{
		{`{"ok":true}`, http.StatusOK},
		{`{"ok":false,"code":"no_device","error":"x"}`, http.StatusNotFound},
		{`{"ok":false,"code":"kes_error","error":"x","op":0,"ecode":"10"}`, http.StatusUnprocessableEntity},
		{`{"ok":false,"code":"retry_exhausted","error":"x"}`, http.StatusBadGateway},
		{`{"ok":false,"code":"internal","error":"x"}`, http.StatusInternalServerError},
		{`{"ok":false,"error":"bad request"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		sock := startKesControlSockTest(t, c.resp)
		s := &Server{opts: Options{User: "malina", KesControlSock: sock}}
		rec := httptest.NewRecorder()
		s.handleAPIKesWrite(rec, authedReq(http.MethodPost, "/api/kes/write",
			`{"id":0,"gen":1,"ops":[{"op":"reg","addr":"E3D","data":"01"}]}`))
		if rec.Code != c.want {
			t.Errorf("resp %s: got %d, want %d", c.resp, rec.Code, c.want)
		}
	}
}

// TestKesWriteAuth — без сессии → 401.
func TestKesWriteAuth(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAPIKesWrite(rec, httptest.NewRequest(http.MethodPost, "/api/kes/write",
		strings.NewReader(`{"id":0}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, ожидалось 401", rec.Code)
	}
}
