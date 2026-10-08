package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"time"

	"github.com/galiy/malina-26/web/internal/sysctl"
)

// handleServices рендерит страницу «Система → Сервисы».
func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	data := s.baseData(r)
	data["Active"] = "services"
	data["Services"] = sysctl.ListServices()
	lang := s.lang(r)
	if ok := r.URL.Query().Get("ok"); ok != "" {
		data["Ok"] = s.tr(lang, "svc.msg."+ok+"_ok")
	}
	if e := r.URL.Query().Get("err"); e != "" {
		data["Err"] = s.tr(lang, "svc.err."+e)
	}
	s.render(w, http.StatusOK, "services", data)
}

// handleAPIServices отдаёт JSON-список управляемых служб (для автообновления).
func (s *Server) handleAPIServices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"services": sysctl.ListServices()})
}

func (s *Server) redirectServices(w http.ResponseWriter, r *http.Request, key string) {
	http.Redirect(w, r, "/services?"+key, http.StatusSeeOther)
}

// handleServiceAction запускает/останавливает управляемую службу.
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	unit := r.FormValue("unit")
	action := r.FormValue("action")
	if err := sysctl.ServiceAction(unit, action); err != nil {
		switch {
		case errors.Is(err, sysctl.ErrNotManaged):
			s.redirectServices(w, r, "err=unit")
		case errors.Is(err, sysctl.ErrBadAction):
			s.redirectServices(w, r, "err=action")
		default:
			s.redirectServices(w, r, "err=failed")
		}
		return
	}
	s.redirectServices(w, r, "ok="+action)
}

// handleSystemReboot инициирует перезагрузку устройства.
func (s *Server) handleSystemReboot(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Сначала отдаём страницу ожидания (общий стиль, скрипт переподключения),
	// затем через короткую паузу инициируем перезагрузку.
	s.render(w, http.StatusOK, "reboot", s.baseData(r))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		_ = exec.Command("systemctl", "reboot").Run()
	}()
}
