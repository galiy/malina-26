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
	sn := r.URL.Query().Get("serial")
	k, ok := findKes(id)
	// Устаревший id/gen (устройство переоткрылось): ищем по серийнику из запроса.
	if !ok && sn != "" {
		k, ok = findKesBySerial(sn)
	}
	data := s.baseData(r)
	data["Active"] = "kes"
	if ok {
		data["ID"] = k.ID // актуальные id/gen найденного устройства
		data["Gen"] = k.Gen
		data["Serial"] = k.SerialLabel()
		data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "kes.title"), k.SerialLabel(), k.Port)
	} else if sn != "" {
		// Устройство ещё не обнаружено (после старта/деплоя) — отдаём страницу-
		// ожидание; клиент сам повторит поиск по серийнику (до ~60 c).
		data["ID"] = id
		data["Gen"] = 0
		data["Serial"] = sn
		data["Title"] = fmt.Sprintf(s.tr(s.lang(r), "kes.title"), sn, "")
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "kes", data)
}
