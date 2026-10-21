package main

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Коды framing (arch §8): как говорить с источником.
const (
	framingNative uint8 = 0 // нативный протокол (напр. PACE/ANT)
	framingMRTU   uint8 = 1 // MODBUS-RTU (CRC)
	framingMTCP   uint8 = 2 // MODBUS-TCP (MBAP)
)

var errTimeout = errors.New("bms: таймаут чтения")

// Transport — источник с байтовым вводом/выводом и таймаутом чтения.
// Read возвращает доступную пачку (0 < n <= len(p)); заполнение всего буфера
// не гарантируется — вызывающий обязан накапливать (readAntFrame/readPace).
// Не знает, serial это или TCP (arch §2/§5).
type Transport interface {
	Read(p []byte) (int, error)
	Write(p []byte) error
	SetReadTimeout(d time.Duration)
	Close() error
}

// --- serial ---

// serialPort — минимальный интерфейс Port для тестов (реальный — *transport.Port).
type serialPort interface {
	Read([]byte, time.Time) (int, error)
	WriteAll([]byte) error
	Close()
}

// ioTimeout — таймаут ожидания ответа по скорости порта (arch §10).
// 9600–19199 → 700 мс, 19200–57599 → 500 мс, ≥57600 → 200 мс; ниже 9600 —
// пропорционально 700 мс (и всегда больше 700). Неизвестная скорость (TCP) —
// 500 мс. Меньше 200 мс не бывает ни в одной ветке.
func ioTimeout(baud int) time.Duration {
	switch {
	case baud <= 0:
		return 500 * time.Millisecond
	case baud < 9600:
		return 700 * time.Millisecond * time.Duration(9600) / time.Duration(baud)
	case baud < 19200:
		return 700 * time.Millisecond
	case baud < 57600:
		return 500 * time.Millisecond
	default:
		return 200 * time.Millisecond
	}
}

type serialTransport struct {
	p   serialPort
	to  time.Duration
	def time.Duration // штатный таймаут для этой скорости (не меняется SetReadTimeout)
}

func openSerial(path string, baud int) (Transport, error) {
	p, err := transport.Open(path)
	if err != nil {
		return nil, err
	}
	if err := p.SetBaud(baud); err != nil {
		p.Close()
		return nil, err
	}
	p.Flush()
	d := ioTimeout(baud)
	return &serialTransport{p: p, to: d, def: d}, nil
}

func (t *serialTransport) SetReadTimeout(d time.Duration) { t.to = d }

// DefaultTimeout — штатный таймаут опроса для этой скорости.
func (t *serialTransport) DefaultTimeout() time.Duration { return t.def }

// Read читает доступную пачку байт одним syscall.Read (Port.Read).
// Дедлайн сохраняется; VMIN/VTIME и select — в lib/transport.
func (t *serialTransport) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.to)
	n, err := t.p.Read(p, deadline)
	if n > 0 {
		return n, nil
	}
	if err != nil {
		if errors.Is(err, transport.ErrTimeout) {
			return 0, errTimeout
		}
		return 0, err
	}
	return 0, errTimeout
}

func (t *serialTransport) Write(p []byte) error { return t.p.WriteAll(p) }
func (t *serialTransport) Close() error         { t.p.Close(); return nil }

// Flush сбрасывает входной буфер порта. Для пассивного потока AntBMS
// применяется перед опросом: накопленный FIFO (до 640 КиБ) отбрасывается,
// публикуется свежий кадр, а не голова очереди.
func (t *serialTransport) Flush() {
	if p, ok := t.p.(interface{ Flush() }); ok {
		p.Flush()
	}
}

// --- TCP ---

type tcpTransport struct {
	c  net.Conn
	to time.Duration
}

// DefaultTimeout — для TCP скорость неизвестна, оставляем 500 мс.
func (t *tcpTransport) DefaultTimeout() time.Duration { return 500 * time.Millisecond }

func openTCP(host string, port int) (Transport, error) {
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("bms: неверный адрес %s:%d", host, port)
	}
	// arch §10: одна попытка с небольшим таймаутом (500 мс — SYN на нагруженном Wi-Fi).
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return &tcpTransport{c: c, to: 500 * time.Millisecond}, nil
}

func (t *tcpTransport) SetReadTimeout(d time.Duration) { t.to = d }

func (t *tcpTransport) Read(p []byte) (int, error) {
	_ = t.c.SetReadDeadline(time.Now().Add(t.to))
	n, err := t.c.Read(p)
	if n == 0 && err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return 0, errTimeout
		}
		return 0, err
	}
	return n, err
}

func (t *tcpTransport) Write(p []byte) error {
	_ = t.c.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := t.c.Write(p)
	return err
}

func (t *tcpTransport) Close() error { return t.c.Close() }

// openTransport открывает источник: serial по пути или TCP по host:port.
func openTransport(serialPath string, baud int, host string, port int) (Transport, error) {
	if serialPath != "" {
		return openSerial(serialPath, baud)
	}
	return openTCP(host, port)
}
