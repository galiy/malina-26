package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
// [offset, offset+count). Возврат JSON-массив десятичных. Вне диапазона/без
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
	arr := make([]int, count)
	for i := 0; i < count; i++ {
		arr[i] = int(va[offset+i])
	}
	body, _ := json.Marshal(arr)
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
func (s *Server) handleWriteSec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	user, pass, ok := r.BasicAuth()
	if !ok || !s.basic.Check(user, pass) {
		s.audit.Event("kes_write", map[string]any{"result": "unauthorized", "ip": clientIP(r)})
		w.Header().Set("WWW-Authenticate", `Basic realm="malina"`)
		writeText(w, http.StatusUnauthorized, "error")
		return
	}

	idSt, ok := r.URL.Query()["id"]
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
	if _, ok := r.URL.Query()["relay"]; ok {
		relay, err := strconv.Atoi(r.URL.Query().Get("relay"))
		if err != nil || relay < 1 || relay > 3 {
			return nil, false
		}
		mode, ok := r.URL.Query()["mode"]
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
		cur := f.RelayC
		r1, r2, r3 := cur&1, (cur>>1)&1, (cur>>2)&1
		switch relay {
		case 1:
			if (r1 == 1) == on {
				// ранний Ok: реле уже в требуемом состоянии — пустой ops, успех
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

	if _, ok := r.URL.Query()["data"]; ok {
		raw := r.URL.Query().Get("data")
		ops, ok := buildKesCellOps(raw)
		if !ok || len(ops) == 0 {
			return nil, false
		}
		return ops, true
	}
	return nil, false
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
