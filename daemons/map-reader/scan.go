package main

import (
	"errors"
	"log"
	"os"
	"sync"

	"github.com/galiy/malina-26/daemons/lib/scanner"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Шаблоны кандидатов по умолчанию.
const defaultPathsStr = "/dev/ttyUSB*,/dev/ttyACM*,/dev/ttyAMA*,/dev/ttyS*,/dev/serial/by-id/*,/dev/serial0,/dev/serial1"

// found — данные найденного МАП, передаваемые из пробы в регистрацию.
type found struct {
	proto string
	baud  int
	dets  []detected
}

// newScanner собирает общий runner (lib/scanner) с MAP-специфичными колбэками:
// проба порта (MicroArt + Modbus) и регистрация/запуск опроса.
func newScanner(cfg *config, reg *Registry, stop chan struct{}, wg *sync.WaitGroup) *scanner.Runner {
	h := scanner.Hooks{
		Paths:          cfg.paths,
		SelfPID:        os.Getpid(),
		MaxDevices:     cfg.maxMaps,
		ScanIdle:       cfg.scanIdle,
		ScanActive:     cfg.scanActive,
		BackoffIdle:    cfg.backoffIdle,
		BackoffActive:  cfg.backoffActive,
		BackoffForeign: cfg.backoffForeign,
		Debugf:         cfg.debugf,
		Count:          reg.Count,
		HasPath:        reg.Has,
		Probe: func(p *transport.Port) (scanner.Result, error) {
			proto, baud, dets, err := probePort(p, cfg)
			if err != nil {
				if errors.Is(err, errForeignMicroArt) {
					return scanner.Result{Kind: scanner.Foreign}, nil
				}
				return scanner.Result{}, err
			}
			return scanner.Result{Kind: scanner.Found, Data: found{proto: proto, baud: baud, dets: dets}}, nil
		},
		Register: func(p *transport.Port, res scanner.Result, rdev uint64, release func()) bool {
			fd := res.Data.(found)
			var entries []*MapEntry
			cmds := make(chan *writeJob, 8)
			for _, d := range fd.dets {
				e, ok := reg.Add(p.Path(), d.unit, fd.proto, fd.baud, d.info, cmds)
				if !ok {
					break // нет свободных слотов
				}
				entries = append(entries, e)
			}
			if len(entries) == 0 {
				return false
			}
			for _, e := range entries {
				log.Printf("МАП найден: %s unit=%d протокол=%s baud=%d модель=%s serial=%d%c (слот %d)",
					p.Path(), e.unit, fd.proto, fd.baud, e.info.Model, e.info.Serial, e.info.SerialLetter, e.slotIdx)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer release()
				pollPort(cfg, reg, stop, p, p.Path(), fd.proto, fd.baud, entries, cmds)
			}()
			return true
		},
	}
	return scanner.NewRunner(h, stop)
}
