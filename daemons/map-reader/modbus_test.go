package main

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPTYForMap — пара master/slave для тестов Modbus.
func openPTYForMap(t *testing.T) (string, int) {
	t.Helper()
	mfd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("нет /dev/ptmx: %v", err)
	}
	var unlock int32
	if err := modbusIoctl(mfd, 0x40045431 /* TIOCSPTLCK */, uintptr(unsafe.Pointer(&unlock))); err != nil {
		syscall.Close(mfd)
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var n uint32
	if err := modbusIoctl(mfd, 0x80045430 /* TIOCGPTN */, uintptr(unsafe.Pointer(&n))); err != nil {
		syscall.Close(mfd)
		t.Skipf("TIOCGPTN: %v", err)
	}
	return fmt.Sprintf("/dev/pts/%d", n), mfd
}

func modbusIoctl(fd int, req, arg uintptr) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if e != 0 {
		return e
	}
	return nil
}

func readN(fd int, n int) []byte {
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		m, err := syscall.Read(fd, buf[:n-len(out)])
		if m > 0 {
			out = append(out, buf[:m]...)
		}
		if err != nil && err != syscall.EINTR && err != syscall.EAGAIN {
			return out
		}
	}
	return out
}

func TestModbusReadRegistersChunked(t *testing.T) {
	slave, master := openPTYForMap(t)
	defer syscall.Close(master)

	done := make(chan error, 1)
	go func() {
		req := readN(master, 8)
		if len(req) != 8 {
			done <- fmt.Errorf("запрос %d Б: %x", len(req), req)
			return
		}
		count := int(req[4])<<8 | int(req[5])
		if count != 4 {
			done <- fmt.Errorf("count=%d", count)
			return
		}
		resp := []byte{req[0], 0x03, byte(count * 2)}
		for i := 0; i < count*2; i++ {
			resp = append(resp, byte(0x10+i))
		}
		c := crc16(resp)
		resp = append(resp, byte(c), byte(c>>8))
		resp = append(resp, 0xFF) // хвост за кадром не должен попасть в CRC
		// Отдаём по частям: заголовок, затем данные+CRC.
		if _, err := syscall.Write(master, resp[:3]); err != nil {
			done <- err
			return
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := syscall.Write(master, resp[3:]); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	p, err := openPort(slave)
	if err != nil {
		t.Skipf("openPort(%s): %v", slave, err)
	}
	defer p.Close()
	data, err := modbusReadRegisters(p, 1, 0, 4, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 8 {
		t.Fatalf("data=%d, ожидалось 8: %x", len(data), data)
	}
	for i := range data {
		if data[i] != byte(0x10+i) {
			t.Fatalf("data[%d]=%#x", i, data[i])
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestModbusExceptionFrame(t *testing.T) {
	slave, master := openPTYForMap(t)
	defer syscall.Close(master)

	go func() {
		req := readN(master, 8)
		if len(req) != 8 {
			return
		}
		resp := []byte{req[0], 0x83, 0x02} // exception: illegal data address
		c := crc16(resp)
		resp = append(resp, byte(c), byte(c>>8))
		_, _ = syscall.Write(master, resp)
	}()

	p, err := openPort(slave)
	if err != nil {
		t.Skipf("openPort(%s): %v", slave, err)
	}
	defer p.Close()
	_, err = modbusReadRegisters(p, 1, 0, 4, 500*time.Millisecond)
	if !errors.Is(err, errModbusExc) {
		t.Fatalf("ожидали errModbusExc, получили %v", err)
	}
}

func TestModbusWriteExchange(t *testing.T) {
	slave, master := openPTYForMap(t)
	defer syscall.Close(master)

	req := []byte{0x01, 0x06, 0x00, 0x01, 0x00, 0x02}
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))

	go func() {
		got := readN(master, len(req))
		if len(got) != len(req) {
			return
		}
		_, _ = syscall.Write(master, got) // echo-ответ = запрос
	}()

	p, err := openPort(slave)
	if err != nil {
		t.Skipf("openPort(%s): %v", slave, err)
	}
	defer p.Close()
	if err := modbusWriteExchange(p, req, len(req), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestModbusWriteException(t *testing.T) {
	slave, master := openPTYForMap(t)
	defer syscall.Close(master)

	req := []byte{0x01, 0x06, 0x00, 0x01, 0x00, 0x02}
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))

	go func() {
		if got := readN(master, len(req)); len(got) != len(req) {
			return
		}
		resp := []byte{0x01, 0x86, 0x02}
		cc := crc16(resp)
		resp = append(resp, byte(cc), byte(cc>>8))
		resp = append(resp, 0xFF) // хвост за кадром исключения
		_, _ = syscall.Write(master, resp)
	}()

	p, err := openPort(slave)
	if err != nil {
		t.Skipf("openPort(%s): %v", slave, err)
	}
	defer p.Close()
	err = modbusWriteExchange(p, req, len(req), 500*time.Millisecond)
	var me *modbusError
	if !errors.As(err, &me) || me.code != 2 {
		t.Fatalf("ожидали modbusError 2, получили %v", err)
	}
}

// TestModbusReadRegistersTailInFirstPacket — кадр и хвост приходят одной
// пачкой; чтение должно обрезать resp до длины кадра до CRC.
func TestModbusReadRegistersTailInFirstPacket(t *testing.T) {
	slave, master := openPTYForMap(t)
	defer syscall.Close(master)

	go func() {
		req := readN(master, 8)
		if len(req) != 8 {
			return
		}
		count := int(req[4])<<8 | int(req[5])
		resp := []byte{req[0], 0x03, byte(count * 2)}
		for i := 0; i < count*2; i++ {
			resp = append(resp, byte(i))
		}
		c := crc16(resp)
		resp = append(resp, byte(c), byte(c>>8))
		resp = append(resp, 0xAA, 0xBB) // хвост в той же пачке
		_, _ = syscall.Write(master, resp)
	}()

	p, err := openPort(slave)
	if err != nil {
		t.Skipf("openPort(%s): %v", slave, err)
	}
	defer p.Close()
	data, err := modbusReadRegisters(p, 1, 0, 8, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 16 {
		t.Fatalf("data=%d, ожидалось 16", len(data))
	}
	for i := range data {
		if data[i] != byte(i) {
			t.Fatalf("data[%d]=%#x", i, data[i])
		}
	}
}
