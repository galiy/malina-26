package main

import (
	"encoding/json"

	controllib "github.com/galiy/malina-26/daemons/lib/control"
)

// control — команды через Unix-сокет: ping, list, reload, rescan (arch §13).
type control struct {
	seg    *bmsShm
	stop   chan struct{}
	reload func()
	rescan func()
}

type ctlRequest struct {
	Cmd string `json:"cmd"`
}

type ctlResponse struct {
	OK      bool         `json:"ok"`
	Code    string       `json:"code,omitempty"`
	Error   string       `json:"error,omitempty"`
	Devices []listRecord `json:"devices,omitempty"`
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
	case "list":
		return ctlResponse{OK: true, Devices: c.seg.list()}
	case "reload":
		if c.reload != nil {
			c.reload()
		}
		return ctlResponse{OK: true}
	case "rescan":
		if c.rescan != nil {
			c.rescan()
		}
		return ctlResponse{OK: true}
	default:
		return ctlResponse{OK: false, Code: "bad_request", Error: "unknown command"}
	}
}
