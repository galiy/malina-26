package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
	"github.com/galiy/malina-26/daemons/lib/solarman"
)

// fakeLogger — минимальный Solarman V5 TCP-сервер для смоука.
type fakeLogger struct {
	t          *testing.T
	ln         net.Listener
	kind       string
	start      uint16
	count      uint16
	emptySN    bool
	snText     string
	noResponse bool
	badBlock   bool
	accepted   atomic.Int32
	closed     atomic.Int32
}

func newFakeLogger(t *testing.T, kind string, start, count uint16) *fakeLogger {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLogger{t: t, ln: ln, kind: kind, start: start, count: count, snText: "SA3ES000000001"}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeLogger) addr() (string, int) {
	h, p, _ := net.SplitHostPort(f.ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return h, n
}

func (f *fakeLogger) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.accepted.Add(1)
		go f.handle(c)
	}
}

func (f *fakeLogger) handle(c net.Conn) {
	defer func() { _ = c.Close(); f.closed.Add(1) }()
	buf := make([]byte, 4096)
	for {
		if _, err := io.ReadFull(c, buf[:36]); err != nil {
			return
		}
		req := buf[:36]
		if f.noResponse {
			return
		}
		// serial запроса (big-endian) и начало/число регистров из PDU.
		serial := binary.LittleEndian.Uint16(req[5:7])
		// datafield 15 + PDU offset; PDU: unit fn start count crc
		pdu := 11 + 15
		if pdu+8 > len(req) {
			return
		}
		start := binary.BigEndian.Uint16(req[pdu+2 : pdu+4])
		count := binary.BigEndian.Uint16(req[pdu+4 : pdu+6])
		if start == 0x0003 && count == 5 {
			if f.emptySN {
				f.writeResp(c, serial, make([]uint16, 5))
				return
			}
			// Серийник Deye ASCII "SN12345678" (10 символов).
			f.writeResp(c, serial, []uint16{0x534E, 0x3132, 0x3334, 0x3536, 0x3738})
			return
		}
		if start == 0x2000 && count == 0x0E {
			vals := make([]uint16, 14)
			for i := range vals {
				vals[i] = 'V'<<8 | '0'
			}
			sn := []byte(f.snText)
			for i := 0; i+1 < len(sn); i += 2 {
				reg := i/2 + 1
				if reg < len(vals) {
					vals[reg] = uint16(sn[i])<<8 | uint16(sn[i+1])
				}
			}
			f.writeResp(c, serial, vals)
			return
		}
		vals := make([]uint16, count)
		for i := range vals {
			vals[i] = uint16(i + 1)
		}
		if f.badBlock && len(vals) > 2 {
			vals = vals[:len(vals)-2] // короткий PDU: нет полного блока
		}
		f.writeResp(c, serial, vals)
	}
}

func (f *fakeLogger) writeResp(c net.Conn, serial uint16, vals []uint16) {
	pdu := make([]byte, 3+len(vals)*2+2)
	pdu[0] = 1
	pdu[1] = 3
	pdu[2] = byte(len(vals) * 2)
	for i, v := range vals {
		binary.BigEndian.PutUint16(pdu[3+i*2:], v)
	}
	crc := crc16(pdu[:3+len(vals)*2])
	pdu[3+len(vals)*2] = byte(crc)
	pdu[4+len(vals)*2] = byte(crc >> 8)
	payload := append(make([]byte, 15), pdu...)
	payload[0] = 0x02
	frame := []byte{0xA5, byte(len(payload)), byte(len(payload) >> 8), 0x10, 0x15, byte(serial), byte(serial >> 8)}
	sn := make([]byte, 4)
	binary.LittleEndian.PutUint32(sn, 123456)
	frame = append(frame, sn...)
	frame = append(frame, payload...)
	frame = append(frame, checksum8(frame[1:]), 0x15)
	_, _ = c.Write(frame)
}

func checksum8(b []byte) byte {
	var s uint8
	for _, v := range b {
		s += v
	}
	return s
}

