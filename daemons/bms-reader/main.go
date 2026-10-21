// Command bms-reader — сбор телеметрии BMS (docs/bms-reader-arch.md,
// docs/bms-reader.md). Настройки (enabled/log_level) — в bms-reader.json
// (legacy bms-gateways.json читается как fallback).
// При enabled=false демон остаётся активным, но не опрашивает BMS и публикует
// ПУСТОЙ список/shm.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
	"github.com/galiy/malina-26/daemons/lib/stat"
)

const defSleepPoll = 30 * time.Second // период перечитывания настроек в режиме сна

// lx — общий логгер демона (уровни debug/info/warn/error).
var lx = logx.New("bms-reader: ")

func main() {
	debug := flag.Bool("debug", false, "подробные логи")
	flag.Parse()

	log.SetOutput(lx)
	log.SetFlags(0)
	log.SetPrefix("")

	holder, err := loadState()
	if err != nil {
		lx.Errorf("%v", err)
		os.Exit(1)
	}
	if *debug {
		holder.setForcedDebug()
	}
	holder.applyLevel(lx)
	st, gws := holder.get()
	lx.Infof("шлюзов=%d, enabled=%v log=%s",
		len(gws.Gateways), st.Enabled, lx.LevelName())

	seg, err := openShm()
	if err != nil {
		lx.Errorf("shm: %v", err)
		os.Exit(1)
	}
	defer seg.Close()

	stop := make(chan struct{})
	reload := make(chan struct{}, 1)

	ctl := &control{seg: seg, stop: stop}
	ctl.reload = func() { holder.reloadSettings(lx, reload) }
	go ctl.serve()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for s := range sigc {
			if s == syscall.SIGHUP {
				lx.Infof("SIGHUP → reload")
				holder.reloadSettings(lx, reload)
				continue
			}
			lx.Infof("%s → останов", s)
			close(stop)
			return
		}
	}()

	// Супервизор: активен (оркестратор опроса) / спит (enabled=false, пустой shm).
	stopped := false
	for !stopped {
		st, gws = holder.get()
		holder.applyLevel(lx)
		if st.Enabled {
			lx.Infof("состояние: ВКЛЮЧЁН — опрос BMS")
			phase := make(chan struct{})
			var wg sync.WaitGroup
			sink := stat.New(rawSock)
			orch := newOrchestrator(seg, sink, phase)
			orch.setGateways(gws)
			wg.Add(1)
			go func() {
				defer wg.Done()
				orch.run()
			}()
			select {
			case <-reload:
			case <-stop:
			}
			close(phase)
			// 20 с — с запасом на probeAll (EnBMS до ~16 с); иначе старый run может
			// дооткрыть порт после closeAll и удержать flock.
			waitTimeout(&wg, 20*time.Second, "оркестратор")
			// Сначала гасим пулеры источников и закрываем их транспорты (порты),
			// только потом публикуем пустой shm — иначе пулер может дописать слот.
			orch.waitPollers()
			orch.closeAll()
			sink.Close()
			seg.releaseAll()
			select {
			case <-stop:
				stopped = true
			default:
			}
			continue
		}
		lx.Infof("состояние: ВЫКЛЮЧЕН — сплю, список пуст")
		seg.releaseAll()
		t := time.NewTimer(defSleepPoll)
		select {
		case <-reload:
			t.Stop()
		case <-stop:
			t.Stop()
			stopped = true
		case <-t.C:
			holder.reloadSettings(lx, reload)
		}
	}

	seg.releaseAll()
	lx.Infof("stopped")
}

func waitTimeout(wg *sync.WaitGroup, d time.Duration, what string) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		lx.Warnf("%s не завершился за %s", what, d)
	}
}
