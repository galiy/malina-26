package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// bmsTypeLabel — локализованная метка типа BMS по внутреннему коду.
func bmsTypeLabel(typeID int, tr map[string]string) string {
	switch typeID {
	case 1:
		return pickLabel(tr, "bms.type.ant", "ANT BMS")
	case 2:
		return pickLabel(tr, "bms.type.en", "EnBMS (RS485)")
	default:
		return "BMS"
	}
}

// bmsModeLabel — локализованная метка канонического режима (младший байт).
func bmsModeLabel(code uint16, tr map[string]string) string {
	keys := []string{"bms.mode.idle", "bms.mode.discharge", "bms.mode.charge", "bms.mode.maintain", "bms.mode.full", "bms.mode.shutdown"}
	if int(code) < len(keys) {
		return pickLabel(tr, keys[code], "")
	}
	return ""
}

func pickLabel(tr map[string]string, key, fallback string) string {
	if tr != nil {
		if s := tr[key]; s != "" {
			return s
		}
	}
	return fallback
}

// mainBmsTile — плитка BMS для /api/mainlist (по образцу МАП/КЭС).
type mainBmsTile struct {
	ID     int       `json:"id"`
	Gen    uint64    `json:"gen"`
	Title  string    `json:"title"`  // метка типа + серийник/ID
	Serial string    `json:"serial"` // наш ID (для ссылки/перепривязки на странице)
	Mode   string    `json:"mode"`   // локализованный режим
	Soc    *float64  `json:"soc"`    // % (nullable)
	Pack   mainBlock `json:"pack"`   // колонка АКБ: U/I/P/T
	Online bool      `json:"online"`

	// Карта ячеек и флаги (для плитки на главной).
	CellCount     int      `json:"cell_count"`
	CellsMV       []uint16 `json:"cells_mv"`        // мВ, первые cell_count
	CellMin       int      `json:"cell_min"`        // мВ
	CellMax       int      `json:"cell_max"`        // мВ
	CellDiff      int      `json:"cell_diff"`       // мВ (max−min)
	BalanceMask   uint32   `json:"balance_mask"`     // бит i → ячейка i
	CellWarnMask  uint32   `json:"cell_warn_mask"`   // бит i → предупреждение по ячейке i
	CellProtMask  uint32   `json:"cell_prot_mask"`   // бит i → защита по ячейке i
	BmsFault      bool     `json:"bms_fault"`        // ошибка BMS в целом
}

// alarmAny — есть ли хоть один установленный бит alarm в диапазоне [base, base+w).
func alarmAny(t *bmscontract.Telemetry, base, w int) bool {
	for k := base; k < base+w && k < bmscontract.AlarmLen*8; k++ {
		if t.Alarm[k/8]&(1<<uint(k%8)) != 0 {
			return true
		}
	}
	return false
}

// alarmVal — значение битового поля ширины w (LSB-first) с позиции base.
func alarmVal(t *bmscontract.Telemetry, base, w int) int {
	v := 0
	for k := 0; k < w; k++ {
		if t.Alarm[(base+k)/8]&(1<<uint((base+k)%8)) != 0 {
			v |= 1 << uint(k)
		}
	}
	return v
}

// antMOSFault — код MOS (0..15) считается ошибкой только для защитных состояний
// (кроме 0=Off, 1=On и 4=battery full / 11=charge on).
func antMOSFault(code int) bool {
	switch code {
	case 2, 3, 5, 6, 7, 8, 9, 10, 12, 13, 14:
		return true
	}
	return false
}

