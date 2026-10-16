// kes-reader — демон опроса КЭС/MPPT (Микроарт) на ПАК «Малина».
//
// Настройки (kes-reader.json): enabled (вкл/выкл опрос) и log_level. При
// enabled=false демон остаётся активным, но ничего не опрашивает и публикует
// ПУСТОЙ список (docs/kes-reader.md).
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

// Значения по умолчанию.
const (
	defSHMPath  = "/dev/shm/malina-kes-state"
	defSockPath = "/run/malina-kes/control.sock"

	defPoll           = time.Second
	defMaxMiss        = 9
	defScanIdle       = 10 * time.Second
	defScanActive     = time.Minute
	defBackoffIdle    = 30 * time.Second
	defBackoffActive  = 10 * time.Minute
	defBackoffForeign = 10 * time.Minute
	defMaxKes         = 32
	defProbeResp      = 200 * time.Millisecond
	defProbeEcho      = 50 * time.Millisecond
	defProbeDeadline  = 2 * time.Second

	defWriteRetries = 5
	defWritePause   = 500 * time.Millisecond

	defSleepPoll = 30 * time.Second // период перечитывания настроек в режиме сна
)

// lx — общий логгер демона (уровни debug/info/warn/error).
var lx = logx.New("kes-reader: ")

// config — параметры демона.
type config struct {
	shmPath  string
	sockPath string
	rawSock  string // Unix-сокет raw-tsdb (модуль сохранения сырья)
	paths    []string

	pollInterval time.Duration
	maxMiss      int

	scanIdle   time.Duration
	scanActive time.Duration

	backoffIdle    time.Duration
	backoffActive  time.Duration
	backoffForeign time.Duration

	maxKes int

	probeResponse time.Duration
	probeEcho     time.Duration
	probeDeadline time.Duration

	writeRetries int
	writePause   time.Duration

	debug bool
}

func (c *config) debugf(format string, args ...any) {
	lx.Debugf(format, args...)
}

func newConfig(debug bool) *config {
	raw := "/run/malina-ts-raw/tsdb.sock"
	if v := os.Getenv("MALINA_TS_RAW_SOCK"); v != "" {
		raw = v
	}
	return &config{
		shmPath:        defSHMPath,
		sockPath:       defSockPath,
		rawSock:        raw,
		paths:          strings.Split(defaultPathsStr, ","),
		pollInterval:   defPoll,
		maxMiss:        defMaxMiss,
		scanIdle:       defScanIdle,
		scanActive:     defScanActive,
		backoffIdle:    defBackoffIdle,
		backoffActive:  defBackoffActive,
		backoffForeign: defBackoffForeign,
		maxKes:         defMaxKes,
		probeResponse:  defProbeResp,
		probeEcho:      defProbeEcho,
		probeDeadline:  defProbeDeadline,
		writeRetries:   defWriteRetries,
		writePause:     defWritePause,
		debug:          debug,
	}
}

func main() {
	debug := flag.Bool("debug", false, "подробный лог (попытки проб, пропуски портов)")
	flag.Parse()

	log.SetOutput(lx)
	log.SetFlags(0)
	log.SetPrefix("")

	cfg := newConfig(*debug)
	initStatsink(cfg.rawSock)

	shm, err := shmOpen(cfg.shmPath, kesSlots)
	if err != nil {
		lx.Errorf("shm: %v", err)
		os.Exit(1)
	}
	defer shm.Close()
	lx.Infof("starting (shm=%s sock=%s raw=%s max=%d poll=%s)", cfg.shmPath, cfg.sockPath, cfg.rawSock, cfg.maxKes, cfg.pollInterval)

	reg := newRegistry(newSlotAlloc(shm), shm)

	if err := os.MkdirAll(filepath.Dir(cfg.sockPath), 0o755); err != nil {
		lx.Warnf("mkdir control dir: %v", err)
	}

	st := loadSettings()
	holder := newSettingsHolder(st)
	if *debug {
		holder.setForcedDebug()
	}
	holder.applyLevel(lx)

	stop := make(chan struct{})
	reload := make(chan struct{}, 1)

	ctl := newControl(cfg, reg, stop)
	ctl.reload = func() { holder.reloadSettings(lx, reload) }
	go ctl.serve()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigv := <-sig
		lx.Infof("signal %v, stopping", sigv)
		close(stop)
	}()

	stopped := false
	for !stopped {
		s := holder.get()
		holder.applyLevel(lx)
		if s.Enabled {
			lx.Infof("состояние: ВКЛЮЧЁН — опрос КЭС")
			phase := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				newScanner(cfg, reg, phase, &wg).Run()
			}()
			select {
			case <-reload:
			case <-stop:
			}
			close(phase)
			waitTimeout(&wg, 5*time.Second, "сканер/пулеры")
			reg.ReleaseAll()
			select {
			case <-stop:
				stopped = true
			default:
			}
			continue
		}
		lx.Infof("состояние: ВЫКЛЮЧЕН — сплю, список пуст")
		reg.ReleaseAll()
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

	reg.ReleaseAll()
	if statsink != nil {
		statsink.Close()
	}
	lx.Infof("stopped")
}

func waitTimeout(wg *sync.WaitGroup, d time.Duration, what string) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		lx.Warnf("%s не завершились за %s", what, d)
	}
}
