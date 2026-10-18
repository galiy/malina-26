package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/web/internal/audit"
	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/authstore"
	"github.com/galiy/malina-26/web/internal/events"
	"github.com/galiy/malina-26/web/internal/i18n"
	"github.com/galiy/malina-26/web/internal/mailer"
	"github.com/galiy/malina-26/web/internal/masternode"
	"github.com/galiy/malina-26/web/internal/session"
	"github.com/galiy/malina-26/web/internal/snmp"
	"github.com/galiy/malina-26/web/internal/sysinfo"
)

// Options задаёт параметры сервера.
type Options struct {
	Addr                string
	AssetsDir           string
	Auth                auth.Authenticator
	User                string
	Langs               []string
	DefaultLang         string
	SessionIdle         time.Duration // скользящий TTL сессий/токенов
	AuthStorePath       string        // /dev/shm/malina-auth
	ControlSock         string        // control-сокет map-reader
	KesControlSock      string        // control-сокет kes-reader
	BasicCacheTTL       time.Duration // TTL кэша успешных Basic-проверок (legacy)
	MapTimeSyncOff      string        // флаг-файл отключения синхронизации времени МАП
	AuditEnabled        bool          // аудит-журнал действий (syslog)
	AuditTag            string        // syslog-тег аудита (по умолчанию malina-web-audit)
	MasterConfigPath    string        // путь к nodes.json (мастер-узел)
	MasterCacheDir      string        // каталог кэша мастер-узла (tmpfs)
	MasterTTL           time.Duration // TTL кэша мастер-узла (по умолчанию 5s)
	EmailConfigPath     string        // путь к email.json
	EmailKeyPath        string        // путь к ключу шифрования секрета
	EmailOutboxDir      string        // каталог аутбокса (tmpfs)
	EventsConfigPath    string        // путь к events.json (менеджер событий)
	EventsStatePath     string        // путь к состоянию правил (tmpfs)
	EventsLogTag        string        // syslog-тег лога менеджера событий
	SNMPPort            int           // UDP-порт SNMP-агента (161)
	SNMPCommunity       string        // community агента (public)
	SNMPMonitoring      string        // путь к настройке периодических трапов
	AccConfigPath       string        // настройка аккумулятора (json)
	TsdbRawRoot         string        // каталог raw-tsdb (перечисление устройств)
	TsdbRawSock         string        // сокет raw-tsdb
	TsdbSock            string        // сокет avg-tsdb
	TsdbAvgRoot         string        // каталог avg-tsdb (перечисление устройств)
	BmsConfigPath       string        // путь к bms-reader.json (BMS-Receiver)
	BmsControlSock      string        // control-сокет bms-reader (reload)
	BmsProxyConfigPath  string        // путь к bms-proxy.json (BMS-Proxy)
	BmsProxyControlSock string        // control-сокет bms-proxy (reload)
	MapReaderConfigPath string        // путь к map-reader.json (вкл/выкл + лог)
	KesReaderConfigPath string        // путь к kes-reader.json (вкл/выкл + лог)
}

// Server — HTTP-сервер.
type Server struct {
	opts    Options
	auth    auth.Authenticator
	basic   *auth.BasicCache
	store   *authstore.Store
	bundle  *i18n.Bundle
	tmpl    map[string]*template.Template
	sampler *sysinfo.Sampler
	initial string
	legacy  *legacyService
	audit   *audit.Logger

	masterPath  string
	masterProxy *masternode.Proxy
	// Кэш конфига узлов (перечитываем только при изменении файла).
	masterMu      sync.Mutex
	masterCfg     masternode.Config
	masterCfgMod  time.Time
	masterCfgSize int64
	masterCfgOK   bool

	// Кэш периода агрегации avg (accumulator.json): горячий путь графиков
	// (каждый zoom-перезапрос) не должен читать/парсить файл.
	accMu     sync.Mutex
	accMod    time.Time
	accSize   int64
	accPeriod int
	accOK     bool

	email *mailer.Mailer

	eventsPath   string
	eventsEngine *events.Engine

	snmpAgent      *snmp.Agent
	monitoringPath string
}

// serverStart — момент старта процесса (аптайм для sysUpTime в trap).
var serverStart = time.Now()

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
	"ka": "ქართული",
	"kk": "Қазақша",
	"uz": "O'zbekcha",
	"tg": "Тоҷикӣ",
}

