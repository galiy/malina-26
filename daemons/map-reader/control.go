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
	Cmd string `json:"cmd"`
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
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	var req ctlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		writeJSON(conn, ctlResponse{OK: false, Error: "bad request"})
		return
	}
	switch req.Cmd {
	case "ping":
		writeJSON(conn, ctlResponse{OK: true})
	case "list":
		d := c.reg.doc()
		writeJSON(conn, ctlResponse{OK: true, State: &d})
	default:
		writeJSON(conn, ctlResponse{OK: false, Error: "unknown command"})
	}
}

func writeJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}
