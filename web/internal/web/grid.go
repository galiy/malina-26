package web

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/solarman"
)

const (
	gridMaxInverters = 16
	gridSHMItemSize  = 160
	gridSHMHeader    = 64
)

type gridInverter struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Type      string `json:"type"`
	Placement string `json:"placement"`
	LoggerSN  uint32 `json:"logger_sn"`
}

type gridConfig struct {
	Enabled    bool           `json:"enabled"`
	LogLevel   string         `json:"log_level"`
	PollPeriod int            `json:"poll_period"`
	Placements []string       `json:"placements"`
	Inverters  []gridInverter `json:"inverters"`
}

func defaultGridConfig() gridConfig {
	return gridConfig{Enabled: false, LogLevel: "info", PollPeriod: 10, Placements: []string{"Дом", "Гараж"}}
}

type gridCache struct {
	mu   sync.Mutex
	path string
	mod  time.Time
	size int64
	cfg  gridConfig
	ok   bool
}

var gridCfgCache gridCache

func (s *Server) loadGridConfig() gridConfig {
	path := s.opts.GridConfigPath
	fi, err := os.Stat(path)
	if err != nil {
		return defaultGridConfig()
	}
	gridCfgCache.mu.Lock()
	defer gridCfgCache.mu.Unlock()
	if gridCfgCache.ok && gridCfgCache.path == path && gridCfgCache.mod.Equal(fi.ModTime()) && gridCfgCache.size == fi.Size() {
		return gridCfgCache.cfg
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultGridConfig()
	}
	c := defaultGridConfig()
	if json.Unmarshal(b, &c) != nil {
		return defaultGridConfig()
	}
	if c.PollPeriod != 10 && c.PollPeriod != 20 && c.PollPeriod != 30 {
		c.PollPeriod = 10
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	gridCfgCache.cfg = c
	gridCfgCache.path = path
	gridCfgCache.mod = fi.ModTime()
	gridCfgCache.size = fi.Size()
	gridCfgCache.ok = true
	return c
}

func (s *Server) saveGridConfig(c gridConfig) error {
	path := s.opts.GridConfigPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".grid-*")
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
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	gridCfgCache.mu.Lock()
	gridCfgCache.ok = false
	gridCfgCache.mu.Unlock()
	return nil
}

func (s *Server) handleGridSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	data := s.baseData(r)
	data["Active"] = "grid_settings"
	data["Title"] = s.tr(s.lang(r), "grid.title")
	data["Levels"] = bmsProxyLevels
	data["Periods"] = []int{10, 20, 30}

	c := s.loadGridConfig()
	if r.Method == http.MethodPost {
		nc, msg := parseGridForm(r)
		if msg != "" {
			data["Err"] = msg
			c = nc
		} else if err := s.saveGridConfig(nc); err != nil {
			data["Err"] = err.Error()
			c = nc
		} else {
			reloadErr := "reload grid-reader: сервис не ответил"
			if b, err := controlCall(s.opts.GridControlSock, []byte(`{"cmd":"reload"}`)); err == nil && strings.Contains(string(b), `"ok":true`) {
				http.Redirect(w, r, r.URL.Path+"?ok=1", http.StatusSeeOther)
				return
			}
			data["Err"] = reloadErr
			c = nc
		}
	} else if r.URL.Query().Get("ok") != "" {
		data["Ok"] = s.tr(s.lang(r), "grid.saved")
	}
	data["Enabled"] = c.Enabled
	data["Level"] = c.LogLevel
	data["Period"] = c.PollPeriod
	data["Placements"] = c.Placements
	data["Inverters"] = c.Inverters
	s.render(w, http.StatusOK, "grid_settings", data)
}

func parseGridForm(r *http.Request) (gridConfig, string) {
	_ = r.ParseForm()
	c := gridConfig{
		Enabled:  r.Form.Get("grid_enabled") == "on",
		LogLevel: r.Form.Get("grid_log_level"),
	}
	if _, ok := normalizeBmsProxyLevel(c.LogLevel); !ok {
		return c, "некорректный уровень логирования"
	}
	p, _ := strconv.Atoi(r.Form.Get("grid_period"))
	if p != 10 && p != 20 && p != 30 {
		return c, "период опроса: допустимо 10/20/30"
	}
	c.PollPeriod = p

	seen := map[string]bool{}
	for _, v := range r.Form["placement_name"] {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		c.Placements = append(c.Placements, v)
	}
	if len(c.Placements) == 0 {
		return c, "добавьте хотя бы одно размещение"
	}

	ips := r.Form["ip"]
	names := r.Form["name"]
	ports := r.Form["port"]
	types := r.Form["type"]
	places := r.Form["placement"]
	sns := r.Form["logger_sn"]
	for i := range ips {
		ip := strings.TrimSpace(ips[i])
		if ip == "" {
			continue
		}
		if len(c.Inverters) >= gridMaxInverters {
			return c, "максимум 16 инверторов"
		}
		if net.ParseIP(ip) == nil {
			return c, "инвертор " + ip + ": некорректный IP-адрес"
		}
		port := 8899
		if i < len(ports) {
			rawPort := strings.TrimSpace(ports[i])
			if rawPort != "" {
				v, err := strconv.Atoi(rawPort)
				if err != nil || v < 1 || v > 65535 {
					return c, "инвертор " + ip + ": порт должен быть 1..65535"
				}
				port = v
			}
		}
		typ := "deye"
		if i < len(types) {
			typ = strings.ToLower(strings.TrimSpace(types[i]))
			if typ != "deye" && typ != "sofar" {
				return c, "инвертор " + ip + ": тип должен быть deye или sofar"
			}
		}
		sn := uint64(0)
		if i < len(sns) {
			v, err := strconv.ParseUint(strings.TrimSpace(sns[i]), 10, 32)
			if err != nil || v == 0 {
				return c, "инвертор " + ip + ": некорректный SN логгера (uint32 > 0)"
			}
			sn = v
		} else {
			return c, "инвертор " + ip + ": укажите SN логгера"
		}
		name := ""
		if i < len(names) {
			name = strings.TrimSpace(names[i])
		}
		if name == "" {
			name = ip
		}
		if len(name) > 64 {
			return c, "инвертор " + ip + ": имя длиннее 64 символов"
		}
		place := ""
		if i < len(places) {
			place = strings.TrimSpace(places[i])
		}
		if len(place) > 64 {
			return c, "инвертор " + name + ": размещение длиннее 64 символов"
		}
		if place == "" || !seen[place] {
			return c, "инвертор " + name + ": размещение не из списка"
		}
		c.Inverters = append(c.Inverters, gridInverter{Name: name, IP: ip, Port: port, Type: typ, Placement: place, LoggerSN: uint32(sn)})
	}
	// Запрет удаления размещения, которое используется.
	used := map[string]bool{}
	for _, d := range c.Inverters {
		used[d.Placement] = true
	}
	for p := range used {
		if !seen[p] {
			return c, "размещение «" + p + "» используется инвертором — удаление запрещено"
		}
	}
	return c, ""
}

