package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Шаблоны кандидатов по умолчанию.
const defaultPathsStr = "/dev/ttyUSB*,/dev/ttyACM*,/dev/ttyAMA*,/dev/ttyS*,/dev/serial/by-id/*,/dev/serial0,/dev/serial1"

type scanner struct {
	cfg     *config
	reg     *Registry
	stop    chan struct{}
	wg      *sync.WaitGroup
	mu      sync.Mutex
	backoff map[uint64]time.Time // ключ — rdev
	active  map[uint64]bool      // порты, уже опрашиваемые (пока горутина жива)
}

func newScanner(cfg *config, reg *Registry, stop chan struct{}, wg *sync.WaitGroup) *scanner {
	return &scanner{
		cfg: cfg, reg: reg, stop: stop, wg: wg,
		backoff: make(map[uint64]time.Time), active: make(map[uint64]bool),
	}
}

func (s *scanner) run() {
	for {
		if !s.cycle() {
			return
		}
		interval := s.cfg.scanIdle
		if s.reg.Count() > 0 {
			interval = s.cfg.scanActive
		}
		select {
		case <-s.stop:
			return
		case <-time.After(interval):
		}
	}
}

func (s *scanner) cycle() bool {
	s.pruneBackoff()
	if s.reg.Count() >= s.cfg.maxMaps {
		return true
	}
	busy := busyRdevs(os.Getpid())
	ports := listPorts(s.cfg.paths)
	s.cfg.debugf("scan: кандидатов %d", len(ports))
	for _, path := range ports {
		select {
		case <-s.stop:
			return false
		default:
		}
		if s.reg.Count() >= s.cfg.maxMaps {
			break
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		rdev := rdevOf(st)
		if s.reg.Has(path) || s.isActive(rdev) {
			continue // порт уже опрашивается
		}
		if s.inBackoff(rdev) {
			s.cfg.debugf("skip %s: backoff", path)
			continue
		}
		if busy[rdev] {
			s.cfg.debugf("skip %s: занят другим процессом", path)
			continue
		}
		p, err := openPort(path)
		if err != nil {
			s.cfg.debugf("open %s: %v", path, err)
			s.setBackoff(rdev)
			continue
		}
		proto, baud, dets, err := probePort(p, s.cfg)
		if err != nil || len(dets) == 0 {
			s.cfg.debugf("probe %s: не МАП (%v)", path, err)
			p.close()
			s.setBackoff(rdev)
			continue
		}
		var entries []*MapEntry
		for _, d := range dets {
			e, ok := s.reg.Add(path, d.unit, proto, baud, d.info)
			if !ok {
				break // нет свободных слотов
			}
			entries = append(entries, e)
		}
		if len(entries) == 0 {
			p.close()
			s.setBackoff(rdev)
			continue
		}
		s.clearBackoff(rdev)
		s.setActive(rdev, true)
		for _, e := range entries {
			log.Printf("МАП найден: %s unit=%d протокол=%s baud=%d модель=%s serial=%d%c (слот %d)",
				path, e.unit, proto, baud, e.info.Model, e.info.Serial, e.info.SerialLetter, e.slotIdx)
		}
		s.wg.Add(1)
		go func(rdev uint64) {
			defer s.wg.Done()
			defer s.setActive(rdev, false)
			pollPort(s.cfg, s.reg, s.stop, p, path, proto, baud, entries)
		}(rdev)
	}
	return true
}

func (s *scanner) isActive(rdev uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[rdev]
}

func (s *scanner) setActive(rdev uint64, v bool) {
	s.mu.Lock()
	if v {
		s.active[rdev] = true
	} else {
		delete(s.active, rdev)
	}
	s.mu.Unlock()
}

func (s *scanner) setBackoff(rdev uint64) {
	ttl := s.cfg.backoffIdle
	if s.reg.Count() > 0 {
		ttl = s.cfg.backoffActive
	}
	s.mu.Lock()
	s.backoff[rdev] = time.Now().Add(ttl)
	s.mu.Unlock()
}

func (s *scanner) pruneBackoff() {
	now := time.Now()
	s.mu.Lock()
	for k, t := range s.backoff {
		if now.After(t) {
			delete(s.backoff, k)
		}
	}
	s.mu.Unlock()
}

func (s *scanner) inBackoff(rdev uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.backoff[rdev]
	return ok && time.Now().Before(t)
}

func (s *scanner) clearBackoff(rdev uint64) {
	s.mu.Lock()
	delete(s.backoff, rdev)
	s.mu.Unlock()
}

func rdevOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Rdev)
	}
	return 0
}

// listPorts собирает кандидатов, дедуп по rdev (by-id ↔ ttyUSB/ttyACM/…).
func listPorts(patterns []string) []string {
	seen := make(map[uint64]bool)
	var out []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			rd := rdevOf(st)
			if rd == 0 || seen[rd] {
				continue
			}
			seen[rd] = true
			out = append(out, m)
		}
	}
	return out
}

// busyRdevs — множество rdev символьных устройств, открытых ДРУГИМИ процессами
// (обход /proc выполняется один раз на цикл, а не на каждый порт).
func busyRdevs(self int) map[uint64]bool {
	set := make(map[uint64]bool)
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return set
	}
	for _, pr := range procs {
		if !pr.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(pr.Name())
		if err != nil || pid == self {
			continue
		}
		fdDir := "/proc/" + pr.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			l, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			fst, err := os.Stat(l)
			if err != nil || fst.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			if s, ok := fst.Sys().(*syscall.Stat_t); ok {
				set[uint64(s.Rdev)] = true
			}
		}
	}
	return set
}
