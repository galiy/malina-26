package main

import (
	"encoding/binary"
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

// fakeKes — виртуальный КЭС поверх образа [2048]byte с отображением dev→RAM
// (dev≥0x1F0 → dev−0x1F0, иначе dev), как у kes-emu.
type fakeKes struct{ img [0x1080]byte }

func (f *fakeKes) at(dev uint16) byte {
	if dev >= 0x1F0 {
		return f.img[dev-0x1F0]
	}
	return f.img[dev]
}

// serve обслуживает master в протоколе Микроарт: эхо запроса + ответ на '\n'.
// Клиент (microart.Read) эхует байты ответа — их игнорируем (см. kes-emu serveDevice).
func (f *fakeKes) serve(master io.ReadWriteCloser, stop <-chan struct{}) {
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
		if perr != nil {
			reply = microart.BuildErrorReply(0x01)
		} else if req.Cmd == 'r' {
			data := make([]byte, int(req.Page)+1)
			for i := range data {
				data[i] = f.at(req.Addr + uint16(i))
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

// TestReadAndPublishE2E — сквозной тест чтения регионов и подготовки публикуемого
// payload (KESE+KESR+KESS) из виртуального КЭС на PTY.
func TestReadAndPublishE2E(t *testing.T) {
	f := &fakeKes{}
	// Заполняем те же ячейки, что читает контракт (RAM-индексы).
	set := func(ram uint16, v byte) { f.img[ram] = v }
	put16 := func(ram uint16, v uint16) { set(ram, byte(v)); set(ram+1, byte(v>>8)) }
	set(0x0000, 5)       // _Device = КЭС
	set(0x0001, 0x02)    // Pow
	set(0x0002, 0xA8)    // FW
	set(0x0005, 0x32)    // ModelPow (×100)
	set(0x009B, 0x04)    // ModelVolt (×12 → 48В)
	set(0x000C, 0x01)    // Pic
	set(0x000D, 0x01)    // Net
	put16(0x2C9, 0x1234) // Serial
	put16(0x2D9, 1234)   // VcPV
	put16(0x2E5, 567)    // VBat
	set(0x31D, 0x5A)     // TempInt
	set(0x3E3, 0x05)     // RelayC
	set(0x3B6, 0x02)     // RSErrSis
	set(0x210, 0x04)     // настройка Roll_H_Speed
	put16(0x250, 3)      // настройка Bat_K_Temp (2 байта)
	set(0xD5A, 0x07)     // I2C_Err

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("открыть slave: %v", err)
	}
	defer p.Close()
	if err := p.SetBaud(19200); err != nil {
		t.Fatalf("SetBaud: %v", err)
	}

	img, err := readAllWindows(p, 200*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("readAllWindows: %v", err)
	}

	info := parseKes(img)
	d := parseData(img)
	if info.Serial != 0x1234 || info.Device != 5 {
		t.Fatalf("parseKes: %+v", info)
	}
	if d.VcPV != 1234 || d.VBat != 567 || d.RelayC != 0x05 || d.RSErrSis != 0x02 || d.TempInt != 0x5A {
		t.Fatalf("parseData измерения: %+v", d)
	}
	if d.ModelVolt != 0x04 || d.ModelPow != 0x32 || d.FW != 0xA8 {
		t.Fatalf("parseData параметры: %+v", d)
	}
	if d.Settings[0] != 0x04 || d.Settings[39] != 3 {
		t.Fatalf("настройки: %+v", d.Settings)
	}
	if d.I2CErr != 0x07 {
		t.Fatalf("I2CErr=%#x", d.I2CErr)
	}

	// Публикуемый payload: [KESE][KESR][KESS].
	pld := buildPayload(contractMeta{Serial: info.Serial, LastSeen: time.Unix(1, 0)}, d)
	if len(pld) != memberKeseSize+memberKesrSize+memberKessSize {
		t.Fatalf("len payload=%d, ждали %d", len(pld), memberKeseSize+memberKesrSize+memberKessSize)
	}
	if string(pld[0:4]) != "KESE" {
		t.Fatalf("magic KESE: %q", pld[0:4])
	}
	if got := binary.LittleEndian.Uint16(pld[6:8]); got != 0x1234 {
		t.Fatalf("payload Serial=%#x", got)
	}
	ramStart := memberKeseSize
	if string(pld[ramStart:ramStart+4]) != "KESR" {
		t.Fatalf("magic KESR: %q", pld[ramStart:ramStart+4])
	}
	setStart := ramStart + memberKesrSize
	if string(pld[setStart:setStart+4]) != "KESS" {
		t.Fatalf("magic KESS: %q", pld[setStart:setStart+4])
	}
}
