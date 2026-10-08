package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/i18n"
	"github.com/galiy/malina-26/web/internal/session"
	"github.com/galiy/malina-26/web/internal/sysinfo"
)

// Options задаёт параметры сервера.
type Options struct {
	Addr        string
	AssetsDir   string
	Auth        auth.Authenticator
	User        string
	Langs       []string
	DefaultLang string
	SessionTTL  time.Duration
}

// Server — HTTP-сервер.
type Server struct {
	opts    Options
	auth    auth.Authenticator
	sess    *session.Manager
	bundle  *i18n.Bundle
	tmpl    map[string]*template.Template
	sampler *sysinfo.Sampler
	initial string
}

// langNames — нативные названия языков для селектора.
var langNames = map[string]string{
	"ru": "Русский",
	"be": "Беларуская",
	"zh": "中文",
	"en": "English",
}

// New собирает сервер, загружая шаблоны/i18n из AssetsDir.
func New(o Options) (*Server, error) {
	if o.Langs == nil {
		o.Langs = []string{"ru", "be", "zh", "en"}
	}
	if o.DefaultLang == "" {
		o.DefaultLang = "en"
	}
	if o.SessionTTL == 0 {
		o.SessionTTL = 12 * time.Hour
	}
	if o.AssetsDir == "" {
		o.AssetsDir = "/malina/usr/local/share/malina/web"
	}
	// Источник строк — русский; недостающие ключи добираются из него.
	bundle, err := i18n.Load(os.DirFS(o.AssetsDir), "i18n", "ru", o.Langs)
	if err != nil {
		return nil, err
	}
	tplDir := filepath.Join(o.AssetsDir, "templates")
	layout := filepath.Join(tplDir, "layout.html")

	funcs := template.FuncMap{
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"list": func(items ...string) []string { return items },
		// asset добавляет версию (?v=<mtime>) для cache-busting статики.
		"asset": func(p string) string {
			fp := filepath.Join(o.AssetsDir, "static", p)
			if fi, err := os.Stat(fp); err == nil {
				return "/static/" + p + "?v=" + strconv.FormatInt(fi.ModTime().Unix(), 10)
			}
			return "/static/" + p
		},
	}

	sets := map[string]*template.Template{}
	for name, page := range map[string]string{
		"info":     "info.html",
		"system":   "system.html",
		"logs":     "logs.html",
		"network":  "network.html",
		"services": "services.html",
		"reboot":   "reboot.html",
	} {
		t, err := template.New("").Funcs(funcs).
			ParseFiles(layout, filepath.Join(tplDir, page))
		if err != nil {
			return nil, err
		}
		sets[name] = t
	}
	login, err := template.New("").Funcs(funcs).
		ParseFiles(filepath.Join(tplDir, "login.html"))
	if err != nil {
		return nil, err
	}
	sets["login"] = login

	return &Server{
		opts:    o,
		auth:    o.Auth,
		sess:    session.NewManager(o.SessionTTL),
		bundle:  bundle,
		tmpl:    sets,
		sampler: sysinfo.New(),
		initial: o.DefaultLang,
	}, nil
}

// Run запускает HTTP-сервер.
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/lang", s.handleLang)
	mux.HandleFunc("/api/system", s.handleAPISystem)
	mux.HandleFunc("/api/log", s.handleAPILog)
	mux.HandleFunc("/system", s.handleSystem)
	mux.HandleFunc("/system/hostname", s.handleSystemHostname)
	mux.HandleFunc("/system/logging", s.handleSystemLogging)
	mux.HandleFunc("/system/password", s.handleSystemPassword)
	mux.HandleFunc("/services", s.handleServices)
	mux.HandleFunc("/api/services", s.handleAPIServices)
	mux.HandleFunc("/services/action", s.handleServiceAction)
	mux.HandleFunc("/system/reboot", s.handleSystemReboot)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/api/scan", s.handleAPIScan)
	mux.HandleFunc("/network", s.handleNetwork)
	mux.HandleFunc("/network/save", s.handleNetworkSave)
	mux.HandleFunc("/network/scan", s.handleNetworkScan)
	mux.HandleFunc("/", s.handleIndex)

	staticDir := filepath.Join(s.opts.AssetsDir, "static")
	staticFS := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", cacheStatic(staticFS)))

	srv := &http.Server{
		Addr:              s.opts.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// cacheStatic разрешает обычное кэширование статики (URL версионируется
// через ?v=<mtime>, поэтому обновления подтягиваются при смене версии).
func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}
