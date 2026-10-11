package web

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/galiy/malina-26/web/internal/masternode"
)

// noFrameHeaders — запрет внешнего фрейминга страниц мастер-узла (ant-clickjacking).
func noFrameHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
}

// masterConfig — конфиг узлов с кэшем по mtime/size (файл не читаем на каждый запрос).
func (s *Server) masterConfig() (masternode.Config, error) {
	s.masterMu.Lock()
	defer s.masterMu.Unlock()
	fi, err := os.Stat(s.masterPath)
	if err != nil {
		if os.IsNotExist(err) {
			return masternode.Config{Version: 1}, nil
		}
		return masternode.Config{}, err
	}
	if s.masterCfgOK && fi.ModTime().Equal(s.masterCfgMod) && fi.Size() == s.masterCfgSize {
		return s.masterCfg, nil
	}
	c, err := masternode.Load(s.masterPath)
	if err != nil {
		return c, err
	}
	s.masterCfg, s.masterCfgMod, s.masterCfgSize, s.masterCfgOK = c, fi.ModTime(), fi.Size(), true
	return c, nil
}

// masterConfigInvalidate — сбросить кэш конфига (после сохранения).
func (s *Server) masterConfigInvalidate() {
	s.masterMu.Lock()
	s.masterCfgOK = false
	s.masterMu.Unlock()
}

// handleMaster — страница «Отобразить»: наш chrome + один общий iframe.
func (s *Server) handleMaster(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	noFrameHeaders(w)
	data := s.baseData(r)
	data["Active"] = "master"
	s.render(w, http.StatusOK, "master", data)
}

// handleMasterPanel — содержимое единственного iframe: legacy-вёрстка со всеми
// узлами. Рендерится без layout (standalone-шаблон "masterpanel").
func (s *Server) handleMasterPanel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	noFrameHeaders(w)
	cfg, err := s.masterConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t := s.tmpl["masterpanel"]
	if t == nil {
		http.Error(w, "template not found: masterpanel", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = t.ExecuteTemplate(w, "masterpanel", map[string]any{"Nodes": cfg.Nodes})
}

// handleMasterSettings — страница «Настройка»: сервер-рендер одной формы.
func (s *Server) handleMasterSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	noFrameHeaders(w)
	cfg, err := s.masterConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := s.baseData(r)
	data["Active"] = "master_settings"

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		oldIDs := map[string]bool{}
		for _, n := range cfg.Nodes {
			oldIDs[n.ID] = true
		}
		nodes := nodesOrDefaults(parseNodesForm(r))
		cfg.Nodes = nodes
		if err := masternode.Save(s.masterPath, cfg); err != nil {
			data["Err"] = err.Error()
			data["Nodes"] = nodes
		} else {
			s.masterConfigInvalidate()
			for _, n := range nodes {
				oldIDs[n.ID] = true
			}
			for id := range oldIDs {
				s.masterProxy.Invalidate(id)
			}
			http.Redirect(w, r, "/master/settings?ok=1", http.StatusSeeOther)
			return
		}
	} else {
		data["Nodes"] = nodesOrDefaults(cfg.Nodes)
		if r.URL.Query().Get("ok") != "" {
			data["Ok"] = "Сохранено"
		}
	}
	s.render(w, http.StatusOK, "master_settings", data)
}

// nodesOrDefaults — при пустом списке подставляем локальный узел по умолчанию.
func nodesOrDefaults(nodes []masternode.Node) []masternode.Node {
	if len(nodes) == 0 {
		return []masternode.Node{{Name: "Локальный", Scheme: "http", Host: "localhost", Port: 80}}
	}
	return nodes
}

// parseNodesForm собирает узлы из формы (параллельные массивы полей).
func parseNodesForm(r *http.Request) []masternode.Node {
	names := r.Form["name"]
	schemes := r.Form["scheme"]
	hosts := r.Form["host"]
	ports := r.Form["port"]
	ids := r.Form["id"]
	var out []masternode.Node
	for i := range hosts {
		get := func(v []string) string {
			if i < len(v) {
				return v[i]
			}
			return ""
		}
		host := strings.TrimSpace(get(hosts))
		name := strings.TrimSpace(get(names))
		if host == "" && name == "" {
			continue // пустая строка — пропускаем
		}
		port, _ := strconv.Atoi(strings.TrimSpace(get(ports)))
		out = append(out, masternode.Node{
			ID:     strings.TrimSpace(get(ids)),
			Name:   name,
			Scheme: strings.TrimSpace(get(schemes)),
			Host:   host,
			Port:   port,
		})
	}
	return out
}

func writeProxyResp(w http.ResponseWriter, resp masternode.Response) {
	w.Header().Set("Content-Type", resp.CType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.Status)
	_, _ = io.WriteString(w, resp.Body)
}

// masterUser — авторизация master-API ТОЛЬКО по cookie-сессии (без Bearer).
func (s *Server) masterUser(r *http.Request) bool {
	tok, fromCookie, ok := tokenFrom(r)
	if !ok || !fromCookie {
		return false
	}
	return s.store.Touch(tok)
}

// masterNode — узел по ?id= (или пишет 404/500 и возвращает ok=false).
func (s *Server) masterNode(w http.ResponseWriter, r *http.Request) (masternode.Node, bool) {
	cfg, err := s.masterConfig()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
		return masternode.Node{}, false
	}
	n, ok := cfg.Find(r.URL.Query().Get("id"))
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return masternode.Node{}, false
	}
	return n, true
}

// masterProxyGet — общий путь master-API: cookie-auth, поиск узла, проксирование.
func (s *Server) masterProxyGet(w http.ResponseWriter, r *http.Request, ep masternode.Endpoint, pathQuery string) {
	if !s.masterUser(r) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	n, ok := s.masterNode(w, r)
	if !ok {
		return
	}
	writeProxyResp(w, s.masterProxy.Fetch(r.Context(), n, ep, pathQuery))
}

// handleAPIMasterCheckDaemons — прокси к /check_daemons.php узла (по id).
func (s *Server) handleAPIMasterCheckDaemons(w http.ResponseWriter, r *http.Request) {
	s.masterProxyGet(w, r, masternode.EpCheckDaemons, masternode.CheckDaemonsPath())
}

// handleAPIMasterReadJSON — прокси к /read_json.php?device=map|bat|mppt узла.
func (s *Server) handleAPIMasterReadJSON(w http.ResponseWriter, r *http.Request) {
	device := r.URL.Query().Get("device")
	ep, ok := masternode.DeviceEndpoint(device)
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "bad_device"})
		return
	}
	s.masterProxyGet(w, r, ep, masternode.ReadJSONPath(device))
}
