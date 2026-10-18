package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/galiy/malina-26/daemons/lib/microart"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

const (
	tiocsptlck = 0x40045431
	tiocgptn   = 0x80045430
)

func ioctlPtr(fd uintptr, req uintptr, p unsafe.Pointer) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(p))
	if e != 0 {
		return e
	}
	return nil
}

// openPTYPair создаёт пару PTY (master/slave) и возвращает master и путь slave.
func openPTYPair(t *testing.T) (*os.File, string) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("ptmx: %v", err)
	}
	var unlock uint32
	if err := ioctlPtr(m.Fd(), tiocsptlck, unsafe.Pointer(&unlock)); err != nil {
		m.Close()
		t.Fatalf("tiocsptlck: %v", err)
	}
	var ptn uint32
	if err := ioctlPtr(m.Fd(), tiocgptn, unsafe.Pointer(&ptn)); err != nil {
		m.Close()
		t.Fatalf("tiocgptn: %v", err)
	}
	slave := fmt.Sprintf("/dev/pts/%d", ptn)
	_ = os.Chmod(slave, 0o666)
	return m, slave
}

// fakeMap — виртуальный МАП поверх EEPROM-образа (адрес < 0x400 → «как есть»).
// Отвечает в протоколе Микроарт: эхо запроса + ответ на '\n'.
type fakeMap struct {
	eep       [0x400]byte
	failFirst bool // ответить кадром ошибки на первый запрос (эффект смены бода)
}

func (f *fakeMap) serve(master io.ReadWriteCloser, stop <-chan struct{}) {
	go func() { <-stop; master.Close() }()
	buf := make([]byte, 1)
	frame := make([]byte, 0, 64)
	var sent []byte
	for {
		n, err := master.Read(buf)
		if n != 1 {
			return
		}
		_ = err
		b := buf[0]
		if len(sent) > 0 && b == sent[0] {
			sent = sent[1:]
			continue
		}
		sent = nil
		if _, werr := master.Write([]byte{b}); werr != nil { // эхо запроса
			return
		}
		if b != 0x0A {
			frame = append(frame, b)
			continue
		}
		req, perr := microart.ParseRequestFrame(frame)
		frame = frame[:0]
		var reply []byte
		if f.failFirst {
			f.failFirst = false
			reply = microart.BuildErrorReply(0x04)
		} else if perr != nil {
			reply = microart.BuildErrorReply(0x01)
		} else if req.Cmd == 'r' {
			data := make([]byte, int(req.Page)+1)
			for i := range data {
				data[i] = f.eep[req.Addr+uint16(i)]
			}
			reply = microart.BuildReply(data)
		} else {
			reply = microart.BuildErrorReply(0x02)
		}
		sent = reply
		if _, werr := master.Write(reply); werr != nil {
			return
		}
	}
}

// TestProbeMicroartPTY — двухшаговая проба: _Device==3 (МАП) → читаем полную
// сигнатуру, получаем mapInfo (протокол/модель/серийник).
func TestProbeMicroartPTY(t *testing.T) {
	f := &fakeMap{}
	f.eep[0x0000] = 3    // _Device = МАП
	f.eep[0x0001] = 0x00 // VerPow (без бита гибрида)
	f.eep[0x0002] = 0x84 // VerPO
	f.eep[0x0007] = 3    // DevOpt = Титанатор -> модель T
	f.eep[0x0018] = 0x05 // SerialNum0
	f.eep[0x0019] = 0x90 // SerialNum1
	f.eep[0x0023] = 'A'  // SerialNum3 (буква)

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	info, err := probeMicroart(p, 19200, 200*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("probeMicroart: %v", err)
	}
	if info == nil {
		t.Fatal("info=nil, ждали mapInfo")
	}
	if info.Protocol != "microart" {
		t.Fatalf("Protocol=%q, ждали \"microart\"", info.Protocol)
	}
	if info.Model != "T" {
		t.Fatalf("Model=%q, ждали \"T\" (DevOpt=3)", info.Model)
	}
	if info.Serial != 0x9005 {
		t.Fatalf("Serial=%#x, ждали 0x9005", info.Serial)
	}
	if info.SerialLetter != 'A' {
		t.Fatalf("SerialLetter=%q, ждали 'A'", info.SerialLetter)
	}
}

// TestProbeMicroartForeign — _Device==5 (КЭС/MPPT): проба должна мгновенно
// вернуть errForeignMicroArt (порт отпускается, Modbus не шлём).
func TestProbeMicroartForeign(t *testing.T) {
	f := &fakeMap{}
	f.eep[0x0000] = 5 // _Device = КЭС/MPPT

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	_, err = probeMicroart(p, 19200, 200*time.Millisecond, 200*time.Millisecond)
	if !errors.Is(err, errForeignMicroArt) {
		t.Fatalf("err=%v, ждали errForeignMicroArt", err)
	}
}

// TestProbeMicroartRetryAfterDeviceError — первый кадр после смены бода МАП
// отклоняет ошибкой e04 (эффект «мусора» на чужой скорости). Проба должна
// повторить запрос на той же скорости и найти устройство (см. probeMicroart).
// TestFlushBeforeFrame — предкадровый Flush должен очищать накопленный мусор,
// иначе первый байт побайтового эха берётся из буфера и кадр сбивается.
func TestFlushBeforeFrame(t *testing.T) {
	master, slave := openPTYPair(t)
	defer master.Close()
	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	if _, err := master.Write([]byte("garbage")); err != nil {
		t.Fatalf("write master: %v", err)
	}
	out, _ := p.ReadUntilNL(time.Now().Add(200*time.Millisecond), 64, false)
	if len(out) == 0 {
		t.Fatal("без Flush мусор не прочитан — тест не воспроизводит условие")
	}

	master2, slave2 := openPTYPair(t)
	defer master2.Close()
	p2, err := transport.Open(slave2)
	if err != nil {
		t.Fatalf("open slave2: %v", err)
	}
	defer p2.Close()

	p2.SetFlushBeforeFrame(true)
	if _, err := master2.Write([]byte("garbage")); err != nil {
		t.Fatalf("write master2: %v", err)
	}
	p2.FlushBeforeFrame()
	out, _ = p2.ReadUntilNL(time.Now().Add(150*time.Millisecond), 64, false)
	if len(out) != 0 {
		t.Fatalf("после Flush прочитано %q, ждали пусто", out)
	}
}

func TestProbeMicroartRetryAfterDeviceError(t *testing.T) {
	f := &fakeMap{failFirst: true}
	f.eep[0x0000] = 3 // _Device = МАП

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	info, err := probeMicroart(p, 19200, 200*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("probeMicroart после e04: %v", err)
	}
	if info == nil || info.Baud != 19200 {
		t.Fatalf("info=%+v, ждали МАП на 19200", info)
	}
}
