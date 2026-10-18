package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Native REST для BMS-Receiver: /api/bms/{devices,raw} — по аналогии с
// /api/kes/* и /api/map/*. Контракт — список (bms-list) + бинарный payload слота
// (107 Б), см. docs/bms-reader.md §1, docs/bms-reader-arch.md §12.
// Только чтение (BMS строго RO). Авторизация — как у /api/kes/*: cookie || Bearer.

// handleAPIBmsDevices — список подключённых BMS.
func (s *Server) handleAPIBmsDevices(w http.ResponseWriter, r *http.Request) {
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
		Type         string `json:"type"`
		ID           int    `json:"id"`     // индекс слота
		Gen          uint64 `json:"gen"`    // поколение слота
		Serial       string `json:"serial"` // логический ID (BASE), может совпадать
		Base         string `json:"base"`
		BmsType      string `json:"bms_type"`
		BmsTypeLabel string `json:"bms_type_label"` // локализованная метка типа
		Source       string `json:"source"`
		Framing      int    `json:"framing"`
		LastSeen     int64  `json:"last_seen"`
	}
	tr := s.trFor(r)
	out := make([]device, 0, bmsSlots)
	for _, b := range loadBmsList().Bms {
		out = append(out, device{
			Type: "bms", ID: b.ID, Gen: b.Gen, Serial: b.Serial, Base: b.Base,
			BmsType: b.TypeName, BmsTypeLabel: bmsTypeLabel(b.TypeID, tr),
			Source: b.Source, Framing: b.Framing, LastSeen: b.LastSeen,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// handleAPIBmsRaw — бинарный payload слота BMS (107 Б), как /api/kes/raw.
func (s *Server) handleAPIBmsRaw(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if idStr == "" || err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	g, err := strconv.ParseUint(r.URL.Query().Get("gen"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if b, ok := findBms(id); !ok || b.Gen != g {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	payload := bmsRawPayload(id, g)
	if payload == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// findBms возвращает запись списка BMS по номеру слота.
func findBms(id int) (BmsInfo, bool) {
	for _, b := range loadBmsList().Bms {
		if b.ID == id {
			return b, true
		}
	}
	return BmsInfo{}, false
}
