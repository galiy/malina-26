// grid-reader — демон опроса сетевых инверторов Deye/Sofar (Solarman V5).
//
// Настройки: /malina/etc/malina/grid-reader.json. При enabled=false сервис
// активен, но спит и публикует пустой список (docs/grid-reader.md).
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

const (
	defSHMPath  = "/dev/shm/malina-grid-state"
	defSockPath = "/run/malina-grid/control.sock"
	sleepPoll   = 30 * time.Second
)

var lx = logx.New("grid-reader: ")

func main() {
	debug := flag.Bool("debug", false, "подробный лог")
	flag.Parse()

	shmPath := defSHMPath
	sockPath := defSockPath
	if v := os.Getenv("MALINA_GRID_SHM"); v != "" {
		shmPath = v
	}
	if v := os.Getenv("MALINA_GRID_SOCK"); v != "" {
		sockPath = v
	}
	rawSock := "/run/malina-ts-raw/tsdb.sock"
	if v := os.Getenv("MALINA_TS_RAW_SOCK"); v != "" {
		rawSock = v
	}

	shm, err := shmOpen(shmPath)
	if err != nil {
		lx.Errorf("shm: %v", err)
		os.Exit(1)
	}
	defer shm.Close()

	sink := newTsdbSink(rawSock)
	defer sink.Close()
	p := newPoller(shm, sink, lx)

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		lx.Warnf("mkdir control dir: %v", err)
	}
	stop := make(chan struct{})
	reload := make(chan struct{}, 1)
	svcCtx, svcCancel := context.WithCancel(context.Background())
	defer svcCancel()
	ctl := newControl(shm, sockPath, stop)
	ctl.reload = func() {
		select {
		case reload <- struct{}{}:
		default:
		}
	}
	ctlDone := make(chan struct{})
	go func() {
		ctl.serve()
		close(ctlDone)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		lx.Infof("signal %v, stopping", s)
		svcCancel()
		close(stop)
	}()

	lx.Infof("starting (shm=%s sock=%s raw=%s)", shmPath, sockPath, rawSock)

outer:
	for {
		cfg, warns := loadSettings()
		for _, w := range warns {
			lx.Warnf("%s", w)
		}
		applyLevel(cfg.LogLevel, *debug)
		p.reset(cfg)

		if !cfg.Enabled {
			p.clear()
			lx.Infof("состояние: ВЫКЛЮЧЕН — сплю, список пуст")
			select {
			case <-reload:
				continue
			case <-stop:
				break outer
			case <-time.After(sleepPoll):
				continue
			}
		}

		lx.Infof("состояние: ВКЛЮЧЁН — опрос %d инверторов, период %d с", len(cfg.Inverters), cfg.PollPeriod)
		var reloadPending atomic.Bool
		for {
			roundCtx, roundCancel := context.WithCancel(svcCtx)
			roundDone := make(chan struct{})
			go func() {
				select {
				case <-reload:
					reloadPending.Store(true)
					roundCancel()
				case <-stop:
					roundCancel()
				case <-roundDone:
				}
			}()
			round := p.pollOnce(roundCtx, cfg)
			close(roundDone)
			roundCancel()
			if reloadPending.Swap(false) {
				continue outer
			}
			select {
			case <-stop:
				break outer
			default:
			}
			period := time.Duration(cfg.PollPeriod) * time.Second
			wait := period - round
			if wait < 0 {
				wait = 0
			}
			select {
			case <-reload:
				continue outer
			case <-stop:
				break outer
			case <-time.After(wait):
			}
		}
	}
	p.clear()
	select {
	case <-ctlDone:
		ctl.wg.Wait()
	case <-time.After(3 * time.Second):
		lx.Warnf("control не остановился за 3 с")
	}
	lx.Infof("stopped")
}

func applyLevel(level string, debug bool) {
	if debug {
		lx.SetLevel(logx.Debug)
		return
	}
	if _, ok := logx.ParseLevel(level); ok {
		lx.SetLevelString(level)
	} else {
		lx.SetLevelString("info")
	}
}
