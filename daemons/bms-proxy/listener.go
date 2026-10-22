package main

import (
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// listener обслуживает найденный порт (docs/bms-proxy.md §7). Читает запрос
// инвертора, берёт payload стабильного слота из shm, собирает SEPLOS и отвечает.
// При отсутствии данных запрошенного слота молчит. Завершается после
// idleListener без запросов (или по stop) и уведомляет через done.
//
// Настройки (скорость, путь shm, адресация) передаются значениями — слушатель не
// читает изменяемый cfg, поэтому reload уровня/скорости/адресации не создаёт гонок.
func listener(p *transport.Port, baud int, shmPath, addrMode string, stop chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		infof("слушатель на %s (baud=%d)", p.Path(), baud)
		var buf []byte
		var reqStart time.Time
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
			if len(buf) == 0 && c == '~' {
				reqStart = time.Now()
			}
			buf = append(buf, c)
			if c != '\r' {
				if len(buf) > 1024 {
					buf = buf[:0]
				}
				continue
			}
			last = time.Now()
			raw := string(buf)
			req := parseRequest(buf)
			buf = buf[:0]
			if req == nil {
				debugf("RX t=%d ignore=parse raw=%q", reqStart.UnixMicro(), raw)
				continue
			}
			if req.CID2 != cid2Tel {
				debugf("RX t=%d adr=%d ignore=cid2 raw=%q", reqStart.UnixMicro(), req.Adr, raw)
				continue
			}
			serveRequest(p, shmPath, addrMode, req, reqStart, raw)
		}
	}()
	return done
}

// mapAddr мапит адрес инвертора ADR на стабильный слот shm по виду адресации:
// «1-16» → слот ADR-1; «0-15» → слот ADR (docs/bms-proxy.md §5). Слот не
// перенумеровывается при выпадении других BMS.
func mapAddr(adr int, addrMode string) int {
	if addrMode == addrMode1 {
		return adr - 1
	}
	return adr
}

// serveRequest обрабатывает один запрос телеметрии; false — данных нет (молчим).
// rx — время первого байта запроса, raw — кадр (для debug-лога).
func serveRequest(p *transport.Port, shmPath, addrMode string, req *paceRequest, rx time.Time, raw string) bool {
	debugf("RX t=%d raw=%q", rx.UnixMicro(), raw)
	slot := mapAddr(req.Adr, addrMode)
	payload := readSlotTelemetry(readShm(shmPath), slot, time.Now())
	if payload == nil || len(payload) < bmscontract.Size {
		debugf("запрос ADR=%d: слот %d недоступен — молчу", req.Adr, slot)
		return false
	}
	tel, err := bmscontract.Decode(payload)
	if err != nil {
		warnf("запрос ADR=%d: декодирование контракта: %v", req.Adr, err)
		return false
	}
	// Пауза на переключение инвертора в приём — как у реального BMS (~30 мс).
	if responseDelay > 0 {
		time.Sleep(responseDelay)
	}
	info := buildSeplosInfo(&tel)
	// slaveNo (INFO[1]) — номер пакета в параллельной сборке. У нас он равен
	// слоту: ADR1→0, ADR2→1, ADR3→2 (protocol-485.md §5, dips.pdf).
	info[1] = byte(slot)
	frame := buildTelemetryFrame(req.Adr, info)
	tx := time.Now()
	if err := p.WriteAll(frame); err != nil {
		warnf("запись ответа ADR=%d: %v", req.Adr, err)
		return false
	}
	debugf("TX t=%d adr=%d raw=%q", tx.UnixMicro(), req.Adr, string(frame))
	return true
}
