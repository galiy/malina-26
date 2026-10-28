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
//
// Настройки (map-reader.json): enabled (вкл/выкл опрос) и log_level. При
// enabled=false демон остаётся активным, но ничего не опрашивает и публикует
// ПУСТОЙ список (docs/map-reader.md).
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
	defTimeSyncThreshMin = 1                                      // дневной порог коррекции, минут

	defSleepPoll = 30 * time.Second // период перечитывания настроек в режиме сна
)

// lx — общий логгер демона (уровни debug/info/warn/error).
var lx = logx.New("map-reader: ")

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

// debugf — подробный лог на уровне DEBUG.
func (c *config) debugf(format string, args ...any) {
	lx.Debugf(format, args...)
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

	// Стандартный log направляем в leveled-логгер (строки → INFO).
	log.SetOutput(lx)
	log.SetFlags(0)
	log.SetPrefix("")

	cfg := newConfig(*debug)
	initStatsink(cfg.rawSock)

	var shm *Shm
	s, err := shmOpen(cfg.shmPath, shmSlots)
	if err != nil {
		lx.Errorf("shm: %v", err)
		os.Exit(1)
	}
	shm = s
	lx.Infof("starting (shm=%s sock=%s raw=%s max=%d poll=%s)", cfg.shmPath, cfg.sockPath, cfg.rawSock, cfg.maxMaps, cfg.pollInterval)

	reg := newRegistry(newSlotAlloc(shm), shm)

	if err := os.MkdirAll(dirOf(cfg.sockPath), 0o755); err != nil {
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

	// Супервизор состояний: активен (опрос) / спит (enabled=false, пустой список).
	stopped := false
	for !stopped {
		s := holder.get()
		holder.applyLevel(lx)
		if s.Enabled {
			lx.Infof("состояние: ВКЛЮЧЁН — опрос МАП")
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

// waitTimeout ждёт wg, но не дольше d (чтобы выход не завис на I/O-таймаутах).
func waitTimeout(wg *sync.WaitGroup, d time.Duration, what string) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		lx.Warnf("%s не завершились за %s", what, d)
	}
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