// buildBmsTile собирает плитку BMS из записи списка и распакованного контракта.
func buildBmsTile(tr map[string]string, b BmsInfo, tel bmscontract.Telemetry) mainBmsTile {
	tile := mainBmsTile{
		ID:     b.ID,
		Gen:    b.Gen,
		Title:  b.Serial, // только серийник (тип — на странице устройства)
		Serial: b.Serial,
		Mode:   bmsModeLabel(tel.Mode&0xFF, tr),
		Online: time.Since(time.Unix(b.LastSeen, 0)) <= mainOnlineWindow,
	}
	soc := float64(tel.SocDP) / 10 // слот активен (иначе не в списке)
	tile.Soc = &soc
	tile.Pack.U = mv(float64(tel.TotalCV) / 100)   // 0.01 В → В
	tile.Pack.F = mvND()                           // у BMS частоты нет
	tile.Pack.I = mv(float64(tel.CurrentDA) / 100) // 0.01 А → А
	tile.Pack.P = mv(float64(tel.PowerW))          // Вт
	if tel.TempCount > 0 {
		tile.Pack.T = mv(float64(tel.TempsDC[0]) / 10) // 0.1 °C → °C
	} else {
		tile.Pack.T = mvND()
	}

	// Карта ячеек: напряжения, min/max/разница (мВ).
	n := int(tel.CellCount)
	if n > bmscontract.MaxCells {
		n = bmscontract.MaxCells
	}
	cells := make([]uint16, 0, n)
	mn, mx := 0, 0
	for i := 0; i < n; i++ {
		v := tel.CellsMV[i]
		cells = append(cells, v)
		if i == 0 || int(v) < mn {
			mn = int(v)
		}
		if i == 0 || int(v) > mx {
			mx = int(v)
		}
	}
	tile.CellCount = n
	tile.CellsMV = cells
	tile.CellMin, tile.CellMax, tile.CellDiff = mn, mx, mx-mn
	tile.BalanceMask = tel.BalanceMask

	// Флаги по типу (alarm — тип-специфичный, раскладка docs/bms/<TYPE>.md).
	var cellWarn, cellProt uint32
	switch b.TypeID {
	case 2: // EnBMS: per-cell warn/protect — alarm-биты 0..63 (4 бита/ячейку:
		// bit0 high-warn, bit1 high-prot, bit2 low-warn, bit3 low-prot).
		// Семантика — как на странице /bms (см. bms.js decodeAlarms).
		for i := 0; i < n; i++ {
			if alarmAny(&tel, i*4, 1) || alarmAny(&tel, i*4+2, 1) {
				cellWarn |= 1 << uint(i)
			}
			if alarmAny(&tel, i*4+1, 1) || alarmAny(&tel, i*4+3, 1) {
				cellProt |= 1 << uint(i)
			}
		}
	}
	tile.CellWarnMask = cellWarn
	tile.CellProtMask = cellProt

	switch b.TypeID {
	case 1: // AntBMS: MOS-коды в alarm (биты 0..3 charge, 4..7 discharge)
		if antMOSFault(alarmVal(&tel, 0, 4)) || antMOSFault(alarmVal(&tel, 4, 4)) {
			tile.BmsFault = true
		}
	case 2: // EnBMS: защиты/предупреждения Ext_Bit Byte0..5 (alarm-биты 100..146)
		if alarmAny(&tel, 100, 47) {
			tile.BmsFault = true
		}
	}
	return tile
}

// Страницы BMS: /bms (конкретное устройство, закладки + Memo-JSON) и
// /bms/settings (шлюзы IP:PORT + режим).
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

// handleBmsPage — страница конкретной BMS (/bms?id&gen): закладки-заглушки
// «Панель/Информация/Управление» + Memo с JSON (bms.js). По образцу handleKesPage.
func (s *Server) handleBmsPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	sn := r.URL.Query().Get("serial")
	b, ok := findBms(id)
	if !ok && sn != "" {
		b, ok = findBmsBySerial(sn)
	}
	data := s.baseData(r)
	data["Active"] = "bms"
	if ok {
		data["ID"] = b.ID
		data["Gen"] = b.Gen
		data["Serial"] = b.Serial
		data["BmsType"] = b.TypeName
		data["Title"] = b.MenuLabel(s.trFor(r))
	} else if sn != "" {
		// Устройство ещё не обнаружено — страница-ожидание; клиент повторит поиск.
		data["ID"] = id
		data["Gen"] = 0
		data["Serial"] = sn
		data["Title"] = bmsTypeLabel(0, s.trFor(r)) + " · " + sn
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "bms", data)
}

// findBmsBySerial — поиск записи списка BMS по серийнику (при устаревшем id/gen).
func findBmsBySerial(serial string) (BmsInfo, bool) {
	if serial == "" {
		return BmsInfo{}, false
	}
	for _, b := range loadBmsList().Bms {
		if b.Serial == serial {
			return b, true
		}
	}
	return BmsInfo{}, false
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
