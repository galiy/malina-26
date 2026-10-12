package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Native REST для КЭС: /api/kes/{devices,raw,write}.
// Контракт — kes-list + payload слота (KESE+KESR+KESS, 1536 Б), см.
// docs/kes-reader.md §4. Авторизация — как у /api/map/*: cookie || Bearer
// (сессия); запись — с аудитом (тег kes_write).
//
// Отличие от legacy-ручек (read_sec/write_sec/read_json?device=mppt): здесь
// сырой контракт kes-reader без legacy-обвязки и без Basic+PAM.

// handleAPIKesDevices — список подключённых КЭС (тип "kes") для меню/инвентаря.
func (s *Server) handleAPIKesDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type device struct {
		Type  string `json:"type"`
		ID    int    `json:"id"`
		Gen   uint64 `json:"gen"`
		Label string `json:"label"`
	}
	out := make([]device, 0, 8)
	for _, k := range loadKesList().Kes {
		out = append(out, device{Type: "kes", ID: k.ID, Gen: k.Gen, Label: k.MenuLabel()})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// handleAPIKesRaw — бинарный payload слота КЭС (id+gen из kes-list), как
// /api/map/raw для МАП.
func (s *Server) handleAPIKesRaw(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	g, err := strconv.ParseUint(r.URL.Query().Get("gen"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if k, ok := findKes(id); !ok || k.Gen != g {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	payload := kesRawPayload(id)
	if payload == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// handleAPIKesWrite — HTTP-обёртка над командой `write` kes-reader
// (cookie || Bearer + аудит). Тело запроса — как у сокета kes-reader
// (docs/kes-reader.md §4.2.3); ответ — тот же JSON, HTTP-статус по коду.
func (s *Server) handleAPIKesWrite(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		s.audit.Event("kes_write", map[string]any{"result": "unauthorized", "ip": clientIP(r)})
		w.Header().Set("WWW-Authenticate", `Bearer realm="malina", error="invalid_token"`)
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"ok": false, "code": "unauthorized", "error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_request", "error": "тело запроса слишком велико или нечитаемо"})
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_request", "error": "невалидный JSON"})
		return
	}
	obj["cmd"] = "write"
	body, _ = json.Marshal(obj)

	resp, err := controlCall(s.opts.KesControlSock, body)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "code": "retry_exhausted", "error": err.Error()})
		return
	}
	var kr kesWriteResp
	_ = json.Unmarshal(resp, &kr)

	id, _ := obj["id"].(float64)
	gen, _ := obj["gen"].(float64)
	opsN := 0
	if ops, ok := obj["ops"].([]any); ok {
		opsN = len(ops)
	}
	res := "ok"
	if !kr.OK {
		res = "err"
	}
	s.audit.Event("kes_write", map[string]any{
		"id": int(id), "gen": uint64(gen), "ops": opsN,
		"result": res, "code": kr.Code, "ip": clientIP(r),
	})

	status := http.StatusOK
	if !kr.OK {
		switch kr.Code {
		case "", "bad_request":
			status = http.StatusBadRequest
		case "no_device":
			status = http.StatusNotFound
		case "kes_error":
			status = http.StatusUnprocessableEntity
		case "retry_exhausted":
			status = http.StatusBadGateway
		default:
			status = http.StatusInternalServerError
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// findKes возвращает запись kes-list по реальному номеру слота.
func findKes(id int) (KesInfo, bool) {
	for _, k := range loadKesList().Kes {
		if k.ID == id {
			return k, true
		}
	}
	return KesInfo{}, false
}

// MenuLabel — подпись КЭС для меню/инвентаря: серийник + порт + скорость.
func (k KesInfo) MenuLabel() string {
	return fmt.Sprintf("КЭС · %s · %s · %d", k.SerialLabel(), k.Port, k.Baud)
}
