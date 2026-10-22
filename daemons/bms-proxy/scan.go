package main

import (
	"os"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Поиск «своего» порта в пассивном режиме (docs/bms-proxy.md §6):
// перебираем свободные кандидаты, на каждом слушаем входящий запрос: для Seplos
// 2 с на PACE-кадр (CID1 0x46, CID2 0x42, валидный CHK), для Vektor до 10 с на
// валидный Modbus RTU-запрос (адрес 1..16, CRC16). Услышали — порт наш; иначе бан.
func scanPatterns() []string {
	return []string{
		"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/ttyAMA*", "/dev/ttyS*",
		"/dev/serial/by-id/*", "/dev/serial0", "/dev/serial1",
	}
}

// pasvProbe слушает порт до deadline. Seplos: первый валидный PACE-запрос
// телеметрии; Vektor: первый валидный Modbus RTU-запрос (docs/bms-proxy.md §5.1).
// Весь принятый поток пишется burst-логгером probe.
func pasvProbe(p *transport.Port, deadline time.Time, protocol string) (int, bool) {
	bl := newBurstLogger("probe")
	defer bl.Stop()
	var buf []byte
	for time.Now().Before(deadline) {
		c, err := p.ReadOneErr(deadline)
		if err != nil {
			break
		}
		bl.Feed('R', []byte{c})
		buf = append(buf, c)
		if protocol == protoVektor {
			// «Наш» порт в Vektor — один из известных блоков: состояние
			// 0x0015×21 или ячейки 0x0071×16. Инвертор шлёт их ~1 раз/с.
			// Прочие валидные Modbus-кадры отбрасываем и ждём дальше.
			if start, adr, n, ok := findModbusRequest(buf); ok {
				if isVektorKnownRequest(buf, start, n) {
					return adr, true
				}
				buf = append(buf[:0], buf[start+n:]...)
			}
			if len(buf) > 4096 {
				buf = buf[:0]
			}
			continue
		}
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
	stop     chan struct{}
	mu       sync.Mutex
	backoff  map[uint64]time.Time
	prefer   string // последний рабочий порт: пробуем его первым
	baud     int    // скорость предыдущего прохода (для сброса банов)
	protocol string // протокол предыдущего прохода
}

func newPortScanner(stop chan struct{}) *portScanner {
	return &portScanner{stop: stop, backoff: make(map[uint64]time.Time)}
}

// findPort запускает один проход поиска на заданной скорости. Возвращает
// открытый найденный порт либо nil, если за проход ничего не нашли.
func (s *portScanner) findPort(baud int, protocol string) *transport.Port {
	s.beginScan(baud, protocol)
	s.pruneBackoff()
	busy := ports.BusyRdevs(os.Getpid())
	// ports.ListPorts раскрывает glob-шаблоны и дедуплицирует кандидатов по rdev.
	paths := ports.ListPorts(scanPatterns())
	s.mu.Lock()
	pref := s.prefer
	s.mu.Unlock()
	// Последний рабочий порт — первым: смена baud применяется за один probe,
	// а не после перебора всех ttyUSB.
	paths = prioritize(paths, pref)
	for _, path := range paths {
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
		budget := probeListen
		if protocol == protoVektor {
			budget = probeListenVektor
		}
		deadline := time.Now().Add(budget)
		if adr, ok := pasvProbe(p, deadline, protocol); ok {
			infof("наш порт %s (baud=%d, protocol=%s, первый адрес %d)", path, baud, protocol, adr)
			return p
		}
		p.Close()
		infof("scan: %s не наш (нет валидного запроса protocol=%s) — бан %s", path, protocol, banPort)
		s.setBackoff(rdev)
	}
	return nil
}

// beginScan фиксирует скорость/протокол прохода. При их смене старые промахи
// недействительны — снимаем баны портов.
func (s *portScanner) beginScan(baud int, protocol string) {
	s.mu.Lock()
	if (s.baud != 0 && s.baud != baud) || (s.protocol != "" && s.protocol != protocol) {
		s.backoff = make(map[uint64]time.Time)
	}
	s.baud = baud
	s.protocol = protocol
	s.mu.Unlock()
}

// prioritize поднимает последний рабочий порт в начало списка кандидатов.
func prioritize(paths []string, pref string) []string {
	if pref == "" {
		return paths
	}
	for i, p := range paths {
		if p == pref {
			copy(paths[1:i+1], paths[0:i])
			paths[0] = pref
			break
		}
	}
	return paths
}

// setPrefer запоминает последний рабочий порт.
func (s *portScanner) setPrefer(path string) {
	s.mu.Lock()
	s.prefer = path
	s.mu.Unlock()
}

// clearBackoff снимает баны со всех портов: при смене baud старые промахи
// недействительны.
func (s *portScanner) clearBackoff() {
	s.mu.Lock()
	s.backoff = make(map[uint64]time.Time)
	s.mu.Unlock()
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
