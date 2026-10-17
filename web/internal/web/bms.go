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
	CellCount    int      `json:"cell_count"`
	CellsMV      []uint16 `json:"cells_mv"`       // мВ, первые cell_count
	CellMin      int      `json:"cell_min"`       // мВ
	CellMax      int      `json:"cell_max"`       // мВ
	CellDiff     int      `json:"cell_diff"`      // мВ (max−min)
	BalanceMask  uint32   `json:"balance_mask"`   // бит i → ячейка i
	CellWarnMask uint32   `json:"cell_warn_mask"` // бит i → предупреждение по ячейке i
	CellProtMask uint32   `json:"cell_prot_mask"` // бит i → защита по ячейке i
	BmsFault     bool     `json:"bms_fault"`      // ошибка BMS в целом
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
// Конфиг — /malina/etc/malina/bms-reader.json (тот же, что читает bms-reader;
// при отсутствии читается legacy bms-gateways.json);
// сохранение → запись конфига + reload демона через control-сокет (SIGHUP-эквивалент).

type bmsGateway struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Mode string `json:"mode"` // "modbus" | "none"
}

type bmsGatewaysFile struct {
	// Настройки bms-reader (вкл/выкл опрос + уровень логов).
	Enabled  bool         `json:"enabled"`
	LogLevel string       `json:"log_level"`
	Gateways []bmsGateway `json:"gateways"`
}

func (s *Server) bmsCfgPath() string {
	if s.opts.BmsConfigPath != "" {
		return s.opts.BmsConfigPath
	}
	return "/malina/etc/malina/bms-reader.json"
}

func (s *Server) bmsSock() string {
	if s.opts.BmsControlSock != "" {
		return s.opts.BmsControlSock
	}
	return "/run/malina-bms/control.sock"
}

// bmsProxyCfgPath / bmsProxySock — пути для настроек БМС-Прокси.
func (s *Server) bmsProxyCfgPath() string {
	if s.opts.BmsProxyConfigPath != "" {
		return s.opts.BmsProxyConfigPath
	}
	return "/malina/etc/malina/bms-proxy.json"
}

func (s *Server) bmsProxySock() string {
	if s.opts.BmsProxyControlSock != "" {
		return s.opts.BmsProxyControlSock
	}
	return "/run/malina-bms-proxy/control.sock"
}

