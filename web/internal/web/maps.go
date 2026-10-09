package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// mapModelFull — полное имя модели (заголовок страницы, не локализуется).
func mapModelFull(code string) string {
	switch code {
	case "P":
		return "Pro"
	case "H":
		return "Hibryd"
	case "D":
		return "Dominator"
	case "T":
		return "Titanator"
	default:
		return code
	}
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
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := s.baseData(r)
	data["Active"] = "map"
	data["ID"] = m.ID
	data["FieldNames"] = mapFieldNames
	data["SerialLabel"] = m.SerialLabel()
	// Заголовок не локализуется: "MAP <модель> s/n <sn> порт <port>".
	data["Title"] = "MAP " + mapModelFull(m.Model) + " s/n " + m.SerialLabel() + " порт " + m.Port
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
	payload := mapRawPayload(id)
	if payload == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// handleAPIDevices — список подключённых устройств для динамического меню
// (под авторизацией). Сейчас — МАП (type="map"); в перспективе сюда добавятся
// КЭС (type="mppt") и др.
func (s *Server) handleAPIDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	type device struct {
		Type  string `json:"type"`
		ID    int    `json:"id"`
		Label string `json:"label"`
	}
	out := make([]device, 0, 8)
	for _, m := range loadMapList().Maps {
		out = append(out, device{Type: "map", ID: m.ID, Label: m.MenuLabel()})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
