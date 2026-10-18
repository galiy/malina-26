package main

import (
	"errors"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// shared — разделяемое состояние «количество + список живых BMS» (docs/bms-proxy.md
// §7). Главный поток пишет через store (формирует локально, затем атомарно
// копирует под мьютексом); слушатель читает через load (под мьютексом).
type shared struct {
	mu    sync.RWMutex
	count int
	alive []aliveBMS
}

// store атомарно обновляет снимок (количество + список).
func (s *shared) store(alive []aliveBMS) {
	s.mu.Lock()
	s.alive = alive
	s.count = len(alive)
	s.mu.Unlock()
}

// load возвращает копию количества и списка (для быстрого чтения слушателем).
func (s *shared) load() (int, []aliveBMS) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count, s.alive
}

// listener обслуживает найденный порт (docs/bms-proxy.md §7). Читает запрос
// инвертора, берёт payload нужного слота из shm, собирает SEPLOS и отвечает.
// При отсутствии запрашиваемой BMS молчит. Завершается после idleListener без
// запросов (или по stop) и уведомляет через done.
//
// Настройки (скорость, путь shm, адресация) передаются значениями — слушатель не
// читает изменяемый cfg, поэтому reload уровня/скорости/адресации не создаёт гонок.
func listener(p *transport.Port, baud int, shmPath, addrMode string, sh *shared, stop chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		infof("слушатель на %s (baud=%d)", p.Path(), baud)
		var buf []byte
		last := time.Now()
		for {
			select {
			case <-stop:
				return
			default:
			}
			c, err := p.ReadOneErr(time.Now().Add(readTick))
			if err != nil {
				if !errors.Is(err, transport.ErrTimeout) {
					infof("слушатель %s: чтение остановлено: %v", p.Path(), err)
					return
				}
				if time.Since(last) >= idleListener {
					infof("слушатель %s: %s без запросов — завершаюсь", p.Path(), idleListener)
					return
				}
				continue
			}
			buf = append(buf, c)
			if c != '\r' {
				if len(buf) > 1024 {
					buf = buf[:0]
				}
				continue
			}
			last = time.Now()
			req := parseRequest(buf)
			buf = buf[:0]
			if req == nil || req.CID2 != cid2Tel {
				continue // не телеметрия — молчим, ждём следующий
			}
			if !sh.serve(p, shmPath, addrMode, req) {
				continue
			}
		}
	}()
	return done
}

// mapAddr мапит адрес инвертора ADR на индекс живого списка по виду адресации:
// «1-16» → ADR-1; «0-15» → ADR (docs/bms-proxy.md §5).
func mapAddr(adr int, addrMode string) int {
	if addrMode == addrMode1 {
		return adr - 1
	}
	return adr
}

// serve обрабатывает один запрос телеметрии; false — BMS нет (молчим).
// req.Adr инвертора мапится на индекс живого списка по виду адресации:
// «1-16» → idx = ADR-1; «0-15» → idx = ADR.
func (s *shared) serve(p *transport.Port, shmPath, addrMode string, req *paceRequest) bool {
	idx := mapAddr(req.Adr, addrMode)
	n, list := s.load()
	if idx < 0 || idx >= n {
		// такой BMS нет — молчим, ждём следующий запрос
		debugf("запрос ADR=%d (%s): нет в списке (живых %d) — молчу", req.Adr, addrMode, n)
		return false
	}
	// берём payload из shm по слоту (не по номеру!), сверяем gen.
	payload := readTelemetry(readShm(shmPath), list[idx].Slot, list[idx].Gen)
	if payload == nil || len(payload) < bmscontract.Size {
		debugf("запрос ADR=%d: телеметрия слота %d недоступна — молчу", req.Adr, list[idx].Slot)
		return false
	}
	tel, err := bmscontract.Decode(payload)
	if err != nil {
		warnf("запрос ADR=%d: декодирование контракта: %v", req.Adr, err)
		return false
	}
	info := buildSeplosInfo(&tel)
	frame := buildTelemetryFrame(req.Adr, info)
	debugf("TX ADR=%d: ~%s", req.Adr, frame[1:len(frame)-1])
	if err := p.WriteAll(frame); err != nil {
		warnf("запись ответа ADR=%d: %v", req.Adr, err)
		return false
	}
	debugf("ответ ADR=%d: INFO=%d Б, ячеек=%d", req.Adr, len(info), tel.CellCount)
	return true
}
