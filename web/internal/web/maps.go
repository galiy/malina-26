package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// mapModelFull — имя модели для заголовка (единый источник с меню — modelNames).
func mapModelFull(code string) string {
	if n := modelNames[code]; n != "" {
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
	m, ok := findMap(id)
	if !ok {
		http.Redirect(w, r, s.base(r)+"/", http.StatusSeeOther)
		return
	}
	// Если задан gen — он должен совпасть (защита от переиспользования слота).
	if gs := r.URL.Query().Get("gen"); gs != "" {
		if g, err := strconv.ParseUint(gs, 10, 64); err != nil || g != m.Gen {
			http.Redirect(w, r, s.base(r)+"/", http.StatusSeeOther)
			return
		}
	}
	data := s.baseData(r)
	data["Active"] = "map"
	data["ID"] = m.ID
	data["Gen"] = m.Gen
	data["FieldNames"] = mapFieldNames
	data["SerialLabel"] = m.SerialLabel()
	// Заголовок: "MAP <модель> s/n <sn> порт <port>" (шаблон локализуется).
	data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "map.title"), mapModelFull(m.Model), m.SerialLabel(), m.Port)
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
		Type  string `json:"type"`
		ID    int    `json:"id"`
		Gen   uint64 `json:"gen"`
		Label string `json:"label"`
	}
	out := make([]device, 0, 8)
	for _, m := range loadMapList().Maps {
		out = append(out, device{Type: "map", ID: m.ID, Gen: m.Gen, Label: m.MenuLabel()})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
