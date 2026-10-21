package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// readerCfg — настройки reader-демона (map-reader.json / kes-reader.json):
// включён/выключен опрос + уровень логирования.
type readerCfg struct {
	Enabled  bool   `json:"enabled"`
	LogLevel string `json:"log_level"`
}

// loadReaderCfg читает конфиг; при отсутствии/невалидности — умолчание
// (defEnabled, info).
func loadReaderCfg(path string, defEnabled bool) readerCfg {
	c := readerCfg{Enabled: defEnabled, LogLevel: "info"}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return c
	}
	if json.Unmarshal(b, &c) != nil {
		return readerCfg{Enabled: defEnabled, LogLevel: "info"}
	}
	if lv, ok := normalizeBmsProxyLevel(c.LogLevel); ok {
		c.LogLevel = lv
	} else {
		c.LogLevel = "info"
	}
	return c
}

// saveReaderCfg — атомарная запись (temp + rename), как у остальных конфигов.
func saveReaderCfg(path string, c readerCfg) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".reader-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// handleMapSettings — страница/сохранение настроек map-reader (Включено + лог).
func (s *Server) handleMapSettings(w http.ResponseWriter, r *http.Request) {
	s.readerSettings(w, r, "map", "menu.map", "menu.map_settings", s.opts.MapReaderConfigPath, "/malina/etc/malina/map-reader.json", true, s.opts.ControlSock, "/run/malina-map/control.sock", "map_settings")
}

// handleKesSettings — страница/сохранение настроек kes-reader.
func (s *Server) handleKesSettings(w http.ResponseWriter, r *http.Request) {
	s.readerSettings(w, r, "kes", "menu.kes", "menu.kes_settings", s.opts.KesReaderConfigPath, "/malina/etc/malina/kes-reader.json", false, s.opts.KesControlSock, "/run/malina-kes/control.sock", "kes_settings")
}

// readerSettings — общий код страницы настроек reader'а: GET — форма, POST —
// сохранить map-reader.json/kes-reader.json и подать демону reload.
func (s *Server) readerSettings(w http.ResponseWriter, r *http.Request, prefix, menuKey, titleKey, cfgPath, defPath string, defEnabled bool, sock, defSock, active string) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if cfgPath == "" {
		cfgPath = defPath
	}
	if sock == "" {
		sock = defSock
	}
	c := loadReaderCfg(cfgPath, defEnabled)
	data := s.baseData(r)
	data["Active"] = active
	data["Title"] = s.tr(s.lang(r), menuKey) + " — " + s.tr(s.lang(r), titleKey)
	data["Levels"] = bmsProxyLevels
	data["Enabled"] = c.Enabled
	data["Level"] = c.LogLevel
	if prefix == "map" {
		// Синхронизация времени МАП живёт на странице настройки МАП.
		data["MapTimeSyncEnabled"] = timeSyncEnabled(s.opts.MapTimeSyncOff)
	}

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		en := r.Form.Get(prefix+"_enabled") == "on"
		lv, ok := normalizeBmsProxyLevel(r.Form.Get(prefix + "_log_level"))
		ms := r.Form.Get(prefix+"_maptime_enabled") == "on"
		saved := false
		switch {
		case !ok:
			data["Err"] = s.tr(s.lang(r), "bms.proxy_bad_level")
		case prefix == "map" && setTimeSyncEnabled(s.opts.MapTimeSyncOff, ms) != nil:
			data["Err"] = s.tr(s.lang(r), "sys.err.maptime")
		default:
			if err := saveReaderCfg(cfgPath, readerCfg{Enabled: en, LogLevel: lv}); err != nil {
				data["Err"] = err.Error()
			} else {
				_, _ = controlCall(sock, []byte(`{"cmd":"reload"}`))
				saved = true
			}
		}
		if saved {
			http.Redirect(w, r, r.URL.Path+"?ok=1", http.StatusSeeOther)
			return
		}
		data["Enabled"] = en
		data["Level"] = lv
		if prefix == "map" {
			data["MapTimeSyncEnabled"] = ms
		}
	} else if qo := r.URL.Query().Get("ok"); qo != "" {
		data["Ok"] = s.tr(s.lang(r), "bms.saved")
	}
	s.render(w, http.StatusOK, active, data)
}