type gridStatusItem struct {
	ID        int    `json:"id"`
	Gen       uint64 `json:"gen"`
	Serial    string `json:"serial"`
	Addr      string `json:"addr"`
	Kind      uint8  `json:"kind"`
	Status    uint8  `json:"status"`
	LastSeen  int64  `json:"last_seen"`
	Name      string `json:"name"`
	Placement string `json:"placement"`
}

func (s *Server) requireAPI(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.currentUser(r); !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unauthorized"})
		return false
	}
	return true
}

func (s *Server) handleAPIGridStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	cfg := s.loadGridConfig()
	items := readGridSHM(s.opts.GridSHMPath)
	for i := range items {
		if items[i].ID >= 0 && items[i].ID < len(cfg.Inverters) {
			d := cfg.Inverters[items[i].ID]
			items[i].Name = d.Name
			items[i].Placement = d.Placement
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": time.Now().Unix(), "inverters": items})
}

func readGridSHM(path string) []gridStatusItem {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < gridSHMHeader {
		return nil
	}
	if binary.LittleEndian.Uint32(b[0:4]) != 0x31445247 || binary.LittleEndian.Uint32(b[4:8]) != 1 {
		return nil
	}
	var out []gridStatusItem
	for i := 0; i < gridMaxInverters; i++ {
		off := gridSHMHeader + i*gridSHMItemSize
		if off+gridSHMItemSize > len(b) {
			break
		}
		it := b[off : off+gridSHMItemSize]
		if binary.LittleEndian.Uint32(it[0:4]) != 1 {
			continue
		}
		seq1 := binary.LittleEndian.Uint64(it[8:16])
		if seq1&1 != 0 {
			continue
		}
		lastSeen := int64(binary.LittleEndian.Uint64(it[144:152]))
		if lastSeen == 0 {
			continue
		}
		item := gridStatusItem{
			ID:       i,
			Gen:      binary.LittleEndian.Uint64(it[136:144]),
			Serial:   cstr(it[16:66]),
			Addr:     cstr(it[66:130]),
			Kind:     it[130],
			Status:   it[131],
			LastSeen: lastSeen,
		}
		if binary.LittleEndian.Uint64(it[8:16]) != seq1 {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cstr(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

func (s *Server) handleAPIGridDetectSN(w http.ResponseWriter, r *http.Request) {
	if !s.requireAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	ip := strings.TrimSpace(r.Form.Get("ip"))
	port := strings.TrimSpace(r.Form.Get("port"))
	typ := strings.ToLower(strings.TrimSpace(r.Form.Get("type")))
	if port == "" {
		port = "8899"
	}
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil || !(ipAddr.IsPrivate() || ipAddr.IsLoopback() || ipAddr.IsLinkLocalUnicast()) {
		writeJSON(w, map[string]any{"ok": false, "error": "нужен локальный IP-адрес"})
		return
	}
	pn, err := strconv.Atoi(port)
	if err != nil || pn < 1 || pn > 65535 {
		writeJSON(w, map[string]any{"ok": false, "error": "некорректный порт"})
		return
	}
	cl := &solarman.Client{Address: net.JoinHostPort(ip, port), DeviceSN: 0, Timeout: 3 * time.Second, DialTimeout: time.Second, ReadTimeout: 3 * time.Second, IdleWindow: 3 * time.Second}
	sn := uint32(0)
	tryDeye := func() {
		if _, frames, err := cl.ReadRegistersDeye(r.Context(), 0x3B, 1, 1); err == nil {
			for _, f := range frames {
				if f.DeviceSN != 0 {
					sn = f.DeviceSN
					return
				}
			}
		}
	}
	trySofar := func() {
		if _, frames, err := cl.ReadRegistersDeyeFn(r.Context(), 0x2000, 0x000E, 1, 0x04); err == nil {
			for _, f := range frames {
				if f.DeviceSN != 0 {
					sn = f.DeviceSN
					return
				}
			}
		}
	}
	switch typ {
	case "deye":
		tryDeye()
	case "sofar":
		trySofar()
	default:
		tryDeye()
		if sn == 0 {
			trySofar()
		}
	}
	if sn == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "SN не получен"})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sn": sn})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
