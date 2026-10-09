// map-reader — демон опроса МАП (Микроарт) на ПАК «Малина».
//
// Главный поток ищет подключённые последовательные устройства МАП: перебирает
// незанятые порты, открывает их и короткой пробой определяет, что это МАП
// (протокол Микроарт, приоритетный, либо Modbus-RTU). Найденный порт
// «захватывается» (остаётся открытым), МАП публикуется в общий список
// (shared memory для API map-list), и запускается горутина опроса.
//
// Состояние публикуется в POSIX shared memory бинарно (seqlock): компактный
// список + слоты с бинарным контрактом MapData — его читает malina-web и отдаёт
// наружу как API map-list. Команды принимаются синхронно через Unix domain
// socket.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Параметры демона зашиты в код (сознательно без CLI-настроек); единственный
// флаг — -debug для подробного лога.
const (
	defSHMPath       = "/dev/shm/malina-map-state"
	defSockPath      = "/run/malina-map/control.sock"
	defPoll          = time.Second
	defMaxMiss       = 9
	defScanIdle      = 10 * time.Second
	defScanActive    = time.Minute
	defBackoffIdle   = 30 * time.Second
	defBackoffActive = 10 * time.Minute
	defMaxMaps       = 9
	defProbeResp     = 200 * time.Millisecond
	defProbeEcho     = 50 * time.Millisecond
)

// config — параметры демона.
type config struct {
	shmPath  string
	sockPath string
	paths    []string

	pollInterval time.Duration // период опроса телеметрии одного МАП
	maxMiss      int           // промахов подряд до потери порта

	scanIdle   time.Duration // цикл сканирования, когда МАП ещё нет
	scanActive time.Duration // цикл сканирования, когда есть хотя бы один МАП

	backoffIdle   time.Duration // пауза для «пустого» порта, когда МАП нет
	backoffActive time.Duration // пауза для «пустого» порта, когда МАП есть

	maxMaps int // максимум одновременно опрашиваемых МАП

	probeResponse time.Duration // ожидание ответа МАП в одной пробе
	probeEcho     time.Duration // ожидание эха одного байта

	debug bool // подробный лог (попытки проб, пропуски портов)
}

// debugf — подробный лог (только при -debug).
func (c *config) debugf(format string, args ...any) {
	if c.debug {
		log.Printf(format, args...)
	}
}

func newConfig(debug bool) *config {
	return &config{
		shmPath:       defSHMPath,
		sockPath:      defSockPath,
		paths:         splitComma(defaultPathsStr),
		pollInterval:  defPoll,
		maxMiss:       defMaxMiss,
		scanIdle:      defScanIdle,
		scanActive:    defScanActive,
		backoffIdle:   defBackoffIdle,
		backoffActive: defBackoffActive,
		maxMaps:       defMaxMaps,
		probeResponse: defProbeResp,
		probeEcho:     defProbeEcho,
		debug:         debug,
	}
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func main() {
	debug := flag.Bool("debug", false, "подробный лог (попытки проб, пропуски портов)")
	flag.Parse()
	cfg := newConfig(*debug)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("map-reader: ")
	log.Printf("starting (shm=%s sock=%s max=%d poll=%s)", cfg.shmPath, cfg.sockPath, cfg.maxMaps, cfg.pollInterval)

	var shm *Shm
	s, err := shmOpen(cfg.shmPath, shmSlots)
	if err != nil {
		log.Fatalf("shm: %v", err)
	}
	shm = s
	log.Printf("shm ready: %s (%d слотов)", cfg.shmPath, shmSlots)

	reg := newRegistry(newSlotAlloc(shm), shm)

	if err := os.MkdirAll(dirOf(cfg.sockPath), 0o755); err != nil {
		log.Printf("mkdir control dir: %v", err)
	}

	stop := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		newControl(cfg, reg, stop).serve()
	}()

	scanner := newScanner(cfg, reg, stop, &wg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner.run()
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

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
