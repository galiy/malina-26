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
)

// config — параметры демона.
type config struct {
	shmPath  string
	sockPath string
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
	if c.debug {
		log.Printf(format, args...)
	}
}

func newConfig(debug bool) *config {
	return &config{
		shmPath:        defSHMPath,
		sockPath:       defSockPath,
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
	cfg := newConfig(*debug)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("kes-reader: ")
	log.Printf("starting (shm=%s sock=%s max=%d poll=%s)", cfg.shmPath, cfg.sockPath, cfg.maxKes, cfg.pollInterval)

	shm, err := shmOpen(cfg.shmPath, kesSlots)
	if err != nil {
		log.Fatalf("shm: %v", err)
	}
	defer shm.Close()
	log.Printf("shm ready: %s (%d слотов)", cfg.shmPath, kesSlots)

	reg := newRegistry(newSlotAlloc(shm), shm)

	if err := os.MkdirAll(filepath.Dir(cfg.sockPath), 0o755); err != nil {
		log.Printf("mkdir control dir: %v", err)
	}

	stop := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		newControl(cfg, reg, stop).serve()
	}()

	scn := newScanner(cfg, reg, stop, &wg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scn.Run()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	sigv := <-sig
	log.Printf("signal %v, stopping", sigv)
	close(stop)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Printf("горутины не завершились за 5 с — принудительный выход")
	}
	reg.ReleaseAll()
	log.Printf("stopped")
}
