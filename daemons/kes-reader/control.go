package main

import (
	"encoding/json"
	"time"

	controllib "github.com/galiy/malina-26/daemons/lib/control"
)

// control — синхронные команды через Unix-сокет (каркас — lib/control):
// ping, list, write (запись ячеек/команд/реле КЭС).
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
	Code  string    `json:"code,omitempty"`
	Error string    `json:"error,omitempty"`
	State *StateDoc `json:"state,omitempty"`
}

func newControl(cfg *config, reg *Registry, stop chan struct{}) *control {
	return &control{cfg: cfg, reg: reg, stop: stop}
}

func (c *control) serve() { controllib.Serve(c.cfg.sockPath, c.stop, c.handleLine) }

func (c *control) handleLine(line []byte) any {
	var req ctlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return ctlResponse{OK: false, Code: "bad_request", Error: "bad request"}
	}
	switch req.Cmd {
	case "ping":
		return ctlResponse{OK: true}
	case "list":
		d := c.reg.doc()
		return ctlResponse{OK: true, State: &d}
	case "write":
		return c.dispatchWrite(writeRequest{Cmd: "write", ID: req.ID, Gen: req.Gen, NoFrame: req.NoFrame, Ops: req.Ops})
	default:
		return ctlResponse{OK: false, Code: "bad_request", Error: "unknown command"}
	}
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
	cmds := c.reg.CmdOf(e)
	if cmds == nil {
		return fail("internal", "канал команд не инициализирован")
	}
	job := &writeJob{req: wr, resp: make(chan writeResponse, 1)}
	select {
	case cmds <- job:
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
