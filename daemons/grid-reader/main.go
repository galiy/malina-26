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
	"sync"
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

		lx.Infof("состояние: ВКЛЮЧЁН — опрос %d инверторов, период %d с (независимые пулеры: %s)",
			len(cfg.Inverters), cfg.PollPeriod, groupsLabel(cfg.Inverters))
		// Deye и Sofar опрашиваются независимыми циклами внутри одного сервиса.
		groupCtx, groupCancel := context.WithCancel(svcCtx)
		present := map[string]bool{}
		for _, d := range cfg.Inverters {
			if d.Valid {
				present[d.Type] = true
			}
		}
		var wg sync.WaitGroup
		for _, typ := range []string{"deye", "sofar"} {
			if !present[typ] {
				continue
			}
			wg.Add(1)
			go func(typ string) {
				defer wg.Done()
				p.pollLoop(groupCtx, cfg, typ)
			}(typ)
		}
		select {
		case <-reload:
			groupCancel()
			wg.Wait()
			continue outer
		case <-stop:
			groupCancel()
			wg.Wait()
			break outer
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

// groupsLabel перечисляет типы, для которых реально запускаются циклы.
func groupsLabel(devs []inverter) string {
	present := map[string]bool{}
	order := make([]string, 0, 2)
	for _, d := range devs {
		if d.Valid && !present[d.Type] {
			present[d.Type] = true
			order = append(order, d.Type)
		}
	}
	if len(order) == 0 {
		return "нет"
	}
	out := order[0]
	for _, typ := range order[1:] {
		out += ", " + typ
	}
	return out
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
