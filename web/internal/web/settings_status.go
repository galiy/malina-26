package web

import (
	"encoding/json"
	"net/http"
	"time"
)

// settingsStatusRow — универсальная строка карточки статуса на страницах
// настроек. Набор полей зависит от типа; клиент выбирает колонки.
type settingsStatusRow map[string]any

func statusFresh(last, now int64) int {
	if last > 0 && now-last <= 60 {
		return 1
	}
	return 0
}

func (s *Server) handleAPISettingsStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "method not allowed"})
		return
	}
	if _, ok := s.currentUser(r); !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
		return
	}
	typ := r.URL.Query().Get("type")
	now := time.Now().Unix()
	rows := []settingsStatusRow{}
	switch typ {
	case "map":
		for _, m := range loadMapList().Maps {
			rows = append(rows, settingsStatusRow{
				"id": m.ID, "gen": m.Gen, "port": m.Port, "model": m.Model,
				"protocol": m.Protocol, "unit": m.Unit, "baud": m.Baud,
				"serial": m.SerialLabel(), "serial_letter": m.SerialLetter,
				"state": statusFresh(m.LastSeen, now), "last_seen": m.LastSeen,
			})
		}
	case "kes":
		for _, k := range loadKesList().Kes {
			rows = append(rows, settingsStatusRow{
				"id": k.ID, "gen": k.Gen, "port": k.Port, "baud": k.Baud,
				"serial": k.SerialLabel(), "state": statusFresh(k.LastSeen, now),
				"last_seen": k.LastSeen,
			})
		}
	case "bms":
		for _, b := range loadBmsList().Bms {
			rows = append(rows, settingsStatusRow{
				"id": b.ID, "gen": b.Gen, "base": b.Base, "source": b.Source,
				"serial": b.Serial, "type": b.TypeName, "framing": b.Framing,
				"state": func() int {
					if b.State == 1 && statusFresh(b.LastSeen, now) == 1 {
						return 1
					}
					return 0
				}(), "last_seen": b.LastSeen,
			})
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "type required"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"type": typ, "updated": now, "rows": rows})
}
