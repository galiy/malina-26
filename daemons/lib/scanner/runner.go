// Package scanner — общий «runner» сканирования портов для демонов ПАК «Малина».
//
// Инкапсулирует device-agnostic механику: сбор кандидатов, дедуп по rdev,
// пропуск портов, занятых другими процессами (flock), backoff (обычный и
// «чужое устройство»), набор активных портов и цикл сканирования с интервалами
// scanIdle/scanActive. Проба устройства и его регистрация/опрос — через колбэки,
// поэтому Modbus/скорости/контракт остаются в конкретном демоне.
package scanner

import (
	"os"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Kind — результат пробы порта.
type Kind int

const (
	Absent  Kind = iota // валидного ответа нет (не наше/пусто)
	Found               // наше устройство найдено
	Foreign             // наше MicroArt-семейство, но другой тип (напр. КЭС для map-reader)
)

// Result — результат пробы. Data — произвольные данные найденного устройства
// (демон кладёт туда то, что нужно Register).
type Result struct {
	Kind Kind
	Data any
}

// Hooks — колбэки и параметры демона.
type Hooks struct {
	Paths      []string // glob-шаблоны кандидатов
	SelfPID    int      // pid демона (исключается из flock-обхода)
	MaxDevices int      // максимум одновременно опрашиваемых устройств

	ScanIdle   time.Duration // цикл сканирования, когда устройств нет
	ScanActive time.Duration // цикл сканирования, когда хотя бы одно есть

	BackoffIdle    time.Duration // пауза для «пустого» порта, устройств нет
	BackoffActive  time.Duration // пауза для «пустого» порта, устройства есть
	BackoffForeign time.Duration // пауза для порта с чужим MicroArt-устройством

	// Probe — проба порта (открыт runner'ом). Ошибка трактуется как Absent.
	Probe func(p *transport.Port) (Result, error)
	// Register — зарегистрировать найденное устройство и запустить опрос.
	// Возвращает true, если порт принят (fd передан демону) и запущен опрос.
	// release вызывается демоном по завершении опроса (снимает признак active).
	Register func(p *transport.Port, res Result, rdev uint64, release func()) bool
	// Count — число активных устройств (для интервалов и MaxDevices).
	Count func() int
	// HasPath — порт уже опрашивается (по пути).
	HasPath func(path string) bool
	// Debugf — необязательный подробный лог.
	Debugf func(format string, args ...any)
}

// Runner — цикл сканирования.
type Runner struct {
	h    Hooks
	stop <-chan struct{}

	mu      sync.Mutex
	backoff map[uint64]time.Time
	active  map[uint64]bool
}

// NewRunner создаёт runner. stop — канал остановки демона.
func NewRunner(h Hooks, stop <-chan struct{}) *Runner {
	return &Runner{
		h: h, stop: stop,
		backoff: make(map[uint64]time.Time),
		active:  make(map[uint64]bool),
	}
}

func (r *Runner) debugf(format string, args ...any) {
	if r.h.Debugf != nil {
		r.h.Debugf(format, args...)
	}
}

// Run — цикл сканирования до закрытия stop.
func (r *Runner) Run() {
	for {
		if !r.cycle() {
			return
		}
		interval := r.h.ScanIdle
		if r.h.Count() > 0 {
			interval = r.h.ScanActive
		}
		select {
		case <-r.stop:
			return
		case <-time.After(interval):
		}
	}
}

func (r *Runner) cycle() bool {
	r.pruneBackoff()
	if r.h.MaxDevices > 0 && r.h.Count() >= r.h.MaxDevices {
		return true
	}
	busy := ports.BusyRdevs(r.h.SelfPID)
	cands := ports.ListPorts(r.h.Paths)
	r.debugf("scan: кандидатов %d", len(cands))
	for _, path := range cands {
		select {
		case <-r.stop:
			return false
		default:
		}
		if r.h.MaxDevices > 0 && r.h.Count() >= r.h.MaxDevices {
			break
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		rdev := ports.RdevOf(st)
		if r.h.HasPath != nil && r.h.HasPath(path) {
			continue // порт уже опрашивается
		}
		if r.isActive(rdev) {
			continue // опрос ещё жив
		}
		if r.inBackoff(rdev) {
			r.debugf("skip %s: backoff", path)
			continue
		}
		if busy[rdev] {
			r.debugf("skip %s: занят другим процессом", path)
			continue
		}
		p, err := transport.Open(path)
		if err != nil {
			r.debugf("open %s: %v", path, err)
			r.setBackoff(rdev)
			continue
		}
		res, err := r.h.Probe(p)
		if err != nil || res.Kind == Absent {
			p.Close()
			r.debugf("probe %s: не наше устройство (%v)", path, err)
			r.setBackoff(rdev)
			continue
		}
		if res.Kind == Foreign {
			// Порт занят чужим MicroArt-устройством (напр. КЭС/MPPT, _Device==5):
			// быстро отпускаем и надолго, чтобы не занимать чужое устройство.
			p.Close()
			r.debugf("probe %s: чужое MicroArt-устройство (не наше) — отпускаю", path)
			r.setForeignBackoff(rdev)
			continue
		}
		r.setActive(rdev, true)
		started := r.h.Register(p, res, rdev, func() { r.setActive(rdev, false) })
		if !started {
			p.Close()
			r.setActive(rdev, false)
			r.setBackoff(rdev)
			continue
		}
		r.clearBackoff(rdev)
	}
	return true
}

func (r *Runner) isActive(rdev uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[rdev]
}

func (r *Runner) setActive(rdev uint64, v bool) {
	r.mu.Lock()
	if v {
		r.active[rdev] = true
	} else {
		delete(r.active, rdev)
	}
	r.mu.Unlock()
}

func (r *Runner) backoffTTL() time.Duration {
	if r.h.Count() > 0 {
		return r.h.BackoffActive
	}
	return r.h.BackoffIdle
}

func (r *Runner) setBackoff(rdev uint64) {
	r.mu.Lock()
	r.backoff[rdev] = time.Now().Add(r.backoffTTL())
	r.mu.Unlock()
}

func (r *Runner) setForeignBackoff(rdev uint64) {
	r.mu.Lock()
	r.backoff[rdev] = time.Now().Add(r.h.BackoffForeign)
	r.mu.Unlock()
}

func (r *Runner) pruneBackoff() {
	now := time.Now()
	r.mu.Lock()
	for k, t := range r.backoff {
		if now.After(t) {
			delete(r.backoff, k)
		}
	}
	r.mu.Unlock()
}

func (r *Runner) inBackoff(rdev uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.backoff[rdev]
	return ok && time.Now().Before(t)
}

func (r *Runner) clearBackoff(rdev uint64) {
	r.mu.Lock()
	delete(r.backoff, rdev)
	r.mu.Unlock()
}
