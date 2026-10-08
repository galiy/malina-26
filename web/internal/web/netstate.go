package web

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/galiy/malina-26/web/internal/netcfg"
)

const netRunDir = "/run/malina-net"

// netState — рантайм-состояние сети (из /run/malina-net).
type netState struct {
	Persistent string        `json:"persistent"`
	Effective  string        `json:"effective"`
	Override   string        `json:"override,omitempty"`
	Serving    bool          `json:"serving"`
	Monitor    *monitorState `json:"monitor,omitempty"`
}

type monitorState struct {
	TS         int64 `json:"ts"`
	Foreign    bool  `json:"foreign"`
	Serving    bool  `json:"serving"`
	CleanCount int   `json:"clean_count"`
}

func loadNetState() netState {
	ns := netState{Persistent: netcfg.Active(), Effective: netcfg.Active()}
	if b, err := os.ReadFile(netRunDir + "/mode"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			ns.Override = s
			ns.Effective = s
		}
	}
	if _, err := os.Stat(netRunDir + "/serving"); err == nil {
		ns.Serving = true
	}
	if b, err := os.ReadFile(netRunDir + "/monitor.state"); err == nil {
		var m monitorState
		if json.Unmarshal(b, &m) == nil {
			ns.Monitor = &m
		}
	}
	return ns
}
