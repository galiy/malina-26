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

	// defBackoffForeign — пауза для порта, где найден валидный MicroArt, но НЕ МАП
	// (например, КЭС/MPPT, _Device==5): чужое устройство не трогаем и не долбим.
	defBackoffForeign = 10 * time.Minute

	defMaxMaps   = 9
	defProbeResp = 200 * time.Millisecond
	defProbeEcho = 50 * time.Millisecond

	defWriteRetries = 5                      // попыток записи на одну операцию
	defWritePause   = 500 * time.Millisecond // пауза между попытками записи

	defTimeSyncOff       = "/malina/etc/malina/map-time-sync.off" // флаг-файл отключения синка
	defTimeSyncWindow    = 5 * time.Second                        // окно вокруг 10-минутной точки
	defTimeSyncThreshMin = 1                                      // порог коррекции, минут
)

// config — параметры демона.
type config struct {
	shmPath  string
	sockPath string
	rawSock  string // Unix-сокет raw-tsdb (модуль сохранения сырья)
	paths    []string

	pollInterval time.Duration // период опроса телеметрии одного МАП
	maxMiss      int           // промахов подряд до потери порта

	scanIdle   time.Duration // цикл сканирования, когда МАП ещё нет
	scanActive time.Duration // цикл сканирования, когда есть хотя бы один МАП

	backoffIdle    time.Duration // пауза для «пустого» порта, когда МАП нет
	backoffActive  time.Duration // пауза для «пустого» порта, когда МАП есть
	backoffForeign time.Duration // пауза для порта с чужим MicroArt-устройством (не МАП)

	maxMaps int // максимум одновременно опрашиваемых МАП

	probeResponse time.Duration // ожидание ответа МАП в одной пробе
	probeEcho     time.Duration // ожидание эха одного байта

	writeRetries int           // попыток записи на операцию
	writePause   time.Duration // пауза между попытками записи

	timeSyncOff       string        // флаг-файл: существует → синхронизация выключена
	timeSyncWindow    time.Duration // окно ± вокруг 10-минутной точки
	timeSyncThreshMin int           // порог коррекции времени, минут

	// Кэш наличия флаг-файла (общий, TTL 1 мин — не дёргать stat каждый цикл).
	timeSyncOffMu   sync.Mutex
	timeSyncOffLast bool
	timeSyncOffAt   time.Time

	debug bool // подробный лог (попытки проб, пропуски портов)
}

// debugf — подробный лог (только при -debug).
func (c *config) debugf(format string, args ...any) {
	if c.debug {
		log.Printf(format, args...)
	}
}

// timeSyncOffTTL — время жизни кэша наличия флаг-файла синхронизации.
const timeSyncOffTTL = time.Minute

// timeSyncDisabled — выключена ли синхронизация флаг-файлом. Результат stat
// кэшируется на timeSyncOffTTL, чтобы не проверять ФС на каждом цикле опроса.
func (c *config) timeSyncDisabled(now time.Time) bool {
	if c.timeSyncOff == "" {
		return false
	}
	c.timeSyncOffMu.Lock()
	defer c.timeSyncOffMu.Unlock()
	if now.Sub(c.timeSyncOffAt) < timeSyncOffTTL {
		return c.timeSyncOffLast
	}
	_, err := os.Stat(c.timeSyncOff)
	c.timeSyncOffLast = err == nil
	c.timeSyncOffAt = now
	return c.timeSyncOffLast
}

func newConfig(debug bool) *config {
	off := defTimeSyncOff
	if v := os.Getenv("MALINA_MAP_TIME_SYNC_OFF"); v != "" {
		off = v
	}
	raw := "/run/malina-ts-raw/tsdb.sock"
	if v := os.Getenv("MALINA_TS_RAW_SOCK"); v != "" {
		raw = v
	}
	return &config{
		shmPath:        defSHMPath,
		sockPath:       defSockPath,
		rawSock:        raw,
		paths:          splitComma(defaultPathsStr),
		pollInterval:   defPoll,
		maxMiss:        defMaxMiss,
		scanIdle:       defScanIdle,
		scanActive:     defScanActive,
		backoffIdle:    defBackoffIdle,
		backoffActive:  defBackoffActive,
		backoffForeign: defBackoffForeign,
		maxMaps:        defMaxMaps,
		probeResponse:  defProbeResp,
		probeEcho:      defProbeEcho,
		writeRetries:   defWriteRetries,
		writePause:     defWritePause,

		timeSyncOff:       off,
		timeSyncWindow:    defTimeSyncWindow,
		timeSyncThreshMin: defTimeSyncThreshMin,

		debug: debug,
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
	log.Printf("starting (shm=%s sock=%s raw=%s max=%d poll=%s)", cfg.shmPath, cfg.sockPath, cfg.rawSock, cfg.maxMaps, cfg.pollInterval)
	initStatsink(cfg.rawSock)

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
	if statsink != nil {
		statsink.Close()
	}
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
