package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// mapModelFull — локализованное имя модели для заголовка (единый источник с
// меню — tr["model."+code]; fallback русский modelNames, англ./прочие при
// отсутствии перевода — как код модели).
func mapModelFull(tr map[string]string, code string) string {
	if n := modelName(tr, code); n != "" {
		return n
	}
	return code
}

func (s *Server) handleMapPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	sn := r.URL.Query().Get("serial")
	m, ok := findMap(id)
	// Устаревший id/gen (устройство переоткрылось): ищем по серийнику из запроса.
	if !ok && sn != "" {
		m, ok = findMapBySerial(sn)
	}
	data := s.baseData(r)
	data["Active"] = "map"
	data["FieldNames"] = mapFieldNames
	data["Model"] = ""
	if ok {
		data["ID"] = m.ID
		data["Gen"] = m.Gen
		data["SerialLabel"] = m.SerialLabel()
		data["Model"] = m.Model
		data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "map.title"), mapModelFull(s.trFor(r), m.Model), m.SerialLabel(), m.Port)
	} else if sn != "" {
		// Устройство ещё не обнаружено — страница-ожидание; клиент повторит поиск.
		data["ID"] = id
		data["Gen"] = 0
		data["SerialLabel"] = sn
		data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "map.title"), "", sn, "")
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "map", data)
}

// handleAPIMapRaw отдаёт бинарный payload MapData конкретного МАП (слот id).
func (s *Server) handleAPIMapRaw(w http.ResponseWriter, r *http.Request) {
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
	// gen обязателен: id+gen должны совпасть с текущим устройством.
	g, err := strconv.ParseUint(r.URL.Query().Get("gen"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if m, ok := findMap(id); !ok || m.Gen != g {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	payload := mapRawPayload(id)
	if payload == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// handleAPIMapDevices — список подключённых устройств типа «map» (МАП) для
// динамического меню. Под авторизацией (cookie || Bearer). Будущие типы (kes и
// др.) получат свои /api/<type>/devices.
func (s *Server) handleAPIMapDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	type device struct {
		Type   string `json:"type"`
		ID     int    `json:"id"`
		Gen    uint64 `json:"gen"`
		Serial string `json:"serial"`
		Label  string `json:"label"`
	}
	out := make([]device, 0, 8)
	tr := s.trFor(r)
	for _, m := range loadMapList().Maps {
		out = append(out, device{Type: "map", ID: m.ID, Gen: m.Gen, Serial: m.SerialLabel(), Label: m.MenuLabel(tr)})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
