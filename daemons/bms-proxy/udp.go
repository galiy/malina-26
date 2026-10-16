package main

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// udpManager управляет СКРЫТЫМ UDP-слушателем (docs/bms-proxy.md §7а): стартует
// его только если в конфиге задан `udp_listen` (по умолчанию пусто → выключен),
// и переключает при изменении настройки по reload.
type udpManager struct {
	mu   sync.Mutex
	addr string // текущий адрес ("" — выключен)
	cur  *udpListener
}

// sync приводит состояние слушателя к заданному адресу (пусто — выключить).
// Неудачный старт НЕ кэшируется: при m.cur == nil повторный sync с тем же адресом
// снова пытается запустить слушатель (важно для reload/периодического перечитывания).
func (m *udpManager) sync(addr string, d *daemon) {
	m.mu.Lock()
	defer m.mu.Unlock()
	addr = strings.TrimSpace(addr)
	if addr == m.addr && (addr == "" || m.cur != nil) {
		return
	}
	m.stopLocked()
	if addr == "" {
		m.addr = ""
		infof("UDP-слушатель выключен")
		return
	}
	ul, err := newUDPListener(addr, d)
	if err != nil {
		errorf("UDP-слушатель не запущен (%s): %v", addr, err)
		m.addr = "" // не кэшируем неуспех — повторим при следующем sync
		return
	}
	m.addr = addr
	m.cur = ul
	go ul.serve()
}

// stop — корректное завершение (при остановке демона).
func (m *udpManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

// stopLocked останавливает текущий слушатель (под m.mu): сигнал stop, закрытие
// сокета, ожидание выхода serve().
func (m *udpManager) stopLocked() {
	if m.cur != nil {
		close(m.cur.stop)
		_ = m.cur.conn.Close()
		<-m.cur.done
		m.cur = nil
	}
}

// udpListener — постоянный второй слушатель (docs/bms-proxy.md §7а): принимает
// PACE-запросы телеметрии по UDP (инвертор → прозрачный UDP-шлюз → сюда) и
// отвечает теми же данными наших BMS, что и COM-порт. Работает независимо от
// состояния COM-слушателя (запускается всегда, пока жив демон).
type udpListener struct {
	conn *net.UDPConn
	d    *daemon
	done chan struct{} // закрывается при выходе serve()
	stop chan struct{} // сигнал остановки (закрывает sync/stop перед conn.Close)
}

// newUDPListener поднимает UDP-сокет на addr (например, "0.0.0.0:502").
func newUDPListener(addr string, d *daemon) (*udpListener, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp", a)
	if err != nil {
		return nil, err
	}
	return &udpListener{conn: c, d: d, done: make(chan struct{}), stop: make(chan struct{})}, nil
}

// serve читает датаграммы и отвечает на PACE-запросы телеметрии. Возврат — при
// закрытии сокета (по stop).
func (u *udpListener) serve() {
	defer close(u.done)
	infof("UDP-слушатель на %s", u.conn.LocalAddr())
	buf := make([]byte, 2048)
	for {
		n, addr, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			// Штатная остановка: sync/stop закрыли stop и сокет.
			select {
			case <-u.stop:
				return
			default:
			}
			// Транзиентная ошибка приёма — не глушим слушатель, продолжаем.
			warnf("UDP-приём: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		req := parseRequest(buf[:n])
		if req == nil || req.CID2 != cid2Tel {
			debugf("UDP %s: не-PACE/не телеметрия (%d Б)", addr, n)
			continue
		}
		frame := u.build(req)
		if frame == nil {
			debugf("UDP %s: ADR=%d — нет BMS, молчу", addr, req.Adr)
			continue
		}
		if _, err := u.conn.WriteToUDP(frame, addr); err != nil {
			warnf("UDP-ответ %s: %v", addr, err)
			continue
		}
		debugf("UDP %s: ответ ADR=%d, кадр=%d Б", addr, req.Adr, len(frame))
	}
}

// build собирает ответный кадр по запросу: мапит ADR на индекс живого списка
// (addr_mode), читает свежий снимок shm и отдаёт SEPLOS INFO. nil — BMS нет.
func (u *udpListener) build(req *paceRequest) []byte {
	s := u.d.settings()
	idx := mapAddr(req.Adr, s.AddrMode)
	shm := readShm(u.d.cfg.shmPath)
	list := buildAliveList(shm, time.Now())
	if idx < 0 || idx >= len(list) {
		return nil
	}
	payload := readTelemetry(shm, list[idx].Slot, list[idx].Gen)
	if payload == nil || len(payload) < bmscontract.Size {
		return nil
	}
	tel, err := bmscontract.Decode(payload)
	if err != nil {
		warnf("UDP: декодирование контракта ADR=%d: %v", req.Adr, err)
		return nil
	}
	info := buildSeplosInfo(&tel)
	return buildTelemetryFrame(req.Adr, info)
}
