package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// legacy-API КЭС: read_sec.php (чтение диапазона из виртуального адресного
// пространства) и write_sec.php (реле + произвольные ячейки). read_json.php?device=mppt
// — расширение handleReadJSON (см. legacyapi.go). Контракт — docs/legacy-api.md §7.

// kesWriteResp — ответ kes-reader (write).
type kesWriteResp struct {
	OK    bool    `json:"ok"`
	Code  string  `json:"code"`
	Error string  `json:"error"`
	Op    *int    `json:"op"`
	ECode *string `json:"ecode"`
}

// handleReadSec — legacy read_sec.php?id=N&offset=A&count=C:
// байты виртуального адресного пространства КЭС id (RAM-раскладка) в диапазоне
// [offset, offset+count). Формат ответа — как legacy read_sec.php (см.
// docs/legacy/kes-legacy-malina2.md §20): при offset=0 — JSON-массив [b0,b1,…],
// при offset>0 — объект {"addr":byte} (массив ключей $arr[$i] от $offset, который
// json_encode выдаёт как объект с десятичными ключами-адресами). Вне диапазона/без
// аргументов — text "error" (как legacy).
func (s *Server) handleReadSec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	idSt, ok1 := r.URL.Query()["id"]
	offSt, ok2 := r.URL.Query()["offset"]
	if !ok1 || len(idSt) == 0 || !ok2 {
		writeText(w, http.StatusOK, "error")
		return
	}
	id, err := strconv.Atoi(idSt[0])
	if err != nil {
		writeText(w, http.StatusOK, "error")
		return
	}
	id-- // legacy: id 1..N → 0-based индекс
	kl := loadKesList()
	if id < 0 || id >= kl.Count {
		writeText(w, http.StatusOK, "error")
		return
	}
	offset, err := strconv.Atoi(offSt[0])
	if err != nil {
		writeText(w, http.StatusOK, "error")
		return
	}
	count := 1
	if v, ok := r.URL.Query()["count"]; ok && len(v) > 0 {
		if c, err := strconv.Atoi(v[0]); err == nil {
			count = c
		}
	}
	if offset < 0 || count < 1 || offset+count > kesVirtualSize {
		writeText(w, http.StatusOK, "error")
		return
	}
	payload := kesRawPayload(kl.Kes[id].ID)
	if payload == nil {
		writeText(w, http.StatusOK, "error")
		return
	}
	f, ok := parseKesPayload(payload)
	if !ok {
		writeText(w, http.StatusOK, "error")
		return
	}
	va := buildKesVA(f)
	var body []byte
	if offset == 0 {
		// legacy: массив (нет смещения) — как read_memory.php?offset=0.
		arr := make([]int, count)
		for i := 0; i < count; i++ {
			arr[i] = int(va[offset+i])
		}
		body, _ = json.Marshal(arr)
	} else {
		// legacy: объект {addr:byte} — ключи-адреса от $offset (json_encode).
		obj := make(map[string]int, count)
		for i := 0; i < count; i++ {
			obj[strconv.Itoa(offset+i)] = int(va[offset+i])
		}
		body, _ = json.Marshal(obj)
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

// handleWriteSec — legacy write_sec.php. GET:
//   - ?id=N&relay=R&mode=on|off|1|0 — реле КЭС (RMW из 0x3E3, ранний Ok;
//     иначе запись маски в командную ячейку 0x0E3D);
//   - ?id=N&data=[[cell,val],…] — произвольные ячейки (десятичные).
//
// Авторизация — HTTP Basic (пароль → PAM с кэшем), как write_eeprom.
// Методы GET|POST (legacy §20); параметры читаются из r.Form (query + urlencoded body).
func (s *Server) handleWriteSec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeText(w, http.StatusOK, "error")
		return
	}
	user, pass, ok := r.BasicAuth()
	if !ok || !s.basic.Check(user, pass) {
		s.audit.Event("kes_write", map[string]any{"result": "unauthorized", "ip": clientIP(r)})
		w.Header().Set("WWW-Authenticate", `Basic realm="malina"`)
		writeText(w, http.StatusUnauthorized, "error")
		return
	}

	idSt, ok := r.Form["id"]
	if !ok || len(idSt) == 0 {
		writeText(w, http.StatusOK, "error")
		return
	}
	id, err := strconv.Atoi(idSt[0])
	if err != nil {
		writeText(w, http.StatusOK, "error")
		return
	}
	id-- // legacy: id 1..N → 0-based
	kl := loadKesList()
	if id < 0 || id >= kl.Count {
		writeText(w, http.StatusOK, "error")
		return
	}
	info := kl.Kes[id]

	ops, ok := s.buildKesWriteOps(info.ID, r)
	if !ok {
		s.audit.Event("kes_write", map[string]any{"result": "error", "reason": "params", "ip": clientIP(r)})
		writeText(w, http.StatusOK, "error")
		return
	}
	if len(ops) == 0 {
		// ранний Ok: реле уже в требуемом состоянии — писать нечего.
		s.audit.Event("kes_write", map[string]any{"id": info.ID, "gen": info.Gen, "result": "ok_early", "ip": clientIP(r)})
		writeText(w, http.StatusOK, "Ok")
		return
	}

	req, _ := json.Marshal(map[string]any{
		"cmd": "write", "id": info.ID, "gen": info.Gen, "ops": ops,
	})
	resp, err := controlCall(s.opts.KesControlSock, req)
	if err != nil {
		s.audit.Event("kes_write", map[string]any{"result": "error", "reason": err.Error(), "ip": clientIP(r)})
		writeText(w, http.StatusOK, "error")
		return
	}
	var mr kesWriteResp
	_ = json.Unmarshal(resp, &mr)
	res := "ok"
	if !mr.OK {
		res = "err"
	}
	s.audit.Event("kes_write", map[string]any{
		"id": info.ID, "gen": info.Gen, "ops": len(ops),
		"result": res, "code": mr.Code, "ip": clientIP(r),
	})
	if !mr.OK {
		writeText(w, http.StatusOK, "error")
		return
	}
	writeText(w, http.StatusOK, "Ok")
}

