package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/authstore"
	"github.com/galiy/malina-26/web/internal/i18n"
	"github.com/galiy/malina-26/web/internal/session"
	"github.com/galiy/malina-26/web/internal/sysinfo"
)

// Options задаёт параметры сервера.
type Options struct {
	Addr          string
	AssetsDir     string
	Auth          auth.Authenticator
	User          string
	Langs         []string
	DefaultLang   string
	SessionIdle   time.Duration // скользящий TTL сессий/токенов
	AuthStorePath string        // /dev/shm/malina-auth
	ControlSock   string        // control-сокет map-reader
}

// Server — HTTP-сервер.
type Server struct {
	opts    Options
	auth    auth.Authenticator
	store   *authstore.Store
	bundle  *i18n.Bundle
	tmpl    map[string]*template.Template
	sampler *sysinfo.Sampler
	initial string
	legacy  *legacyService
}

// ctxKey — ключ контекста «запрос авторизован».
type ctxKey struct{}

// authKey — значение в контексте, если middleware подтвердил сессию/токен.
var authKey = ctxKey{}

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
	if o.SessionIdle == 0 {
		o.SessionIdle = time.Hour
	}
	if o.AuthStorePath == "" {
		o.AuthStorePath = "/dev/shm/malina-auth"
	}
	if o.ControlSock == "" {
		o.ControlSock = "/run/malina-map/control.sock"
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
		"map":      "map.html",
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

	store, err := authstore.Open(o.AuthStorePath, o.SessionIdle)
	if err != nil {
		return nil, err
	}

	return &Server{
		opts:    o,
		auth:    o.Auth,
		store:   store,
		bundle:  bundle,
		tmpl:    sets,
		sampler: sysinfo.New(),
		initial: o.DefaultLang,
		legacy:  newLegacyService(),
	}, nil
}

// Run запускает HTTP-сервер.
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/lang", s.handleLang)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/img/malina_small.gif", http.StatusFound)
	})
	mux.HandleFunc("/api/system", s.handleAPISystem)
	mux.HandleFunc("/api/processes", s.handleAPIProcesses)
	mux.HandleFunc("/api/log", s.handleAPILog)
	mux.HandleFunc("/system", s.handleSystem)
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/system/host", s.handleSystemHost)
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
	mux.HandleFunc("/map", s.handleMapPage)
	mux.HandleFunc("/api/map/devices", s.handleAPIMapDevices)
	mux.HandleFunc("/api/map/raw", s.handleAPIMapRaw)
	mux.HandleFunc("/api/map/write", s.handleAPIMapWrite)
	mux.HandleFunc("/api/token", s.handleAPIToken)
	mux.HandleFunc("/read_json.php", s.handleReadJSON)
	mux.HandleFunc("/read_memory.php", s.handleReadMemory)
	mux.HandleFunc("/check_daemons.php", s.handleCheckDaemons)
	mux.HandleFunc("/", s.handleIndex)

	go s.legacy.run()

	staticDir := filepath.Join(s.opts.AssetsDir, "static")
	staticFS := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", cacheStatic(staticFS)))

	srv := &http.Server{
		Addr:              s.opts.Addr,
		Handler:           cleanSlashes(s.authMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// tokenFrom извлекает токен из cookie или `Authorization: Bearer`.
func tokenFrom(r *http.Request) (tok string, fromCookie bool, ok bool) {
	if c, err := r.Cookie(session.CookieName); err == nil && c.Value != "" {
		return c.Value, true, true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if t := strings.TrimSpace(h[len("Bearer "):]); t != "" {
			return t, false, true
		}
	}
	return "", false, false
}

// setSessionCookie выставляет/продлевает cookie сессии (скользящий TTL).
func (s *Server) setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    tok,
		Path:     "/",
		MaxAge:   int(s.opts.SessionIdle / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// authMiddleware подтверждает сессию/токен (`cookie || Bearer`), продлевает срок
// (скользящий TTL, cookie при этом обновляется) и кладёт признак авторизации в
// контекст запроса. Cookie и Bearer проверяются независимо: невалидная cookie не
// блокирует корректный Bearer и наоборот.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cookieTok, bearerTok string
		if c, err := r.Cookie(session.CookieName); err == nil {
			cookieTok = c.Value
		}
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			bearerTok = strings.TrimSpace(h[len("Bearer "):])
		}
		authed := false
		if cookieTok != "" && s.store.Touch(cookieTok) {
			s.setSessionCookie(w, cookieTok) // скользящий TTL
			authed = true
		} else if bearerTok != "" && s.store.Touch(bearerTok) {
			authed = true
		}
		if authed {
			r = r.WithContext(context.WithValue(r.Context(), authKey, true))
		}
		next.ServeHTTP(w, r)
	})
}

// cleanSlashes устраняет дублирующиеся слэши в пути запроса. Legacy
// request_node.php формирует строку как "GET /"+dev, где dev уже начинается
// со "/", т.е. приходит "//read_json.php?device=map" и "//check_daemons.php".
// http.ServeMux считает такой путь «грязным» и отвечает 302-редиректом, из-за
// чего совместимые потребители получают тело редиректа вместо JSON.
func cleanSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "//") {
			for strings.Contains(r.URL.Path, "//") {
				r.URL.Path = strings.ReplaceAll(r.URL.Path, "//", "/")
			}
		}
		next.ServeHTTP(w, r)
	})
}

// cacheStatic разрешает обычное кэширование статики (URL версионируется
// через ?v=<mtime>, поэтому обновления подтягиваются при смене версии).
// Неверсионированные файлы (legacy-панель) не кэшируем — иначе правки не видны.
func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}
