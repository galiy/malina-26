package main

import (
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/scanner"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Шаблоны кандидатов (как у map-reader).
const defaultPathsStr = "/dev/ttyUSB*,/dev/ttyACM*,/dev/ttyAMA*,/dev/ttyS*,/dev/serial/by-id/*,/dev/serial0,/dev/serial1"

// kesSpeeds — порядок скоростей пробы (как у МАП; КЭС по умолчанию 19200).
var kesSpeeds = []int{115200, 19200, 57600, 9600}

// foundKes — данные найденного КЭС из пробы.
type foundKes struct {
	info *kesInfo
	baud int
}

// probeKesPort перебирает скорости и определяет КЭС по _Device==5.
func probeKesPort(p *transport.Port, echoTO, respTO time.Duration) (*kesInfo, int, error) {
	for _, b := range kesSpeeds {
		if err := p.SetBaud(b); err != nil {
			continue
		}
		p.Flush()
		info, err := probeKes(p, echoTO, respTO)
		if err == nil {
			return info, b, nil
		}
		if errors.Is(err, errForeignMap) {
			return nil, b, errForeignMap // это МАП — быстро отпускаем порт
		}
	}
	return nil, 0, errNoKes
}

// newScanner собирает общий runner (lib/scanner) с КЭС-специфичными колбэками.
func newScanner(cfg *config, reg *Registry, stop chan struct{}, wg *sync.WaitGroup) *scanner.Runner {
	h := scanner.Hooks{
		Paths:          cfg.paths,
		SelfPID:        os.Getpid(),
		MaxDevices:     cfg.maxKes,
		ScanIdle:       cfg.scanIdle,
		ScanActive:     cfg.scanActive,
		BackoffIdle:    cfg.backoffIdle,
		BackoffActive:  cfg.backoffActive,
		BackoffForeign: cfg.backoffForeign,
		Debugf:         cfg.debugf,
		Count:          reg.Count,
		HasPath:        reg.Has,
		Probe: func(p *transport.Port) (scanner.Result, error) {
			info, baud, err := probeKesPort(p, cfg.probeEcho, cfg.probeResponse)
			if err != nil {
				if errors.Is(err, errForeignMap) {
					return scanner.Result{Kind: scanner.Foreign}, nil
				}
				return scanner.Result{}, err
			}
			return scanner.Result{Kind: scanner.Found, Data: foundKes{info: info, baud: baud}}, nil
		},
		Register: func(p *transport.Port, res scanner.Result, rdev uint64, release func()) bool {
			fd := res.Data.(foundKes)
			cmds := make(chan *writeJob, 8)
			e, ok := reg.Add(p.Path(), fd.baud, fd.info, cmds)
			if !ok {
				return false
			}
			log.Printf("КЭС найден: %s baud=%d UID=%d (слот %d)", p.Path(), fd.baud, fd.info.UID, e.slotIdx)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer release()
				pollPort(cfg, reg, stop, p, e, cmds)
			}()
			return true
		},
	}
	return scanner.NewRunner(h, stop)
}
