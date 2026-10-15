package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Страницы BMS: /bms/settings (шлюзы IP:PORT + режим) и /bms/monitor (заготовка).
// Конфиг — /malina/etc/malina/bms-gateways.json (тот же, что читает bms-reader);
// сохранение → запись конфига + reload демона через control-сокет (SIGHUP-эквивалент).

type bmsGateway struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Mode string `json:"mode"` // "modbus" | "none"
}

type bmsGatewaysFile struct {
	Gateways []bmsGateway `json:"gateways"`
}

func (s *Server) bmsCfgPath() string {
	if s.opts.BmsConfigPath != "" {
		return s.opts.BmsConfigPath
	}
	return "/malina/etc/malina/bms-gateways.json"
}

func (s *Server) bmsSock() string {
	if s.opts.BmsControlSock != "" {
		return s.opts.BmsControlSock
	}
	return "/run/malina-bms/control.sock"
}

func loadBmsGateways(path string) (bmsGatewaysFile, error) {
	var f bmsGatewaysFile
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if len(b) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	return f, nil
}

// saveBmsGateways — атомарная запись (temp + rename), как у остальных конфигов.
func saveBmsGateways(path string, f bmsGatewaysFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bms-gw-*")
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

// handleBmsSettings — страница настройки шлюзов (GET) и сохранение (POST).
func (s *Server) handleBmsSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	initPage(w)
	cfg, err := loadBmsGateways(s.bmsCfgPath())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := s.baseData(r)
	data["Active"] = "bms_settings"

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		gws, verr := parseBmsForm(r)
		if verr != "" {
			data["Err"] = verr
			data["Gateways"] = gws
		} else {
			cfg.Gateways = gws
			if err := saveBmsGateways(s.bmsCfgPath(), cfg); err != nil {
				data["Err"] = err.Error()
				data["Gateways"] = gws
			} else {
				// reload демона (не критично, если он сейчас не запущен)
				_, _ = controlCall(s.bmsSock(), []byte(`{"cmd":"reload"}`))
				http.Redirect(w, r, "/bms/settings?ok=1", http.StatusSeeOther)
				return
			}
		}
	} else {
		data["Gateways"] = cfg.Gateways
		if r.URL.Query().Get("ok") != "" {
			data["Ok"] = s.tr(s.lang(r), "bms.saved")
		}
	}
	s.render(w, http.StatusOK, "bms_settings", data)
}

// handleBmsMonitor — страница «Монитор» (пока пустая).
func (s *Server) handleBmsMonitor(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	initPage(w)
	data := s.baseData(r)
	data["Active"] = "bms_monitor"
	s.render(w, http.StatusOK, "bms_monitor", data)
}

// parseBmsForm собирает шлюзы из формы; возвращает текст ошибки (или "").
func parseBmsForm(r *http.Request) ([]bmsGateway, string) {
	hosts := r.Form["host"]
	ports := r.Form["port"]
	modes := r.Form["mode"]
	get := func(v []string, i int) string {
		if i < len(v) {
			return strings.TrimSpace(v[i])
		}
		return ""
	}
	var out []bmsGateway
	for i := range hosts {
		host := get(hosts, i)
		if host == "" {
			continue // пустая строка — пропускаем
		}
		port, perr := strconv.Atoi(get(ports, i))
		if perr != nil || port < 1 || port > 65535 {
			return out, "bms.err_port"
		}
		mode := get(modes, i)
		if mode != "modbus" && mode != "none" {
			mode = "none"
		}
		out = append(out, bmsGateway{Host: host, Port: port, Mode: mode})
	}
	return out, ""
}

// initPage — базовый заголовок no-store (как у admin-страниц).
func initPage(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
