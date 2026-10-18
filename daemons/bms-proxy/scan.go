package main

import (
	"os"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Поиск «своего» порта в пассивном режиме (docs/bms-proxy.md §6):
// перебираем свободные кандидаты, на каждом слушаем probeListen (2 с) входящий
// PACE-запрос; услышали валидный запрос телеметрии (CID1 0x46, CID2 0x42,
// валидный CHK) — порт наш; иначе бан на banPort.
func scanPatterns() []string {
	return []string{
		"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/ttyAMA*", "/dev/ttyS*",
		"/dev/serial/by-id/*", "/dev/serial0", "/dev/serial1",
	}
}

// pasvProbe слушает порт до deadline и возвращает адрес из первого валидного
// PACE-запроса телеметрии; (0,false), если подходящего кадра нет.
func pasvProbe(p *transport.Port, deadline time.Time) (int, bool) {
	var buf []byte
	for time.Now().Before(deadline) {
		c, err := p.ReadOneErr(deadline)
		if err != nil {
			return 0, false
		}
		buf = append(buf, c)
		if c == '\r' {
			req := parseRequest(buf)
			buf = buf[:0]
			if req != nil && req.CID2 == cid2Tel && req.OK {
				return req.Adr, true
			}
		}
		if len(buf) > 1024 {
			buf = buf[:0]
		}
	}
	return 0, false
}

// portScanner — перебор кандидатов с backoff. Живёт столько же, сколько демон,
// поэтому 10-секундный бан портов переживает циклы A→B→A (docs/bms-proxy.md §7).
type portScanner struct {
	stop    chan struct{}
	mu      sync.Mutex
	backoff map[uint64]time.Time
}

func newPortScanner(stop chan struct{}) *portScanner {
	return &portScanner{stop: stop, backoff: make(map[uint64]time.Time)}
}

// findPort запускает один проход поиска на заданной скорости. Возвращает
// открытый найденный порт либо nil, если за проход ничего не нашли.
func (s *portScanner) findPort(baud int) *transport.Port {
	s.pruneBackoff()
	busy := ports.BusyRdevs(os.Getpid())
	// ports.ListPorts раскрывает glob-шаблоны и дедуплицирует кандидатов по rdev.
	for _, path := range ports.ListPorts(scanPatterns()) {
		select {
		case <-s.stop:
			return nil
		default:
		}
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		rdev := ports.RdevOf(st)
		if s.inBackoff(rdev) {
			continue
		}
		if busy[rdev] {
			debugf("scan: %s занят другим процессом", path)
			continue
		}
		p, err := transport.Open(path)
		if err != nil {
			s.setBackoff(rdev)
			continue
		}
		if err := p.SetBaud(baud); err != nil {
			p.Close()
			warnf("scan: %s baud=%d: %v", path, baud, err)
			s.setBackoff(rdev)
			continue
		}
		deadline := time.Now().Add(probeListen)
		if adr, ok := pasvProbe(p, deadline); ok {
			infof("наш порт %s (baud=%d, первый адрес %d)", path, baud, adr)
			return p
		}
		p.Close()
		infof("scan: %s не наш (молчит) — бан %s", path, banPort)
		s.setBackoff(rdev)
	}
	return nil
}

func (s *portScanner) setBackoff(rdev uint64) {
	s.mu.Lock()
	s.backoff[rdev] = time.Now().Add(banPort)
	s.mu.Unlock()
}

func (s *portScanner) inBackoff(rdev uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.backoff[rdev]
	return ok && time.Now().Before(t)
}

func (s *portScanner) pruneBackoff() {
	now := time.Now()
	s.mu.Lock()
	for k, t := range s.backoff {
		if now.After(t) {
			delete(s.backoff, k)
		}
	}
	s.mu.Unlock()
}
