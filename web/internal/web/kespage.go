package web

import (
	"fmt"
	"net/http"
	"strconv"
)

// handleKesPage — страница КЭС (заготовка): заголовок, отключение обновлений,
// вкладки-заглушки и чёрное мемо с распакованным payload (kes.js).
// По образцу handleMapPage (maps.go): авторизация, GET, id+gen через findKes.
func (s *Server) handleKesPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	k, ok := findKes(id)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// Если задан gen — он должен совпасть (защита от переиспользования слота).
	if gs := r.URL.Query().Get("gen"); gs != "" {
		if g, err := strconv.ParseUint(gs, 10, 64); err != nil || g != k.Gen {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	data := s.baseData(r)
	data["Active"] = "kes"
	data["ID"] = k.ID
	data["Gen"] = k.Gen
	// Заголовок: "КЭС s/n <sn> порт <port>" (шаблон локализуется).
	data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "kes.title"), k.SerialLabel(), k.Port)
	s.render(w, http.StatusOK, "kes", data)
}
