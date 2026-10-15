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
// Не знает, serial это или TCP (arch §2/§5).
type Transport interface {
	Read(p []byte) (int, error)
	Write(p []byte) error
	SetReadTimeout(d time.Duration)
	Close() error
}

// --- serial ---

type serialTransport struct {
	p  *transport.Port
	to time.Duration
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
	return &serialTransport{p: p, to: 500 * time.Millisecond}, nil
}

func (t *serialTransport) SetReadTimeout(d time.Duration) { t.to = d }

// Read собирает байты до заполнения буфера или таймаута (побайтово,
// через терминал с VMIN/VTIME в lib/transport).
func (t *serialTransport) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.to)
	n := 0
	for n < len(p) {
		if !time.Now().Before(deadline) {
			break
		}
		b, ok := t.p.ReadOne(deadline)
		if !ok {
			break
		}
		p[n] = b
		n++
	}
	if n == 0 {
		return 0, errTimeout
	}
	return n, nil
}

func (t *serialTransport) Write(p []byte) error { return t.p.WriteAll(p) }
func (t *serialTransport) Close() error         { t.p.Close(); return nil }

// --- TCP ---

type tcpTransport struct {
	c  net.Conn
	to time.Duration
}

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
