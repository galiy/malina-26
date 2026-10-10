package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"time"
)

// control — синхронные команды через Unix domain socket (запрос → ответ).
// Пока: ping, list. Запись в МАП появится позже (по явному разрешению).
type control struct {
	cfg  *config
	reg  *Registry
	stop chan struct{}
}

type ctlRequest struct {
	Cmd     string    `json:"cmd"`
	ID      int       `json:"id"`
	Gen     uint64    `json:"gen"`
	NoFrame bool      `json:"no_frame"`
	Ops     []writeOp `json:"ops"`
}

type ctlResponse struct {
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	State *StateDoc `json:"state,omitempty"`
}

func newControl(cfg *config, reg *Registry, stop chan struct{}) *control {
	return &control{cfg: cfg, reg: reg, stop: stop}
}

func (c *control) serve() {
	_ = os.Remove(c.cfg.sockPath)
	ln, err := net.Listen("unix", c.cfg.sockPath)
	if err != nil {
		log.Printf("control socket: %v (команды отключены)", err)
		return
	}
	defer ln.Close()
	_ = os.Chmod(c.cfg.sockPath, 0o660)
	log.Printf("control socket: %s", c.cfg.sockPath)

	// При остановке закрываем листенер, чтобы Accept вернулся; done — чтобы
	// горутина не висла, если serve завершится сам.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-c.stop:
			ln.Close()
		case <-done:
		}
	}()

	sem := make(chan struct{}, 16) // ограничение параллельных соединений
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				c.handle(conn)
			}()
		default:
			conn.Close() // перегрузка — не копим горутины
		}
	}
}

func (c *control) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req ctlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		writeJSON(conn, ctlResponse{OK: false, Error: "bad request"})
		return
	}
	var resp any
	switch req.Cmd {
	case "ping":
		resp = ctlResponse{OK: true}
	case "list":
		d := c.reg.doc()
		resp = ctlResponse{OK: true, State: &d}
	case "write":
		resp = c.dispatchWrite(writeRequest{Cmd: "write", ID: req.ID, Gen: req.Gen, NoFrame: req.NoFrame, Ops: req.Ops})
	default:
		resp = ctlResponse{OK: false, Error: "unknown command"}
	}
	// Дедлайн на запись ответа ставим после обработки: команда записи в МАП
	// может выполняться заметно дольше 3 с (несколько операций × попытки).
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	writeJSON(conn, resp)
}

// dispatchWrite находит устройство по (id, gen) и передаёт команду пулеру порта,
// ожидая результат.
func (c *control) dispatchWrite(wr writeRequest) writeResponse {
	if _, err := validateWrite(wr); err != nil {
		return fail("bad_request", err.Error())
	}
	e, ok := c.reg.Find(wr.ID, wr.Gen)
	if !ok {
		return fail("no_device", "устройство не найдено (несовпадение id/gen)")
	}
	job := &writeJob{req: wr, resp: make(chan writeResponse, 1)}
	select {
	case e.cmd <- job:
	case <-time.After(2 * time.Second):
		return fail("internal", "очередь записи занята")
	}
	select {
	case r := <-job.resp:
		return r
	case <-time.After(120 * time.Second):
		return fail("internal", "таймаут выполнения записи")
	}
}

func writeJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}