func loadBmsGateways(path string) (bmsGatewaysFile, error) {
	var f bmsGatewaysFile
	b, err := os.ReadFile(path)
	if err != nil && os.IsNotExist(err) && strings.HasSuffix(path, "bms-reader.json") {
		// Миграция: старое имя конфига.
		legacy := filepath.Join(filepath.Dir(path), "bms-gateways.json")
		b, err = os.ReadFile(legacy)
	}
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
	// Нормализуем уровень (нет поля/невалидно → info), чтобы сохранение формы
	// шлюзов не писало пустое значение.
	if lv, ok := normalizeBmsProxyLevel(f.LogLevel); ok {
		f.LogLevel = lv
	} else {
		f.LogLevel = "info"
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

// bmsProxyCfg — содержимое bms-proxy.json (настройки БМС-Прокси).
type bmsProxyCfg struct {
	Enabled  bool   `json:"enabled"`
	Baud     int    `json:"baud"`
	LogLevel string `json:"log_level"`
	AddrMode string `json:"addr_mode"`
	// UDP-слушатель: явный переключатель UDPEnabled + адрес UDPListen.
	UDPEnabled bool   `json:"udp_enabled"`
	UDPListen  string `json:"udp_listen,omitempty"`
}

// bmsProxyBauds — допустимые скорости порта bms-proxy.
// bmsProxyLevels — допустимые уровни логирования bms-proxy.
// bmsProxyAddrModes — виды адресации инвертора (ADR=0..15 и ADR=1..16).
var bmsProxyBauds = []int{2400, 9600, 19200, 57600, 115200, 230400}
var bmsProxyLevels = []string{"debug", "info", "warn", "error"}
var bmsProxyAddrModes = []string{"1-16", "0-15"}

func defaultBmsProxyCfg() bmsProxyCfg {
	return bmsProxyCfg{Enabled: false, Baud: 19200, LogLevel: "info", AddrMode: "1-16"}
}

func loadBmsProxyCfg(path string) bmsProxyCfg {
	c := defaultBmsProxyCfg()
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return c
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return defaultBmsProxyCfg()
	}
	if !bmsProxyBaudOK(c.Baud) {
		c.Baud = defaultBmsProxyCfg().Baud
	}
	if lv, ok := normalizeBmsProxyLevel(c.LogLevel); ok {
		c.LogLevel = lv
	} else {
		c.LogLevel = defaultBmsProxyCfg().LogLevel
	}
	if am, ok := normalizeBmsProxyAddrMode(c.AddrMode); ok {
		c.AddrMode = am
	} else {
		c.AddrMode = defaultBmsProxyCfg().AddrMode
	}
	return c
}

func saveBmsProxyCfg(path string, c bmsProxyCfg) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bms-proxy-*")
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

func bmsProxyBaudOK(b int) bool {
	for _, v := range bmsProxyBauds {
		if b == v {
			return true
		}
	}
	return false
}

// normalizeBmsProxyLevel приводит уровень к канонической форме (в нижнем
// регистре, "warning"→"warn"); ("", false), если значение неизвестно.
func normalizeBmsProxyLevel(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "warning" {
		s = "warn"
	}
	for _, v := range bmsProxyLevels {
		if s == v {
			return s, true
		}
	}
	return "", false
}

// normalizeBmsProxyAddrMode приводит вид адресации к «0-15»/«1-16»
// (принимает "1..16", "1_16" и т.п.); ("", false), если не распознано.
func normalizeBmsProxyAddrMode(s string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.ReplaceAll(t, "..", "-")
	t = strings.ReplaceAll(t, "_", "-")
	t = strings.ReplaceAll(t, " ", "")
	for _, v := range bmsProxyAddrModes {
		if t == v {
			return v, true
		}
	}
	return "", false
}

// baudFromForm парсит скорость порта из формы; невалидное → 0.
func baudFromForm(v string) int {
	b, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return b
}

// handleBmsSettings — страница настроек BMS-Reader (GET) и сохранение (POST).
// Одна форма сохраняет вместе: шлюзы + настройки BMS-Reader (bms-reader.json) и
// настройки БМС-Прокси (bms-proxy.json), затем reload обоих демонов.
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
	pc := loadBmsProxyCfg(s.bmsProxyCfgPath())
	data := s.baseData(r)
	data["Active"] = "bms_settings"
	data["Bauds"] = bmsProxyBauds
	data["Levels"] = bmsProxyLevels
	data["AddrModes"] = bmsProxyAddrModes
	data["ProxyEnabled"] = pc.Enabled
	data["ProxyBaud"] = pc.Baud
	data["ProxyLevel"] = pc.LogLevel
	data["ProxyAddrMode"] = pc.AddrMode
	data["ProxyUDPEnabled"] = pc.UDPEnabled
	data["ProxyUDPListen"] = pc.UDPListen
	// Список шлюзов всегда в данных: при ошибке сохранения любой из форм
	// страница рендерится повторно и не должна терять таблицу шлюзов.
	data["Gateways"] = cfg.Gateways
	// Рамка BMS-Reader (bms-reader: вкл/выкл + уровень логов).
	data["BmsrEnabled"] = cfg.Enabled
	bmsrLvl := cfg.LogLevel
	if lv, ok := normalizeBmsProxyLevel(bmsrLvl); ok {
		bmsrLvl = lv
	} else {
		bmsrLvl = "info"
	}
	data["BmsrLevel"] = bmsrLvl

	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		// Единая форма: шлюзы + BMS-Reader + БМС-Прокси сохраняются вместе.
		gws, verr := parseBmsForm(r)
		bmsrEn := r.Form.Get("bmsr_enabled") == "on"
		bmsrLv, bmsrOK := normalizeBmsProxyLevel(r.Form.Get("bmsr_log_level"))

		np := bmsProxyCfg{
			Enabled:    r.Form.Get("proxy_enabled") == "on",
			Baud:       baudFromForm(r.Form.Get("proxy_baud")),
			LogLevel:   r.Form.Get("proxy_log_level"),
			AddrMode:   r.Form.Get("proxy_addr_mode"),
			UDPEnabled: r.Form.Get("udp_enabled") == "on",
			UDPListen:  strings.TrimSpace(r.Form.Get("udp_listen")),
		}
		lv, lvOK := normalizeBmsProxyLevel(np.LogLevel)
		am, amOK := normalizeBmsProxyAddrMode(np.AddrMode)

		switch {
		case verr != "":
			data["Err"] = verr
		case !bmsrOK:
			data["Err"] = s.tr(s.lang(r), "bms.proxy_bad_level")
		case !bmsProxyBaudOK(np.Baud):
			data["Err"] = s.tr(s.lang(r), "bms.proxy_bad_baud")
		case !lvOK:
			data["Err"] = s.tr(s.lang(r), "bms.proxy_bad_level")
		case !amOK:
			data["Err"] = s.tr(s.lang(r), "bms.proxy_bad_addr_mode")
		default:
			np.LogLevel = lv
			np.AddrMode = am
			cfg.Gateways = gws
			cfg.Enabled = bmsrEn
			cfg.LogLevel = bmsrLv
			// Сохраняем и перезагружаем каждый демон сразу после записи его файла,
			// чтобы сбой второй записи не оставлял первый файл без reload.
			if err := saveBmsGateways(s.bmsCfgPath(), cfg); err != nil {
				data["Err"] = err.Error()
			} else {
				_, _ = controlCall(s.bmsSock(), []byte(`{"cmd":"reload"}`))
				if err := saveBmsProxyCfg(s.bmsProxyCfgPath(), np); err != nil {
					data["Err"] = err.Error()
				} else {
					_, _ = controlCall(s.bmsProxySock(), []byte(`{"cmd":"reload"}`))
					http.Redirect(w, r, "/bms/settings?ok=1", http.StatusSeeOther)
					return
				}
			}
		}
		// повторный рендер при ошибке — показать то, что ввёл пользователь
		data["Gateways"] = gws
		data["BmsrEnabled"] = bmsrEn
		if bmsrOK {
			data["BmsrLevel"] = bmsrLv
		} else {
			data["BmsrLevel"] = bmsrLvl
		}
		data["ProxyEnabled"] = np.Enabled
		data["ProxyBaud"] = np.Baud
		if lvOK {
			data["ProxyLevel"] = lv
		} else {
			data["ProxyLevel"] = pc.LogLevel
		}
		if amOK {
			data["ProxyAddrMode"] = am
		} else {
			data["ProxyAddrMode"] = pc.AddrMode
		}
		data["ProxyUDPEnabled"] = np.UDPEnabled
		data["ProxyUDPListen"] = np.UDPListen
	} else if r.URL.Query().Get("ok") != "" {
		data["Ok"] = s.tr(s.lang(r), "bms.saved")
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
