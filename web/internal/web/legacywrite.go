package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// writeText пишет текстовый ответ (legacy-совместимо: тело "Ok"/"error").
func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// buildEEPROMOps переводит legacy-формат `[[addr,val],…]` (десятичные) в ops
// контракта записи: addr==0 — команда МАП, иначе запись байта. false — мусор.
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
		a, err1 := strconv.Atoi(pr[0].String())
		v, err2 := strconv.Atoi(pr[1].String())
		if err1 != nil || err2 != nil {
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
		w.Header().Set("WWW-Authenticate", `Basic realm="malina"`)
		writeText(w, http.StatusUnauthorized, "error")
		return
	}

	ops, ok := buildEEPROMOps(r.FormValue("data"))
	if !ok || len(ops) == 0 {
		writeText(w, http.StatusOK, "error")
		return
	}

	// Запись исполняется в основном цикле legacy-модуля (как пулер в map-reader):
	// ставим команду в очередь и ждём результат.
	job := &legacyWriteJob{ops: ops, resp: make(chan bool, 1)}
	select {
	case s.legacy.cmds <- job:
	case <-time.After(2 * time.Second):
		writeText(w, http.StatusOK, "error")
		return
	}
	select {
	case ok := <-job.resp:
		if ok {
			writeText(w, http.StatusOK, "Ok")
		} else {
			writeText(w, http.StatusOK, "error")
		}
	case <-time.After(10 * time.Second):
		writeText(w, http.StatusOK, "error")
	}
}
