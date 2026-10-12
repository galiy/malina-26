package main

import (
	"net"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/microart"
)

// readFrame читает кадр ответа до байта 0x0A (экранированный 0x0A в поток не попадает).
func readFrame(t *testing.T, c net.Conn) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	out := make([]byte, 0, 32)
	buf := make([]byte, 1)
	for {
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("read frame: %v (накоплено %x)", err, out)
		}
		out = append(out, buf[0])
		if buf[0] == 0x0A {
			return out
		}
	}
}

// TestServeDeviceNonEchoClient — клиент НЕ эхует ответы устройства (как legacy mpptd):
// кадры обрабатываются и после ответа, без «залипания» счётчика эха.
func TestServeDeviceNonEchoClient(t *testing.T) {
	emuSide, clientSide := net.Pipe()
	defer clientSide.Close()
	stop := make(chan struct{})
	defer close(stop)

	d := newDevice(0, 0x053E, 0x63, 0x0E3D, nil, 0)
	go serveDevice(d, emuSide, stop)
	time.Sleep(20 * time.Millisecond)

	req := microart.BuildReadRequest(0x0000, 1)

	for i := 0; i < 2; i++ {
		// net.Pipe синхронный: пишем запрос в горутине, читаем эхо/ответ в основной.
		done := make(chan struct{})
		go func() {
			_, _ = clientSide.Write(req)
			close(done)
		}()
		readFrame(t, clientSide) // эхо запроса (завершается 0x0A запроса)
		reply := readFrame(t, clientSide)
		<-done
		if reply[0] == 'e' { // 'e' = кадр ошибки
			t.Fatalf("запрос %d: ответ-ошибка %x", i+1, reply)
		}
	}
}