func crc16(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func testPayload(vals []uint16) []byte {
	pdu := make([]byte, 3+len(vals)*2+2)
	pdu[0] = 1
	pdu[1] = 3
	pdu[2] = byte(len(vals) * 2)
	for i, v := range vals {
		binary.BigEndian.PutUint16(pdu[3+i*2:], v)
	}
	crc := crc16(pdu[:3+len(vals)*2])
	pdu[3+len(vals)*2] = byte(crc)
	pdu[4+len(vals)*2] = byte(crc >> 8)
	payload := append(make([]byte, 15), pdu...)
	payload[0] = 0x02
	return payload
}

func TestFullRegsLastPicksLast(t *testing.T) {
	frames := []solarman.Frame{
		{Payload: testPayload([]uint16{1, 2})},
		{Payload: testPayload([]uint16{3, 4})},
	}
	regs, ok := fullRegsLast(frames, 0, 2)
	if !ok || regs[0] != 3 || regs[1] != 4 {
		t.Fatalf("ожидался последний PDU: ok=%v regs=%v", ok, regs)
	}
}

func TestClientNewConnectionEachRequest(t *testing.T) {
	srv := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	ip, port := srv.addr()
	cl := &solarman.Client{
		Address: net.JoinHostPort(ip, strconv.Itoa(port)), DeviceSN: 123456,
		Timeout: time.Second, DialTimeout: time.Second, ReadTimeout: time.Second,
		IdleWindow: 200 * time.Millisecond, MaxTotal: time.Second,
	}
	for i := 0; i < 2; i++ {
		_, frames, err := cl.ReadRegistersDeyeFn(context.Background(), 0x0000, 0x0028, 1, 0x03)
		if err != nil || len(frames) == 0 {
			t.Fatalf("запрос %d: err=%v frames=%d", i+1, err, len(frames))
		}
	}
	if got := srv.accepted.Load(); got != 2 {
		t.Fatalf("ожидалось новое TCP-соединение на каждый запрос: %d", got)
	}
	deadline := time.Now().Add(time.Second)
	for srv.closed.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.closed.Load(); got != 2 {
		t.Fatalf("ожидалось закрытие соединения после каждого запроса: %d", got)
	}
}

func TestPollDeviceNewConnectionEachCall(t *testing.T) {
	srv := newFakeLogger(t, "deye", 0x3B, 0x3A)
	ip, port := srv.addr()
	dev := inverter{Name: "test", IP: ip, Port: port, Type: "deye", LoggerSN: 123456}
	p := &poller{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if _, _, err := p.pollDevice(ctx, 0, dev, "SN12345678"); err != nil {
			t.Fatalf("запрос %d: %v", i+1, err)
		}
	}
	if got := srv.accepted.Load(); got != 2 {
		t.Fatalf("pollDevice должен открывать новое соединение на вызов: %d", got)
	}
}

func TestPollDeviceNoSerialIsError(t *testing.T) {
	srv := newFakeLogger(t, "deye", 0x3B, 0x3A)
	srv.emptySN = true
	ip, port := srv.addr()
	dev := inverter{Name: "test", IP: ip, Port: port, Type: "deye", LoggerSN: 123456}
	p := &poller{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := p.pollDevice(ctx, 0, dev, ""); err == nil {
		t.Fatal("ожидалась ошибка: серийник не получен")
	}
}

func TestPollOnceNoRetryOnNoData(t *testing.T) {
	srv1 := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	srv1.noResponse = true
	ip1, port1 := srv1.addr()
	srv2 := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	srv2.noResponse = true
	ip2, port2 := srv2.addr()
	dev1 := inverter{Name: "first", IP: ip1, Port: port1, Type: "sofar", LoggerSN: 123456, Valid: true}
	dev2 := inverter{Name: "second", IP: ip2, Port: port2, Type: "sofar", LoggerSN: 654321, Valid: true}
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 2)
	p.lastSeen = make([]string, 2)
	p.energyWarned = make([]bool, 2)
	cfg := settings{PollPeriod: 10, Inverters: []inverter{dev1, dev2}}
	p.pollOnce(context.Background(), cfg, "sofar")
	if got := srv1.accepted.Load(); got != 1 {
		t.Fatalf("no data не должен повторяться: соединений=%d, ожидалось 1", got)
	}
	if got := srv2.accepted.Load(); got != 1 {
		t.Fatalf("после отказа от повтора должен опрашиваться следующий инвертор: соединений=%d, ожидалось 1", got)
	}
	for i := range p.states {
		if p.states[i].status != statusBad || p.states[i].failCount != 1 || p.states[i].timeouts != 1 {
			t.Fatalf("состояние #%d после no data: %+v", i+1, p.states[i])
		}
	}
}

func TestPollOnceRetriesBadBlock(t *testing.T) {
	srv := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	srv.badBlock = true
	ip, port := srv.addr()
	dev := inverter{Name: "test", IP: ip, Port: port, Type: "sofar", LoggerSN: 123456, Valid: true}
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	p.lastSeen = make([]string, 1)
	p.energyWarned = make([]bool, 1)
	cfg := settings{PollPeriod: 10, Inverters: []inverter{dev}}
	p.pollOnce(context.Background(), cfg, "sofar")
	if got := srv.accepted.Load(); got != 2 {
		t.Fatalf("битый PDU должен повторяться: соединений=%d, ожидалось 2", got)
	}
	if p.states[0].status != statusBad || p.states[0].failCount != 1 {
		t.Fatalf("после двух битых ответов: %+v", p.states[0])
	}
}

func TestPollDeviceSofarSmoke(t *testing.T) {
	srv := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	ip, port := srv.addr()
	dev := inverter{Name: "test", IP: ip, Port: port, Type: "sofar", LoggerSN: 123456}
	p := &poller{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, sn, err := p.pollDevice(ctx, 0, dev, "")
	if err != nil {
		t.Fatal(err)
	}
	if sn != "SA3ES000000001" {
		t.Fatalf("sn=%q", sn)
	}
}

func TestPollDeviceDeyeSmoke(t *testing.T) {
	srv := newFakeLogger(t, "deye", 0x3B, 0x3A)
	ip, port := srv.addr()
	dev := inverter{Name: "test", IP: ip, Port: port, Type: "deye", LoggerSN: 123456}
	p := &poller{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tm, sn, err := p.pollDevice(ctx, 0, dev, "")
	if err != nil {
		t.Fatal(err)
	}
	if sn != "SN12345678" {
		t.Fatalf("sn=%q", sn)
	}
	// fake отдаёт значения 1..58 по порядку адресов 0x3B..0x74.
	if tm.vals[0] != 0x6D-0x3B+1 || tm.vals[17] != 1 {
		t.Fatalf("vals: %+v", tm.vals[:20])
	}
}

func TestParallelInverterWritesSeparateSHMSlots(t *testing.T) {
	dir := t.TempDir()
	shm, err := shmOpen(filepath.Join(dir, "grid-state"))
	if err != nil {
		t.Fatal(err)
	}
	defer shm.Close()
	srv1 := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	srv1.snText = "SA3ES111"
	srv2 := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	srv2.snText = "SA3ES222"
	ip1, port1 := srv1.addr()
	ip2, port2 := srv2.addr()
	cfg := settings{PollPeriod: 10, Inverters: []inverter{
		{Name: "one", IP: ip1, Port: port1, Type: "sofar", LoggerSN: 1, Valid: true},
		{Name: "two", IP: ip2, Port: port2, Type: "sofar", LoggerSN: 2, Valid: true},
	}}
	p := newPoller(shm, nil, logx.New("test: "))
	p.reset(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p.pollOne(context.Background(), cfg, i)
		}(i)
	}
	wg.Wait()
	doc := listDoc(shm)
	if len(doc.Inverters) != 2 {
		t.Fatalf("shm list: %+v", doc)
	}
	byID := map[int]GridView{}
	for _, it := range doc.Inverters {
		byID[it.ID] = it
	}
	if byID[0].Serial != "SA3ES111" || byID[1].Serial != "SA3ES222" {
		t.Fatalf("serial попал не в свой слот: %+v", doc.Inverters)
	}
	if byID[0].Addr != net.JoinHostPort(ip1, strconv.Itoa(port1)) ||
		byID[1].Addr != net.JoinHostPort(ip2, strconv.Itoa(port2)) {
		t.Fatalf("addr/слот: %+v", doc.Inverters)
	}
	if byID[0].Status != statusOK || byID[1].Status != statusOK {
		t.Fatalf("statuses: %+v", doc.Inverters)
	}
}

func TestPollInverterExitsOnLongBan(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	p.lastSeen = make([]string, 1)
	st := &p.states[0]
	st.banStatus = statusBan5
	st.banUntil = time.Now().Add(time.Minute)
	cfg := settings{PollPeriod: 10, Inverters: []inverter{{Name: "x", Type: "sofar", Valid: true}}}
	ban := p.pollInverter(context.Background(), cfg, 0)
	if ban.IsZero() || !ban.Equal(st.banUntil) {
		t.Fatalf("ожидалось завершение worker по бану: %v", ban)
	}
}

func TestRunInvertersRestartsWorkerAfterLongBan(t *testing.T) {
	srv := newFakeLogger(t, "sofar", 0x0000, 0x0028)
	ip, port := srv.addr()
	shm, err := shmOpen(filepath.Join(t.TempDir(), "grid"))
	if err != nil {
		t.Fatal(err)
	}
	defer shm.Close()
	cfg := settings{PollPeriod: 30, Inverters: []inverter{
		{Name: "x", IP: ip, Port: port, Type: "sofar", LoggerSN: 1, Valid: true},
	}}
	p := newPoller(shm, nil, logx.New("test: "))
	p.reset(cfg)
	st := &p.states[0]
	st.banStatus = statusBan5
	st.banUntil = time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.runInverters(ctx, cfg)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for srv.accepted.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.accepted.Load() < 2 {
		cancel()
		<-done
		t.Fatalf("после бана worker не продолжил опрос: соединений=%d", srv.accepted.Load())
	}
	deadline = time.Now().Add(2 * time.Second)
	var doc StateDoc
	for time.Now().Before(deadline) {
		doc = listDoc(shm)
		if len(doc.Inverters) == 1 && doc.Inverters[0].Status == statusOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(doc.Inverters) != 1 || doc.Inverters[0].Status != statusOK {
		cancel()
		<-done
		t.Fatalf("shm после перезапуска: %+v", doc.Inverters)
	}
	n := srv.accepted.Load()
	time.Sleep(300 * time.Millisecond)
	if got := srv.accepted.Load(); got > n {
		cancel()
		<-done
		t.Fatalf("hot loop после бана: соединений=%d -> %d", n, got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("оркестратор не остановился")
	}
}
