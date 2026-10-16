package web

import "net/http"

// Заглушки страниц «Настройка» для МАП и КЭС (в разработке).
// Подпункт меню появляется всегда; содержимое — заготовка (dev.stub).

func (s *Server) handleMapSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data := s.baseData(r)
	data["Active"] = "map_settings"
	data["Title"] = s.tr(s.lang(r), "menu.map") + " — " + s.tr(s.lang(r), "menu.map_settings")
	s.render(w, http.StatusOK, "page_stub", data)
}

func (s *Server) handleKesSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data := s.baseData(r)
	data["Active"] = "kes_settings"
	data["Title"] = s.tr(s.lang(r), "menu.kes") + " — " + s.tr(s.lang(r), "menu.kes_settings")
	s.render(w, http.StatusOK, "page_stub", data)
}
