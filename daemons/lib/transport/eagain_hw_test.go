//go:build linux && hwtest

package transport

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestHardwareWriteEAGAIN — ручной тест на реальном USB-serial:
// PORT=/dev/ttyUSB2 BAUD=57600 go test -tags hwtest -run TestHardwareWriteEAGAIN.
// Пишет валидные MicroArt-запросы чтения в порт и требует, чтобы WriteAll
// хотя бы раз прошёл через EAGAIN (буфер неблокирующего порта заполняется).
func TestHardwareWriteEAGAIN(t *testing.T) {
	port := os.Getenv("PORT")
	if port == "" {
		t.Skip("PORT не задан (например PORT=/dev/ttyUSB2)")
	}
	baud := 57600
	if v := os.Getenv("BAUD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("BAUD=%q: %v", v, err)
		}
		baud = n
	}
	p, err := Open(port)
	if err != nil {
		t.Fatalf("Open(%s): %v", port, err)
	}
	defer p.Close()
	if err := p.SetBaud(baud); err != nil {
		t.Fatalf("SetBaud(%d): %v", baud, err)
	}

	req := []byte{'r', 0, 0, 0, 0x8E, '\n'} // MicroArt read addr=0, page=0
	before := EAGAINRetries()
	start := time.Now()
	deadline := start.Add(10 * time.Second)
	writes := 0
	for time.Now().Before(deadline) {
		if err := p.WriteAll(req); err != nil {
			t.Fatalf("WriteAll после %d записей: %v", writes, err)
		}
		writes++
	}
	after := EAGAINRetries()
	t.Logf("port=%s baud=%d writes=%d bytes=%d elapsed=%s eagain=%d",
		port, baud, writes, writes*len(req), time.Since(start), after-before)
	if after == before {
		t.Fatalf("EAGAIN не наблюдался: путь WriteAll/EAGAIN не проверен")
	}
}