// buildKesWriteOps формирует ops контракта kes-reader (только op=reg) из
// параметров write_sec: приоритет у relay, иначе data=[[cell,val],…]. slotID —
// реальный номер слота КЭС в shm.
func (s *Server) buildKesWriteOps(slotID int, r *http.Request) ([]map[string]any, bool) {
	if _, ok := r.Form["relay"]; ok {
		relay, err := strconv.Atoi(r.Form.Get("relay"))
		if err != nil || relay < 1 || relay > 3 {
			return nil, false
		}
		mode, ok := r.Form["mode"]
		if !ok || len(mode) == 0 {
			return nil, false
		}
		on := mode[0] == "1" || mode[0] == "on"
		// RMW из текущего состояния (0x3E3) — приоритет у write, как в legacy.
		payload := kesRawPayload(slotID)
		if payload == nil {
			return nil, false
		}
		f, ok := parseKesPayload(payload)
		if !ok {
			return nil, false
		}
		return kesRmwOps(f.RelayC, relay, on)
	}

	if _, ok := r.Form["data"]; ok {
		raw := r.Form.Get("data")
		ops, ok := buildKesCellOps(raw)
		if !ok || len(ops) == 0 {
			return nil, false
		}
		return ops, true
	}
	return nil, false
}

// kesRmwOps — RMW-логика реле КЭС (текущее состояние cur = Relay_C 0x3E3):
// если реле уже в требуемом состоянии — пустой ops (ранний Ok), иначе запись
// маски состояния в командную ячейку 0x0E3D. relay — 1..3; on — включить.
func kesRmwOps(cur byte, relay int, on bool) ([]map[string]any, bool) {
	if relay < 1 || relay > 3 {
		return nil, false
	}
	r1, r2, r3 := cur&1, (cur>>1)&1, (cur>>2)&1
	switch relay {
	case 1:
		if (r1 == 1) == on {
			return []map[string]any{}, true
		}
	case 2:
		if (r2 == 1) == on {
			return []map[string]any{}, true
		}
	case 3:
		if (r3 == 1) == on {
			return []map[string]any{}, true
		}
	}
	newState := 0
	if relay == 1 {
		newState = b2i(on) + int(r2)<<1 + int(r3)<<2
	}
	if relay == 2 {
		newState = int(r1) + b2i(on)<<1 + int(r3)<<2
	}
	if relay == 3 {
		newState = int(r1) + int(r2)<<1 + b2i(on)<<2
	}
	return []map[string]any{{
		"op": "reg", "addr": "E3D", "data": fmt.Sprintf("%02X", newState),
	}}, true
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// buildKesCellOps — перевод legacy data=[[cell,val],…] (десятичные) в контракт
// kes-reader: op=reg {addr: hex(cell), data: hexByte(val)}. Валидация cell 0..0xFFFF,
// val 0..255 — как legacy (однобайтовые ячейки).
func buildKesCellOps(raw string) ([]map[string]any, bool) {
	var pairs [][]json.Number
	if err := json.Unmarshal([]byte(raw), &pairs); err != nil {
		return nil, false
	}
	if len(pairs) == 0 {
		return nil, false
	}
	ops := make([]map[string]any, 0, len(pairs))
	for _, pr := range pairs {
		if len(pr) < 2 {
			return nil, false
		}
		a, ok1 := parseIntNum(pr[0])
		v, ok2 := parseIntNum(pr[1])
		if !ok1 || !ok2 || a < 0 || a > 0xFFFF || v < 0 || v > 255 {
			return nil, false
		}
		ops = append(ops, map[string]any{
			"op": "reg", "addr": fmt.Sprintf("%X", a), "data": fmt.Sprintf("%02X", v),
		})
	}
	return ops, true
}

// kesSnapshotJSON — массив legacy-снимков всех КЭС или nil (нет данных).
func kesSnapshotJSON() []byte {
	kl := loadKesList()
	if kl.Count == 0 {
		return nil
	}
	out := make([]byte, 0, 256)
	out = append(out, '[')
	first := true
	for _, inf := range kl.Kes {
		payload := kesRawPayload(inf.ID)
		if payload == nil {
			continue
		}
		f, ok := parseKesPayload(payload)
		if !ok {
			continue
		}
		if !first {
			out = append(out, ',')
		}
		first = false
		out = append(out, renderKesJSON(f)...)
	}
	out = append(out, ']')
	return out
}

// kesEnergyWh — суммарная суточная энергия всех присутствующих КЭС в Вт·ч
// (аналог поля mppt_day_E у batmon): сумма (Pwr_kW*1000 + Pwr_W) по слотам.
func kesEnergyWh() int64 {
	var wh int64
	kl := loadKesList()
	for _, inf := range kl.Kes {
		payload := kesRawPayload(inf.ID)
		if payload == nil {
			continue
		}
		f, ok := parseKesPayload(payload)
		if !ok {
			continue
		}
		wh += int64(f.PwrKW)*1000 + int64(f.PwrW)
	}
	return wh
}

// handleReadBat — legacy read_json.php?device=bat (открыт, без авторизации).
// Эмулирует ответ batmon (shm 2017 = 2 слота по 1024 Б): ТОЧНЫЙ состав полей и их
// типы — все значения строки (формат воспроизводит daemons/batmond, извлечён из
// строк binary — см. docs/legacy/batmon-api.md). Реальная величина только
// mppt_day_E = суточная энергия присутствующих КЭС (Вт·ч); остальное — нули
// (батареи на узле нет). Метод/формат обёртки повторяет read_json.php.
func (s *Server) handleReadBat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ts := time.Now().Unix()
	now := time.Now().Format("15:04:05")
	dayWh := kesEnergyWh()
	slot0 := fmt.Sprintf(
		`{"timestamp":"%d","time":"%s","C20":"0.0","C20_t":"0.0","Uacc_avg":"0.00",`+
			`"Iavg":"0.00","C_Ah_remain":"0.0","C_100_remain":"0.0","C(I)":"0.0",`+
			`"TTG":"0","mppt_day_E":"%d","low_V":"0.0","hi_V":"0.0","cycles":"0",`+
			`"LD":"0.0","DD":"0.0","AD":"0.0","Esum_from_bat":"0","Esum_to_bat":"0",`+
			`"No_async":"0","est_soc":"0.0","cons_Ah":"0.0","E_wind_day":"0"}`,
		ts, now, dayWh)
	slot1 := fmt.Sprintf(
		`{"timestamp":"%d","time":"%s","dCdt":"0.0000","dEdt":"0.0000","Isumm":"0.000",`+
			`"IntegralC_0_t":"0.0000","Integral_E_0_t":"0.0000","t_bat":"0","Ah_user":"0.0",`+
			`"Wh_user":"0","Sun_I":"0.0","Sun_P":"0","wind_I":"0.0","wind_P":"0"}`,
		ts, now)
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte("[" + slot0 + "," + slot1 + "]"))
}
