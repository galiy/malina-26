package web

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/solarman"
	"github.com/galiy/malina-26/daemons/lib/tscontract"
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
	LoggerSN  uint32 `json:"logger_sn"`
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
	shmItems := readGridSHM(s.opts.GridSHMPath)
	byID := make(map[int]gridStatusItem, len(shmItems))
	for _, it := range shmItems {
		byID[it.ID] = it
	}
	items := make([]gridStatusItem, 0, len(cfg.Inverters))
	for id, d := range cfg.Inverters {
		it, ok := byID[id]
		if !ok {
			it = gridStatusItem{ID: id, Kind: gridKind(d.Type)}
		}
		it.Name = d.Name
		it.Placement = d.Placement
		// Не подставляем SN чужому слоту в переходный период после смены
		// настроек: адрес shm должен совпадать с текущей записью конфига.
		ip := strings.TrimSpace(d.IP)
		port := d.Port
		if port == 0 {
			port = 8899
		}
		addr := net.JoinHostPort(ip, strconv.Itoa(port))
		if !ok || it.Addr == "" || it.Addr == addr {
			it.LoggerSN = d.LoggerSN
		}
		items = append(items, it)
	}
	// Если конфига нет, но shm что-то содержит — показываем shm как есть.
	if len(items) == 0 {
		items = shmItems
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": time.Now().Unix(), "inverters": items})
}

func readGridSHM(path string) []gridStatusItem {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseGridSHM(b)
}

