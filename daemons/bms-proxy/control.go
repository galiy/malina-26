package main

import (
	"encoding/json"

	controllib "github.com/galiy/malina-26/daemons/lib/control"
)

// control — команды через Unix-сокет (docs/bms-proxy.md §8): ping, reload.
type control struct {
	stop   chan struct{}
	reload chan struct{}
}

type ctlRequest struct {
	Cmd string `json:"cmd"`
}

type ctlResponse struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

func (c *control) serve() {
	controllib.Serve(sockPath, c.stop, c.handleLine)
}

func (c *control) handleLine(line []byte) any {
	var req ctlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return ctlResponse{OK: false, Code: "bad_request", Error: "bad request"}
	}
	switch req.Cmd {
	case "ping":
		return ctlResponse{OK: true}
	case "reload":
		select {
		case c.reload <- struct{}{}:
		default:
		}
		return ctlResponse{OK: true}
	default:
		return ctlResponse{OK: false, Code: "bad_request", Error: "unknown command"}
	}
}
