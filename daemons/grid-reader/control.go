package main

import (
	"encoding/json"
	"sync"

	controllib "github.com/galiy/malina-26/daemons/lib/control"
)

type control struct {
	shm    *Shm
	sock   string
	stop   chan struct{}
	reload func()
	wg     sync.WaitGroup
}

type ctlRequest struct {
	Cmd string `json:"cmd"`
}

type ctlResponse struct {
	OK    bool      `json:"ok"`
	Code  string    `json:"code,omitempty"`
	Error string    `json:"error,omitempty"`
	State *StateDoc `json:"state,omitempty"`
}

func newControl(shm *Shm, sock string, stop chan struct{}) *control {
	return &control{shm: shm, sock: sock, stop: stop}
}

func (c *control) serve() { controllib.ServeWG(c.sock, c.stop, &c.wg, c.handleLine) }

func (c *control) handleLine(line []byte) any {
	var req ctlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return ctlResponse{OK: false, Code: "bad_request", Error: "bad request"}
	}
	switch req.Cmd {
	case "ping":
		return ctlResponse{OK: true}
	case "list":
		d := listDoc(c.shm)
		return ctlResponse{OK: true, State: &d}
	case "reload":
		if c.reload != nil {
			c.reload()
		}
		return ctlResponse{OK: true}
	default:
		return ctlResponse{OK: false, Code: "bad_request", Error: "unknown command"}
	}
}