func parseGridSHM(b []byte) []gridStatusItem {
	if len(b) < gridSHMHeader {
		return nil
	}
	if binary.LittleEndian.Uint32(b[0:4]) != 0x31445247 || binary.LittleEndian.Uint32(b[4:8]) != 1 {
		return nil
	}
	if binary.LittleEndian.Uint32(b[8:12]) != 32+64 || binary.LittleEndian.Uint32(b[12:16]) != gridMaxInverters {
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

func gridKind(typ string) uint8 {
	if typ == "sofar" {
		return 1
	}
	return 0
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

// mainGridTile — плашка сетевого инвертора для главной.
type mainGridTile struct {
	ID                 int       `json:"id"`
	Gen                uint64    `json:"gen"`
	Serial             string    `json:"serial"`
	Name               string    `json:"name"`
	Placement          string    `json:"placement"`
	Type               string    `json:"type"`
	Online             bool      `json:"online"`
	Status             uint8     `json:"status"`
	StatusText         string    `json:"status_text"`
	InverterStatus     uint8     `json:"inverter_status"`
	InverterStatusText string    `json:"inverter_status_text"`
	Alarm              bool      `json:"alarm"`
	PVVoltage          mainValue `json:"pv_voltage"`
	PVCurrent          mainValue `json:"pv_current"`
	PVPower            mainValue `json:"pv_power"`
	ACActive           mainValue `json:"ac_active"`
	ACReactive         mainValue `json:"ac_reactive"`
	Frequency          mainValue `json:"frequency"`
	L1Voltage          mainValue `json:"l1_voltage"`
	L2Voltage          mainValue `json:"l2_voltage"`
	L3Voltage          mainValue `json:"l3_voltage"`
	CurrentSum         mainValue `json:"current_sum"`
	Temp1              mainValue `json:"temp1"`
	Temp2              mainValue `json:"temp2"`
	EnergyToday        mainValue `json:"energy_today"`
	EnergyTotal        mainValue `json:"energy_total"`
	LastSeen           int64     `json:"last_seen"`
}

var gridMainContract = func() tscontract.Contract {
	r := tscontract.NewDefaultRegistry()
	c, _ := r.Get("grid", 1)
	return c
}()

func (s *Server) buildMainGridTiles(tr map[string]string) []mainGridTile {
	cfg := s.loadGridConfig()
	if !cfg.Enabled || len(cfg.Inverters) == 0 {
		return []mainGridTile{}
	}
	data, err := os.ReadFile(s.opts.GridSHMPath)
	if err != nil {
		return []mainGridTile{}
	}
	items := parseGridSHM(data)
	if len(items) == 0 {
		return []mainGridTile{}
	}
	period := cfg.PollPeriod
	if period <= 0 {
		period = 10
	}
	stale := 7 * time.Duration(period) * time.Second / 2
	if stale < 2*time.Minute {
		stale = 2 * time.Minute
	}
	out := make([]mainGridTile, 0, len(items))
	for _, it := range items {
		t := mainGridTile{
			ID:                 it.ID,
			Gen:                it.Gen,
			Serial:             it.Serial,
			Type:               "deye",
			Status:             it.Status,
			LastSeen:           it.LastSeen,
			StatusText:         tr["grid.st"+strconv.Itoa(int(it.Status))],
			InverterStatusText: tr["grid.inv_st0"],
			PVVoltage:          mvND(), PVCurrent: mvND(), PVPower: mvND(),
			ACActive: mvND(), ACReactive: mvND(), Frequency: mvND(),
			L1Voltage: mvND(), L2Voltage: mvND(), L3Voltage: mvND(),
			CurrentSum: mvND(), Temp1: mvND(), Temp2: mvND(),
			EnergyToday: mvND(), EnergyTotal: mvND(),
		}
		if time.Since(time.Unix(it.LastSeen, 0)) > stale && it.Status == 1 {
			t.Status = 2
			t.StatusText = tr["grid.st2"]
		}
		t.Online = t.Status == 1
		if it.Kind == 1 {
			t.Type = "sofar"
		}
		if it.ID >= 0 && it.ID < len(cfg.Inverters) {
			d := cfg.Inverters[it.ID]
			t.Name = d.Name
			t.Placement = d.Placement
			t.Type = d.Type
		}
		if t.Name == "" {
			t.Name = t.Serial
		}
		if payload, ok := gridSlotPayload(data, it.ID, it.Gen); ok {
			if vals, derr := gridMainContract.Decode(payload[:50]); derr == nil {
				m := make(map[uint16]int64, len(vals))
				for _, v := range vals {
					m[v.ID] = v.Int
				}
				t.Alarm = m[19] != 0 || m[20] != 0 || m[21] != 0 || m[22] != 0 || m[23] != 0 || m[24] != 0
				t.InverterStatus = uint8(m[18])
				t.InverterStatusText = tr["grid.inv_st"+strconv.Itoa(int(m[18]))]
				t.PVVoltage = mv(maxF(float64(m[1]), float64(m[4])) * 0.1)
				t.PVCurrent = mv(float64(m[2]+m[5]) * 0.01)
				t.PVPower = mv(float64(m[3]+m[6]) * 10)
				t.ACActive = mv(float64(m[7]) * 10)
				t.ACReactive = mv(float64(m[8]) * 10)
				t.Frequency = mv(float64(m[9]) * 0.01)
				t.L1Voltage = mv(float64(m[10]) * 0.1)
				t.L2Voltage = mv(float64(m[12]) * 0.1)
				t.L3Voltage = mv(float64(m[14]) * 0.1)
				t.CurrentSum = mv(float64(m[11]+m[13]+m[15]) * 0.01)
				if m[16] == -1000 {
					t.Temp1 = mvND()
				} else {
					t.Temp1 = mv(float64(m[16]) * 0.1)
				}
				if m[17] == -1000 {
					t.Temp2 = mvND()
				} else {
					t.Temp2 = mv(float64(m[17]) * 0.1)
				}
				if len(payload) >= 55 {
					today := binary.LittleEndian.Uint16(payload[50:52])
					total := uint32(payload[52]) | uint32(payload[53])<<8 | uint32(payload[54])<<16
					t.EnergyToday = mv(float64(today) * 0.1)
					t.EnergyTotal = mv(float64(total) * 0.1)
				}
			}
		}
		out = append(out, t)
	}
	return out
}

func gridSlotPayload(data []byte, id int, gen uint64) ([]byte, bool) {
	const header = 64
	const slots = 16
	const itemSize = 160
	const slotSize = 32 + 64
	off := header + slots*itemSize + id*slotSize
	if off+slotSize > len(data) {
		return nil, false
	}
	sl := data[off : off+slotSize]
	if binary.LittleEndian.Uint32(sl[0:4]) != 1 {
		return nil, false
	}
	seq1 := binary.LittleEndian.Uint64(sl[16:24])
	if seq1&1 != 0 || binary.LittleEndian.Uint64(sl[8:16]) != gen {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(sl[24:28]))
	if n < 55 || n > 64 {
		return nil, false
	}
	payload := sl[32 : 32+n]
	if binary.LittleEndian.Uint32(sl[28:32]) != crc32.ChecksumIEEE(payload) {
		return nil, false
	}
	if binary.LittleEndian.Uint64(sl[16:24]) != seq1 {
		return nil, false
	}
	return payload, true
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

type gridDeviceField struct {
	ID      uint16 `json:"id"`
	NameKey string `json:"name_key"`
	DescKey string `json:"desc_key"`
	Value   string `json:"value"`
	UnitKey string `json:"unit_key,omitempty"`
	ND      bool   `json:"nd"`
}

type gridAlarm struct {
	Word    int    `json:"word"`
	Code    string `json:"code"`
	NameKey string `json:"name_key"`
}

type gridDeviceDoc struct {
	Serial             string            `json:"serial"`
	Name               string            `json:"name"`
	Placement          string            `json:"placement"`
	Type               string            `json:"type"`
	Kind               uint8             `json:"kind"`
	Gen                uint64            `json:"gen"`
	Status             uint8             `json:"status"`
	StatusText         string            `json:"status_text"`
	InverterStatus     uint8             `json:"inverter_status"`
	InverterStatusText string            `json:"inverter_status_text"`
	LastSeen           int64             `json:"last_seen"`
	EnergyToday        string            `json:"energy_today"`
	EnergyTotal        string            `json:"energy_total"`
	EnergyTodayND      bool              `json:"energy_today_nd"`
	EnergyTotalND      bool              `json:"energy_total_nd"`
	Fields             []gridDeviceField `json:"fields"`
	AlarmWords         []string          `json:"alarm_words"`
	Alarms             []gridAlarm       `json:"alarms"`
}

var gridFieldNames = []string{"pv1_voltage", "pv1_current", "pv1_power", "pv2_voltage", "pv2_current", "pv2_power", "ac_active_power", "ac_reactive_power", "grid_frequency", "l1_voltage", "l1_current", "l2_voltage", "l2_current", "l3_voltage", "l3_current", "temperature_1", "temperature_2", "inverter_status", "alarm_w0", "alarm_w1", "alarm_w2", "alarm_w3", "alarm_w4", "alarm_w5"}

type gridMenuItem struct {
	Serial string
	Gen    uint64
	Name   string
}

func (s *Server) gridMenuItems() []gridMenuItem {
	cfg := s.loadGridConfig()
	out := []gridMenuItem{}
	for _, it := range readGridSHM(s.opts.GridSHMPath) {
		name := it.Serial
		if it.ID >= 0 && it.ID < len(cfg.Inverters) && cfg.Inverters[it.ID].Name != "" {
			name = cfg.Inverters[it.ID].Name
		}
		out = append(out, gridMenuItem{Serial: it.Serial, Gen: it.Gen, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Server) handleGridPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	serial := r.URL.Query().Get("id")
	if serial == "" {
		http.Redirect(w, r, "/grid/settings", http.StatusSeeOther)
		return
	}
	data := s.baseData(r)
	data["Active"] = "grid"
	data["Title"] = serial
	data["Serial"] = serial
	data["Gen"] = r.URL.Query().Get("gen")
	for _, it := range readGridSHM(s.opts.GridSHMPath) {
		if it.Serial == serial {
			cfg := s.loadGridConfig()
			name, place := serial, ""
			if it.ID >= 0 && it.ID < len(cfg.Inverters) {
				if cfg.Inverters[it.ID].Name != "" {
					name = cfg.Inverters[it.ID].Name
				}
				place = cfg.Inverters[it.ID].Placement
			}
			data["Title"] = name
			data["Placement"] = place
			data["Type"] = "Deye"
			if it.Kind == 1 {
				data["Type"] = "Sofar"
			}
			data["Gen"] = it.Gen
			break
		}
	}
	s.render(w, http.StatusOK, "grid", data)
}

func (s *Server) handleAPIGridDevice(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	serial := r.URL.Query().Get("serial")
	if serial == "" {
		http.Error(w, "serial required", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(s.opts.GridSHMPath)
	if err != nil {
		http.Error(w, "no shm", http.StatusNotFound)
		return
	}
	items := parseGridSHM(data)
	var it *gridStatusItem
	for i := range items {
		if items[i].Serial == serial {
			it = &items[i]
			break
		}
	}
	if it == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cfg := s.loadGridConfig()
	doc := gridDeviceDoc{
		Serial: it.Serial, Name: it.Serial, Type: "deye", Kind: it.Kind,
		Gen: it.Gen, Status: it.Status, LastSeen: it.LastSeen,
		Fields: []gridDeviceField{}, AlarmWords: []string{}, Alarms: []gridAlarm{},
	}
	doc.StatusText = s.tr(s.lang(r), "grid.st"+strconv.Itoa(int(it.Status)))
	if it.Kind == 1 {
		doc.Type = "sofar"
	}
	if it.ID >= 0 && it.ID < len(cfg.Inverters) {
		if cfg.Inverters[it.ID].Name != "" {
			doc.Name = cfg.Inverters[it.ID].Name
		}
		doc.Placement = cfg.Inverters[it.ID].Placement
		doc.Type = cfg.Inverters[it.ID].Type
	}
	payload, ok := gridSlotPayload(data, it.ID, it.Gen)
	if !ok {
		doc.Fields = gridEmptyFields()
		doc.EnergyTodayND, doc.EnergyTotalND = true, true
		writeJSON(w, doc)
		return
	}
	vals, derr := gridMainContract.Decode(payload[:50])
	if derr != nil {
		doc.Fields = gridEmptyFields()
		doc.EnergyTodayND, doc.EnergyTotalND = true, true
		writeJSON(w, doc)
		return
	}
	m := make(map[uint16]int64, len(vals))
	for _, v := range vals {
		m[v.ID] = v.Int
	}
	doc.InverterStatus = uint8(m[18])
	doc.InverterStatusText = s.tr(s.lang(r), "grid.inv_st"+strconv.Itoa(int(m[18])))
	for id := uint16(1); id <= 24; id++ {
		f := gridDeviceField{ID: id, NameKey: "stats.f.grid." + gridFieldNames[id-1], DescKey: "stats.d.grid." + gridFieldNames[id-1]}
		v := m[id]
		switch {
		case id >= 19:
			f.Value = "0x" + fmt.Sprintf("%04X", uint16(v))
		case id == 18:
			f.Value = fmt.Sprintf("%d", v)
			f.ND = false
		case id == 16 || id == 17:
			if v == -1000 {
				f.ND = true
			} else {
				f.Value = fmtFloat(float64(v)*0.1, 1)
				f.UnitKey = "home.grid.c"
			}
		case id == 8:
			f.Value = fmtFloat(float64(v)*10, 0)
			f.UnitKey = "home.grid.var"
		case id == 9:
			f.Value = fmtFloat(float64(v)*0.01, 2)
			f.UnitKey = "home.grid.hz"
		case id == 1 || id == 4 || id == 10 || id == 12 || id == 14:
			f.Value = fmtFloat(float64(v)*0.1, 1)
			f.UnitKey = "home.grid.v"
		case id == 2 || id == 5 || id == 11 || id == 13 || id == 15:
			f.Value = fmtFloat(float64(v)*0.01, 2)
			f.UnitKey = "home.grid.a"
		default: // 3,6,7,18
			if id == 3 || id == 6 || id == 7 {
				f.Value = fmtFloat(float64(v)*10, 0)
				f.UnitKey = "home.grid.w"
			} else {
				f.Value = fmt.Sprintf("%d", v)
			}
		}
		doc.Fields = append(doc.Fields, f)
	}
	if len(payload) >= 55 {
		today := binary.LittleEndian.Uint16(payload[50:52])
		total := uint32(payload[52]) | uint32(payload[53])<<8 | uint32(payload[54])<<16
		doc.EnergyToday = fmtFloat(float64(today)*0.1, 1)
		doc.EnergyTotal = fmtFloat(float64(total)*0.1, 1)
	} else {
		doc.EnergyTodayND, doc.EnergyTotalND = true, true
	}
	for i, w := range []int64{m[19], m[20], m[21], m[22], m[23], m[24]} {
		doc.AlarmWords = append(doc.AlarmWords, fmt.Sprintf("0x%04X", uint16(w)))
		doc.Alarms = append(doc.Alarms, decodeAlarms(uint8(it.Kind), i, uint16(w))...)
	}
	writeJSON(w, doc)
}

func fmtFloat(v float64, dec int) string {
	return strconv.FormatFloat(v, 'f', dec, 64)
}

func gridEmptyFields() []gridDeviceField {
	out := make([]gridDeviceField, 0, 24)
	for id := uint16(1); id <= 24; id++ {
		out = append(out, gridDeviceField{ID: id, NameKey: "stats.f.grid." + gridFieldNames[id-1], DescKey: "stats.d.grid." + gridFieldNames[id-1], ND: true})
	}
	return out
}

func decodeAlarms(kind uint8, word int, v uint16) []gridAlarm {
	var out []gridAlarm
	if v == 0 {
		return out
	}
	if kind == 1 {
		if word == 5 {
			return append(out, gridAlarm{Word: word, Code: fmt.Sprintf("0x%04X", v), NameKey: "grid.alm.sofar.alert"})
		}
		for _, a := range sofarFaultAlarm {
			if v&a.Bit != 0 {
				out = append(out, gridAlarm{Word: word, Code: fmt.Sprintf("0x%04X", a.Bit), NameKey: a.Key})
			}
		}
		return out
	}
	for bit := 0; bit < 16; bit++ {
		if v&(1<<uint(bit)) == 0 {
			continue
		}
		switch {
		case word < 2:
			n := word*16 + bit + 1
			key, ok := deyeWarnAlarm[n]
			if !ok {
				key = "grid.alm.unknown"
			}
			out = append(out, gridAlarm{Word: word, Code: fmt.Sprintf("W%d", n), NameKey: key})
		default:
			n := (word-2)*16 + bit + 1
			key, ok := deyeFaultAlarm[n]
			if !ok {
				key = "grid.alm.unknown"
			}
			out = append(out, gridAlarm{Word: word, Code: fmt.Sprintf("F%d", n), NameKey: key})
		}
	}
	return out
}
