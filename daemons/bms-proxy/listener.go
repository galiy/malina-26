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
		bl := newBurstLogger("listen")
		defer bl.Stop()
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
			bl.Feed('R', []byte{c})
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
			serveRequest(p, shmPath, addrMode, req, bl)
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
// Сырые RX/TX пишет burst-логгер (docs/bms-proxy.md §5.1).
func serveRequest(p *transport.Port, shmPath, addrMode string, req *paceRequest, bl *burstLogger) bool {
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
	bl.Feed('T', frame)
	if err := p.WriteAll(frame); err != nil {
		warnf("запись ответа ADR=%d: %v", req.Adr, err)
		return false
	}
	return true
}

// vektorListener — состояние B для Vektor(Growatt): читает Modbus RTU-запросы,
// отвечает на блок состояния `0x03 0x0015×21` и блок ячеек `0x03 0x0071×16`
// синтетическими данными из активных BMS (4 с), остальные запросы логирует
// (docs/bms-proxy.md §5.1).
func vektorListener(p *transport.Port, shmPath string, stop chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		infof("слушатель Vektor на %s (protocol=%s, отвечаю на 0x03 0x0015×21 и 0x03 0x0071×16)", p.Path(), protoVektor)
		bl := newBurstLogger("vektor")
		defer bl.Stop()
		last := time.Now()
		var buf []byte
		for {
			select {
			case <-stop:
				return
			default:
			}
			c, err := p.ReadOneErr(time.Now().Add(readTick))
			if err != nil {
				if !errors.Is(err, transport.ErrTimeout) {
					infof("слушатель Vektor %s: чтение остановлено: %v", p.Path(), err)
					return
				}
				if time.Since(last) >= idleListener {
					infof("слушатель Vektor %s: %s без запросов — завершаюсь", p.Path(), idleListener)
					return
				}
				continue
			}
			bl.Feed('R', []byte{c})
			buf = append(buf, c)
			var handled bool
			buf, handled = vektorProcess(p, shmPath, buf, bl)
			if handled {
				last = time.Now() // idle считается по валидным Modbus-кадрам
			}
		}
	}()
	return done
}

// vektorProcess разбирает накопленный поток: отвечает на блоки состояния и
// напряжений ячеек, остальные полные кадры логирует и отбрасывает. Возвращает
// остаток буфера (неполный/неразобранный хвост) и признак обработки валидного
// кадра.
func vektorProcess(p *transport.Port, shmPath string, buf []byte, bl *burstLogger) ([]byte, bool) {
	handled := false
	for {
		start, adr, n, ok := findModbusRequest(buf)
		if !ok {
			if len(buf) > 4096 {
				return buf[:0], handled
			}
			return buf, handled
		}
		handled = true
		if start > 0 {
			buf = append(buf[:0], buf[start:]...)
		}
		var reply []byte
		supported := true
		switch {
		case isVektorCellRequest(buf, 0, n):
			reply = vektorCellReply(shmPath, byte(adr))
		case isVektorStatusRequest(buf, 0, n):
			reply = vektorStatusReply(shmPath, byte(adr), vektorRequestQty(buf, 0))
		default:
			supported = false
		}
		if !supported {
			debugf("Vektor: ADR=%d fn=0x%02X — запрос не поддержан, молчу", adr, buf[1])
		} else if reply == nil {
			debugf("Vektor: ADR=%d — нет активных BMS, молчу", adr)
		} else {
			if responseDelay > 0 {
				time.Sleep(responseDelay) // полдуплекс RS485: дать инвертору переключиться на приём
			}
			bl.Feed('T', reply)
			if err := p.WriteAll(reply); err != nil {
				warnf("Vektor: запись ответа ADR=%d: %v", adr, err)
			}
		}
		buf = buf[n:]
	}
}