// New собирает сервер, загружая шаблоны/i18n из AssetsDir.
func New(o Options) (*Server, error) {
	if o.Langs == nil {
		o.Langs = []string{"ru", "be", "zh", "en", "ka", "kk", "uz", "tg"}
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
	if o.KesControlSock == "" {
		o.KesControlSock = "/run/malina-kes/control.sock"
	}
	if o.MapReaderConfigPath == "" {
		o.MapReaderConfigPath = "/malina/etc/malina/map-reader.json"
	}
	if o.KesReaderConfigPath == "" {
		o.KesReaderConfigPath = "/malina/etc/malina/kes-reader.json"
	}
	if o.BasicCacheTTL == 0 {
		o.BasicCacheTTL = 5 * time.Minute
	}
	if o.MapTimeSyncOff == "" {
		o.MapTimeSyncOff = "/malina/etc/malina/map-time-sync.off"
	}
	if o.AssetsDir == "" {
		o.AssetsDir = "/malina/usr/local/share/malina/web"
	}
	if o.MasterConfigPath == "" {
		o.MasterConfigPath = masternode.DefaultPath
	}
	if o.MasterCacheDir == "" {
		o.MasterCacheDir = masternode.DefaultCacheDir
	}
	if o.EmailConfigPath == "" {
		o.EmailConfigPath = mailer.DefaultConfigPath
	}
	if o.EmailKeyPath == "" {
		o.EmailKeyPath = mailer.DefaultKeyPath
	}
	if o.EmailOutboxDir == "" {
		o.EmailOutboxDir = mailer.DefaultOutboxDir
	}
	if o.EventsConfigPath == "" {
		o.EventsConfigPath = events.DefaultPath
	}
	if o.EventsLogTag == "" {
		o.EventsLogTag = "malina-events"
	}
	if o.EventsStatePath == "" {
		o.EventsStatePath = events.DefaultStatePath
	}
	if o.SNMPPort == 0 {
		o.SNMPPort = 161
	}
	if o.SNMPCommunity == "" {
		o.SNMPCommunity = "public"
	}
	if o.SNMPMonitoring == "" {
		o.SNMPMonitoring = DefaultMonitoringPath
	}
	if o.AccConfigPath == "" {
		o.AccConfigPath = "/malina/etc/malina/accumulator.json"
	}
	if o.TsdbRawRoot == "" {
		o.TsdbRawRoot = "/run/malina-ts-raw"
	}
	if o.TsdbRawSock == "" {
		o.TsdbRawSock = "/run/malina-ts-raw/tsdb.sock"
	}
	if o.TsdbSock == "" {
		o.TsdbSock = "/run/malina-ts/tsdb.sock"
	}
	if o.TsdbAvgRoot == "" {
		o.TsdbAvgRoot = "/malina/var/malina-ts"
	}
	if o.BmsConfigPath == "" {
		o.BmsConfigPath = "/malina/etc/malina/bms-reader.json"
	}
	if o.BmsControlSock == "" {
		o.BmsControlSock = "/run/malina-bms/control.sock"
	}
	if o.BmsProxyConfigPath == "" {
		o.BmsProxyConfigPath = "/malina/etc/malina/bms-proxy.json"
	}
	if o.BmsProxyControlSock == "" {
		o.BmsProxyControlSock = "/run/malina-bms-proxy/control.sock"
	}
	// Источник строк — русский; недостающие ключи добираются из него.
	bundle, err := i18n.Load(os.DirFS(o.AssetsDir), "i18n", "ru", o.Langs)
	if err != nil {
		return nil, err
	}
	tplDir := filepath.Join(o.AssetsDir, "templates")
	layout := filepath.Join(tplDir, "layout.html")

	funcs := template.FuncMap{
		// safeHTML — доверенный HTML (i18n-строки с ссылками в инструкциях).
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"list": func(items ...string) []string { return items },
		// asset — абсолютный путь к статике (/static/...), с версией
		// (?v=<mtime>) для cache-busting.
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
		"home":            "home.html",
		"info":            "info.html",
		"system":          "system.html",
		"logs":            "logs.html",
		"network":         "network.html",
		"services":        "services.html",
		"map":             "map.html",
		"kes":             "kes.html",
		"reboot":          "reboot.html",
		"shutdown":        "shutdown.html",
		"master":          "master.html",
		"master_settings": "master_settings.html",
		"map_settings":    "map_settings.html",
		"kes_settings":    "kes_settings.html",
		"bms_settings":    "bms_settings.html",
		"bms":             "bms.html",
		"email":           "email.html",
		"events":          "events.html",
		"stats_charts":    "stats_charts.html",
		"stats_tables":    "stats_tables.html",
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

	// iframe-содержимое мастер-узла — standalone-шаблон без layout.
	panel, err := template.New("").Funcs(funcs).
		ParseFiles(filepath.Join(tplDir, "masterpanel.html"))
	if err != nil {
		return nil, err
	}
	sets["masterpanel"] = panel

	store, err := authstore.Open(o.AuthStorePath, o.SessionIdle)
	if err != nil {
		return nil, err
	}

	aud := audit.New(o.AuditEnabled, o.AuditTag)
	// Истечение простаивающих сессий/токенов — в аудит (через reaper/Touch).
	store.SetOnExpire(func(prefix string, lastUsed time.Time) {
		aud.Event("session_expire", map[string]any{
			"token":      prefix,
			"idle_since": lastUsed.UTC().Format(time.RFC3339),
		})
	})

	em := mailer.New(o.EmailConfigPath, o.EmailKeyPath, o.EmailOutboxDir)
	em.OnEvent = func(e mailer.Event) {
		aud.Event("email_send", map[string]any{
			"kind": e.Kind, "subject": e.Subject, "recipient": e.Recipient,
			"attempts": e.Attempts, "error": e.Error,
		})
	}

	return &Server{
		opts:    o,
		auth:    o.Auth,
		basic:   auth.NewBasicCache(o.Auth, o.User, o.BasicCacheTTL, 32),
		store:   store,
		bundle:  bundle,
		tmpl:    sets,
		sampler: sysinfo.New(),
		initial: o.DefaultLang,
		legacy:  newLegacyService(o.ControlSock),
		audit:   aud,

		masterPath:  o.MasterConfigPath,
		masterProxy: masternode.NewProxy(o.MasterCacheDir, o.MasterTTL),

		email: em,

		eventsPath:     o.EventsConfigPath,
		monitoringPath: o.SNMPMonitoring,
	}, nil
}

// startEvents запускает движок менеджера событий; результат/срабатывания пишутся
// в отдельный лог (свой тег → отдельный файл на log-хосте).
func (s *Server) startEvents() {
	evLog := audit.New(s.opts.AuditEnabled, s.opts.EventsLogTag)
	s.eventsEngine = events.New(s.eventsPath,
		&mapEventsProvider{s: s}, &eventsActions{s: s},
		func(result string, fields map[string]any) { evLog.Event(result, fields) },
	)
	s.eventsEngine.SetStatePath(s.opts.EventsStatePath)
	lang := s.opts.DefaultLang
	s.eventsEngine.ModeLabel = func(code uint32) string { return s.modeLabelStatic(lang, code) }
	s.eventsEngine.ModeText = func(code, statusCh uint32) string { return s.modeTextDynamic(lang, code, statusCh) }
	go s.eventsEngine.Run(context.Background())
}

// startSNMP запускает SNMP-агент (v2c, UDP) встроенно в web: тот же источник shm
// и общий маппинг MIB с trap. Порт 161 <1024 — web-админка работает от root,
// набор привилегий в юните не ограничивается (иначе ломается смена пароля).
// Ошибка запуска (порт занят/нет CAP) не валит веб — логируем (агент по
// необходимости). Периодическая отправка трапов (runMonitoring) — независимая
// функция, запускается отдельно в Run().
func (s *Server) startSNMP() {
	ag := snmp.NewAgent(fmt.Sprintf(":%d", s.opts.SNMPPort), s.opts.SNMPCommunity, webSnmpHandler{})
	if err := ag.Start(); err != nil {
		s.audit.Event("snmp_agent", map[string]any{"port": s.opts.SNMPPort, "result": "err", "error": err.Error()})
		log.Printf("snmp-agent: не удалось стартовать (%v)", err)
		return
	}
	s.snmpAgent = ag
	s.audit.Event("snmp_agent", map[string]any{"port": s.opts.SNMPPort, "community": s.opts.SNMPCommunity, "result": "ok"})
}

// Run запускает HTTP-сервер.
func (s *Server) Run(ctx context.Context) error {
	s.email.Start()
	defer s.email.Stop()
	s.startEvents()
	s.startSNMP()
	// Периодическая отправка трапов — независимая от агента функция (§2.9).
	go s.runMonitoring(context.Background())
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
	mux.HandleFunc("/system/map-time-sync", s.handleSystemMapTimeSync)
	mux.HandleFunc("/system/accumulator", s.handleSystemAccumulator)
	mux.HandleFunc("/stats/charts", s.handleStatsCharts)
	mux.HandleFunc("/stats/tables", s.handleStatsTables)
	mux.HandleFunc("/api/stats/devices", s.handleAPIStatsDevices)
	mux.HandleFunc("/api/stats/extremes", s.handleAPIStatsExtremes)
	mux.HandleFunc("/api/stats/table", s.handleAPIStatsTable)
	mux.HandleFunc("/api/stats/series", s.handleAPIStatsSeries)
	mux.HandleFunc("/services", s.handleServices)
	mux.HandleFunc("/api/services", s.handleAPIServices)
	mux.HandleFunc("/services/action", s.handleServiceAction)
	mux.HandleFunc("/system/reboot", s.handleSystemReboot)
	mux.HandleFunc("/system/shutdown", s.handleSystemShutdown)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/api/scan", s.handleAPIScan)
	mux.HandleFunc("/master", s.handleMaster)
	mux.HandleFunc("/master/panel", s.handleMasterPanel)
	mux.HandleFunc("/master/settings", s.handleMasterSettings)
	mux.HandleFunc("/bms", s.handleBmsPage)
	mux.HandleFunc("/bms/settings", s.handleBmsSettings)
	mux.HandleFunc("/map/settings", s.handleMapSettings)
	mux.HandleFunc("/kes/settings", s.handleKesSettings)
	mux.HandleFunc("/system/email", s.handleEmail)
	mux.HandleFunc("/system/email/save", s.handleEmailSave)
	mux.HandleFunc("/system/email/test", s.handleEmailTest)
	mux.HandleFunc("/system/email/outbox", s.handleEmailOutbox)
	mux.HandleFunc("/system/events", s.handleEvents)
	mux.HandleFunc("/system/events/save", s.handleEventsSave)
	mux.HandleFunc("/system/monitoring", s.handleSystemMonitoring)
	mux.HandleFunc("/api/master/check_daemons", s.handleAPIMasterCheckDaemons)
	mux.HandleFunc("/api/master/read_json", s.handleAPIMasterReadJSON)
	mux.HandleFunc("/network", s.handleNetwork)
	mux.HandleFunc("/network/save", s.handleNetworkSave)
	mux.HandleFunc("/network/scan", s.handleNetworkScan)
	mux.HandleFunc("/map", s.handleMapPage)
	mux.HandleFunc("/kes", s.handleKesPage)
	mux.HandleFunc("/api/map/devices", s.handleAPIMapDevices)
	mux.HandleFunc("/api/map/raw", s.handleAPIMapRaw)
	mux.HandleFunc("/api/map/write", s.handleAPIMapWrite)
	mux.HandleFunc("/api/kes/devices", s.handleAPIKesDevices)
	mux.HandleFunc("/api/kes/raw", s.handleAPIKesRaw)
	mux.HandleFunc("/api/kes/write", s.handleAPIKesWrite)
	mux.HandleFunc("/api/bms/devices", s.handleAPIBmsDevices)
	mux.HandleFunc("/api/bms/raw", s.handleAPIBmsRaw)
	mux.HandleFunc("/api/mainlist", s.handleAPIMainList)
	mux.HandleFunc("/api/token", s.handleAPIToken)
	mux.HandleFunc("/read_json.php", s.handleReadJSON)
	mux.HandleFunc("/write_eeprom.php", s.handleWriteEEPROM)
	mux.HandleFunc("/read_memory.php", s.handleReadMemory)
	mux.HandleFunc("/read_sec.php", s.handleReadSec)
	mux.HandleFunc("/write_sec.php", s.handleWriteSec)
	mux.HandleFunc("/check_daemons.php", s.handleCheckDaemons)
	mux.HandleFunc("/read_bms.php", s.handleReadBMS)
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
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done(): // graceful: дренаж активных соединений
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// pathOnly убирает схему/хост (http://, https://) — оставляем только путь.
func pathOnly(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return rest[j:]
		}
		return "/"
	}
	return u
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
