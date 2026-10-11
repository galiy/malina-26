package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// writeText пишет текстовый ответ (legacy-совместимо: тело "Ok"/"error").
func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// parseIntNum — целое из json.Number (принимает и "438", и "438.0" — как старый PHP).
func parseIntNum(n json.Number) (int, bool) {
	f, err := n.Float64()
	if err != nil {
		return 0, false
	}
	i := int64(f)
	if float64(i) != f {
		return 0, false
	}
	return int(i), true
}

// buildEEPROMOps переводит legacy-формат `[[addr,val],…]` (десятичные) в ops
// контракта записи: addr==0 — команда МАП, иначе запись байта. false — мусор или
// выход за диапазон (addr 0..0x7FF, val 0..255).
func buildEEPROMOps(raw string) ([]map[string]any, bool) {
	var pairs [][]json.Number
	if err := json.Unmarshal([]byte(raw), &pairs); err != nil {
		return nil, false
	}
	ops := make([]map[string]any, 0, len(pairs))
	for _, pr := range pairs {
		if len(pr) < 2 {
			return nil, false
		}
		a, ok1 := parseIntNum(pr[0])
		v, ok2 := parseIntNum(pr[1])
		if !ok1 || !ok2 || a < 0 || a > 0x7FF || v < 0 || v > 255 {
			return nil, false
		}
		if a == 0 {
			ops = append(ops, map[string]any{"op": "cmd", "code": fmt.Sprintf("%02X", v)})
		} else {
			ops = append(ops, map[string]any{"op": "reg", "addr": fmt.Sprintf("%X", a), "data": fmt.Sprintf("%02X", v)})
		}
	}
	return ops, true
}

// handleWriteEEPROM — legacy-совместимая запись: POST `data` = JSON [[addr,val],…]
// (десятичные). addr==0 — команда МАП (val 1..9), иначе запись байта. Основной
// результат — текст "Ok" (успех) / "error" (ошибка) как в старом write_eeprom.php.
// Авторизация — HTTP Basic (пароль → PAM с кэшем), см. docs/api-auth.md.
func (s *Server) handleWriteEEPROM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	user, pass, ok := r.BasicAuth()
	if !ok || !s.basic.Check(user, pass) {
		s.audit.Event("eeprom_write", map[string]any{"result": "unauthorized", "ip": clientIP(r)})
		w.Header().Set("WWW-Authenticate", `Basic realm="malina"`)
		writeText(w, http.StatusUnauthorized, "error")
		return
	}

	ops, ok := buildEEPROMOps(r.FormValue("data"))
	if !ok || len(ops) == 0 {
		s.audit.Event("eeprom_write", map[string]any{"result": "error", "reason": "data", "ip": clientIP(r)})
		writeText(w, http.StatusOK, "error")
		return
	}

	// Запись исполняется в основном цикле legacy-модуля (как пулер в map-reader):
	// ставим команду в очередь и ждём результат.
	job := &legacyWriteJob{ops: ops, resp: make(chan bool, 1)}
	select {
	case s.legacy.cmds <- job:
	case <-time.After(2 * time.Second):
		s.audit.Event("eeprom_write", map[string]any{"ops": len(ops), "result": "busy", "ip": clientIP(r)})
		writeText(w, http.StatusOK, "error")
		return
	}
	select {
	case ok := <-job.resp:
		if ok {
			s.audit.Event("eeprom_write", map[string]any{"ops": len(ops), "result": "ok", "ip": clientIP(r)})
			writeText(w, http.StatusOK, "Ok")
		} else {
			s.audit.Event("eeprom_write", map[string]any{"ops": len(ops), "result": "error", "ip": clientIP(r)})
			writeText(w, http.StatusOK, "error")
		}
	case <-time.After(10 * time.Second):
		s.audit.Event("eeprom_write", map[string]any{"ops": len(ops), "result": "timeout", "ip": clientIP(r)})
		writeText(w, http.StatusOK, "error")
	}
}
