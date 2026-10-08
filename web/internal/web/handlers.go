package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/logtail"
	"github.com/galiy/malina-26/web/internal/session"
	"github.com/galiy/malina-26/web/internal/sysctl"
	"github.com/galiy/malina-26/web/internal/sysinfo"
)

const langCookie = "lang"

func (s *Server) lang(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && s.bundle.Has(c.Value) {
		return c.Value
	}
	return s.initial
}

func (s *Server) tr(lang, key string) string {
	return s.bundle.Translations(lang)[key]
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data map[string]any) {
	set := s.tmpl[name]
	if set == nil {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	root := "layout"
	if set.Lookup("layout") == nil {
		root = "login"
	}
	if set.Lookup("reboot") != nil {
		root = "reboot"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := set.ExecuteTemplate(w, root, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) baseData(r *http.Request) map[string]any {
	lang := s.lang(r)
	return map[string]any{
		"Lang":      lang,
		"T":         s.bundle.Translations(lang),
		"Langs":     s.opts.Langs,
		"LangNames": langNames,
		"Path":      r.URL.Path,
	}
}

func (s *Server) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(session.CookieName)
	if err != nil {
		return "", false
	}
	return s.sess.User(c.Value)
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return "", false
	}
	return user, true
}

// captiveProbePaths — типовые пути captive-portal-проб клиентов;
// отвечаем редиректом на нашу страницу, чтобы ОС показала «Войти в сеть».
var captiveProbePaths = map[string]bool{
	"/generate_204":              true,
	"/gen_204":                   true,
	"/hotspot-detect.html":       true,
	"/library/test/success.html": true,
	"/connecttest.txt":           true,
	"/ncsi.txt":                  true,
	"/canonical.html":            true,
	"/success.txt":               true,
	"/connectivity-check.html":   true,
	"/redirect":                  true,
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		if captiveProbePaths[r.URL.Path] {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	data := s.baseData(r)
	data["Active"] = "info"
	data["LogFiles"] = logtail.Whitelist
	s.render(w, http.StatusOK, "info", data)
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	data := s.baseData(r)
	data["Active"] = "system"
	data["Hostname"] = sysctl.CurrentHostname()
	data["HostnameSetting"] = sysctl.HostnameSetting()
	data["RemoteSyslog"] = sysctl.RemoteSyslogSetting()
	lang := s.lang(r)
	if ok := r.URL.Query().Get("ok"); ok != "" {
		data["Ok"] = s.tr(lang, "sys.msg."+ok+"_ok")
	}
	if e := r.URL.Query().Get("err"); e != "" {
		data["Err"] = s.tr(lang, "sys.err."+e)
	}
	s.render(w, http.StatusOK, "system", data)
}

func (s *Server) redirectSystem(w http.ResponseWriter, r *http.Request, key string) {
	http.Redirect(w, r, "/system?"+key, http.StatusSeeOther)
}

func (s *Server) handleSystemHostname(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("hostname"))
	if !sysctl.ValidHostname(name) {
		s.redirectSystem(w, r, "err=hostname")
		return
	}
	if err := sysctl.SetHostname(name); err != nil {
		s.redirectSystem(w, r, "err=generic")
		return
	}
	s.redirectSystem(w, r, "ok=hostname")
}

func (s *Server) handleSystemLogging(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	host := strings.TrimSpace(r.FormValue("host"))
	if host != "" && !sysctl.ValidRemoteHost(host) {
		s.redirectSystem(w, r, "err=logging")
		return
	}
	if err := sysctl.SetRemoteSyslog(host); err != nil {
		s.redirectSystem(w, r, "err=generic")
		return
	}
	s.redirectSystem(w, r, "ok=logging")
}

func (s *Server) handleSystemPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	p1 := r.FormValue("password_new")
	p2 := r.FormValue("password_confirm")
	if p1 != p2 {
		s.redirectSystem(w, r, "err=password_mismatch")
		return
	}
	if len(p1) < 4 {
		s.redirectSystem(w, r, "err=password_short")
		return
	}
	if strings.ContainsAny(p1, "\n\r:") ||
		strings.IndexFunc(p1, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		s.redirectSystem(w, r, "err=password_chars")
		return
	}
	if err := auth.ChangePassword(s.opts.User, p1); err != nil {
		s.redirectSystem(w, r, "err=password")
		return
	}
	s.redirectSystem(w, r, "ok=password")
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	file := r.URL.Query().Get("file")
	if !logtail.Allowed(file) {
		file = "syslog"
	}
	data := s.baseData(r)
	data["Active"] = "info"
	data["File"] = file
	data["Files"] = logtail.Whitelist
	s.render(w, http.StatusOK, "logs", data)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.currentUser(r); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		s.render(w, http.StatusOK, "login", s.baseData(r))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		password := r.FormValue("password")
		if err := s.auth.Authenticate(s.opts.User, password); err != nil {
			data := s.baseData(r)
			data["Error"] = true
			s.render(w, http.StatusUnauthorized, "login", data)
			return
		}
		tok, exp := s.sess.Create(s.opts.User)
		http.SetCookie(w, &http.Cookie{
			Name:     session.CookieName,
			Value:    tok,
			Path:     "/",
			Expires:  exp,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(session.CookieName); err == nil {
		s.sess.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleLang(w http.ResponseWriter, r *http.Request) {
	if l := r.URL.Query().Get("set"); s.bundle.Has(l) {
		http.SetCookie(w, &http.Cookie{
			Name:     langCookie,
			Value:    l,
			Path:     "/",
			MaxAge:   365 * 24 * 3600,
			SameSite: http.SameSiteLaxMode,
		})
	}
	next := r.URL.Query().Get("next")
	// Разрешаем только внутренние пути: одиночный ведущий «/» без «//» или «/\»
	// (иначе получается открытый редирект на внешний хост).
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleAPISystem(w http.ResponseWriter, r *http.Request) {
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
	type apiSystem struct {
		*sysinfo.Info
		Net netState `json:"net"`
	}
	info := s.sampler.Snapshot()
	_ = json.NewEncoder(w).Encode(apiSystem{Info: &info, Net: loadNetState()})
}

func (s *Server) handleAPILog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	file := r.URL.Query().Get("file")
	if !logtail.Allowed(file) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	n := 100
	if v := r.URL.Query().Get("lines"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x > 0 && x <= 500 {
			n = x
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 0 && x <= 1000000 {
			offset = x
		}
	}
	lines, older, ok := logtail.Window(file, offset, n)
	if lines == nil {
		lines = []string{}
	}
	resp := map[string]any{
		"file": file, "lines": lines, "offset": offset,
		"total": offset + len(lines), "has_more": older, "missing": !ok,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
