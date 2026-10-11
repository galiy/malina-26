package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

// mapWriteResp — ответ map-reader (см. docs/map-write-api.md).
type mapWriteResp struct {
	OK    bool    `json:"ok"`
	Code  string  `json:"code"`
	Error string  `json:"error"`
	Op    *int    `json:"op"`
	ECode *string `json:"ecode"`
}

// controlCall отправляет строку запроса в control-сокет map-reader и читает ответ.
func controlCall(sock string, req []byte) ([]byte, error) {
	c, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(120 * time.Second))
	if _, err := c.Write(append(req, '\n')); err != nil {
		return nil, err
	}
	rd := bufio.NewReader(c)
	line, err := rd.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

// handleAPIMapWrite — HTTP-обёртка над командой `write` map-reader (cookie || Bearer).
// Тело запроса — как у сокета (docs/map-write-api.md); ответ — тот же JSON,
// HTTP-статус по коду.
func (s *Server) handleAPIMapWrite(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		s.audit.Event("map_write", map[string]any{"result": "unauthorized", "ip": clientIP(r)})
		w.Header().Set("WWW-Authenticate", `Bearer realm="malina", error="invalid_token"`)
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"ok": false, "code": "unauthorized", "error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_request", "error": "тело запроса слишком велико или нечитаемо"})
		return
	}
	// Тело — тот же объект, что у сокета; поле "cmd" задаёт эндпоинт.
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_request", "error": "невалидный JSON"})
		return
	}
	obj["cmd"] = "write"
	body, _ = json.Marshal(obj)

	resp, err := controlCall(s.opts.ControlSock, body)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "code": "retry_exhausted", "error": err.Error()})
		return
	}

	var mr mapWriteResp
	_ = json.Unmarshal(resp, &mr)

	// Аудит: пишем результат вместе с адресацией (id/gen) и числом операций.
	id, _ := obj["id"].(float64)
	gen, _ := obj["gen"].(float64)
	opsN := 0
	if ops, ok := obj["ops"].([]any); ok {
		opsN = len(ops)
	}
	res := "ok"
	if !mr.OK {
		res = "err"
	}
	s.audit.Event("map_write", map[string]any{
		"id": int(id), "gen": uint64(gen), "ops": opsN,
		"result": res, "code": mr.Code, "ip": clientIP(r),
	})

	status := http.StatusOK
	if !mr.OK {
		switch mr.Code {
		case "", "bad_request":
			status = http.StatusBadRequest
		case "no_device":
			status = http.StatusNotFound
		case "map_error":
			status = http.StatusUnprocessableEntity
		case "retry_exhausted":
			status = http.StatusBadGateway
		default:
			status = http.StatusInternalServerError
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}
