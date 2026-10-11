// Package transport — общий последовательный транспорт (raw 8N1) для демонов
// ПАК «Малина» (map-reader, kes-reader, kes-emu).
//
// Инкапсулирует открытие порта, настройку скорости (стандартные termios и
// нестандартные RS485 через termios2/BOTHER), побайтовое чтение с дедлайном,
// чтение до '\n' (с опциональным эхо — для протокола Микроарт) и запись.
package transport

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

// Поддерживаемые скорости. Стандартные — через termios; нестандартные RS485
// (312500/625000) — через termios2/BOTHER.
var baudTable = map[int]uint32{
	9600:   syscall.B9600,
	19200:  syscall.B19200,
	57600:  syscall.B57600,
	115200: syscall.B115200,
}

// Нестандартные скорости (RS485): задаются через termios2 c BOTHER.
var baudOther = map[int]bool{312500: true, 625000: true}

const (
	tcgets2   = 0x802C542A
	tcsets2   = 0x402C542B
	cbaudMask = 0x100F
	bother    = 0x1000
)

type termios2 struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	Cc                         [19]uint8
	Ispeed, Ospeed             uint32
}

// rs485Direction — включать ли аппаратный режим RS485 (RTS/DE) на порту.
// По умолчанию ВЫКЛЮЧЕНО: автонаправление в USB-адаптерах работает само, а на
// GPIO-UART (наш МАП по RS232) принудительный RS485 сломал бы линию.
const rs485Direction = false

// RS485-режим ядра (linux serial_rs485). Номера ioctl — plain (без size).
const (
	tiocgrs485           = 0x542E
	tiocsrs485           = 0x542F
	serRS485Enabled      = 1 << 0
	serRS485RTSOnSend    = 1 << 1
	serRS485RTSAfterSend = 1 << 2
)

type serialRS485 struct {
	Flags              uint32
	DelayRTSBeforeSend uint32
	DelayRTSAfterSend  uint32
	Pad                [5]uint32
}

// TCFLSH/TCIOFLUSH нет в пакете syscall — значения из asm-generic/ioctls.h.
const (
	tcflsh    = 0x540B
	tcioflush = 2
)

// ErrClosed возвращается при операциях с закрытым портом.
var ErrClosed = errors.New("порт закрыт")

// Port — открытый последовательный порт (raw, 8N1, non-blocking).
type Port struct {
	path string
	fd   int
	baud int
}

// Open открывает порт и переводит его на скорость по умолчанию (115200).
//
// O_RDWR, а не O_RDONLY: иначе tcsetattr не применяет скорость на части
// USB-serial адаптеров. Открываем в блокирующем режиме; таймаут чтения
// задаётся через VTIME.
func Open(path string) (*Port, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	p := &Port{path: path, fd: fd}
	if err := p.SetBaud(115200); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if rs485Direction {
		p.enableRS485()
	}
	return p, nil
}

// Close закрывает порт.
func (p *Port) Close() {
	if p.fd >= 0 {
		syscall.Close(p.fd)
		p.fd = -1
	}
}

// Path возвращает путь устройства.
func (p *Port) Path() string { return p.path }

// Baud возвращает текущую скорость.
func (p *Port) Baud() int { return p.baud }

// enableRS485 пытается включить аппаратный режим RS485 (если поддерживается).
func (p *Port) enableRS485() {
	var s serialRS485
	if err := ioctl(p.fd, tiocgrs485, uintptr(unsafe.Pointer(&s))); err != nil {
		return // не RS485-порт (USB-автонаправление) — не трогаем
	}
	s.Flags |= serRS485Enabled | serRS485RTSOnSend | serRS485RTSAfterSend
	s.DelayRTSBeforeSend = 0
	s.DelayRTSAfterSend = 0
	_ = ioctl(p.fd, tiocsrs485, uintptr(unsafe.Pointer(&s)))
}

func ioctl(fd int, req uintptr, arg uintptr) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if e != 0 {
		return e
	}
	return nil
}

// SetBaud переводит порт в raw 8N1 на заданной скорости.
func (p *Port) SetBaud(baud int) error {
	if p.fd < 0 {
		return ErrClosed
	}
	bc, ok := baudTable[baud]
	if !ok {
		if baudOther[baud] {
			return p.setBaudOther(baud)
		}
		return errors.New("неподдерживаемая скорость")
	}
	var t syscall.Termios
	if err := ioctl(p.fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	t.Iflag = 0
	t.Oflag = 0
	t.Lflag = 0
	t.Cflag = syscall.CLOCAL | syscall.CREAD | syscall.CS8 | bc
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 1 // таймаут чтения 100 мс (десятые доли секунды)
	if err := ioctl(p.fd, syscall.TCSETS, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	p.Flush()
	p.baud = baud
	return nil
}

// setBaudOther задаёт нестандартную скорость (RS485: 312500/625000) через
// termios2 с BOTHER.
func (p *Port) setBaudOther(baud int) error {
	if p.fd < 0 {
		return ErrClosed
	}
	var t termios2
	if err := ioctl(p.fd, tcgets2, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	t.Iflag = 0
	t.Oflag = 0
	t.Lflag = 0
	t.Cflag = (t.Cflag &^ cbaudMask) | bother | syscall.CLOCAL | syscall.CREAD | syscall.CS8
	t.Ispeed = uint32(baud)
	t.Ospeed = uint32(baud)
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 1 // таймаут чтения 100 мс, иначе блокировка без дедлайна
	if err := ioctl(p.fd, tcsets2, uintptr(unsafe.Pointer(&t))); err != nil {
		return err
	}
	p.Flush()
	p.baud = baud
	return nil
}

// Flush сбрасывает входной/выходной буфер порта.
func (p *Port) Flush() {
	if p.fd < 0 {
		return
	}
	_ = ioctl(p.fd, tcflsh, uintptr(tcioflush))
}

// WriteAll пишет все байты с таймаутом.
func (p *Port) WriteAll(b []byte) error {
	if p.fd < 0 {
		return ErrClosed
	}
	off := 0
	deadline := time.Now().Add(500 * time.Millisecond)
	for off < len(b) {
		n, err := syscall.Write(p.fd, b[off:])
		if n > 0 {
			off += n
			continue
		}
		if err != nil && (err == syscall.EAGAIN || err == syscall.EINTR || err == syscall.EWOULDBLOCK) {
			if time.Now().After(deadline) {
				return errors.New("write timeout")
			}
			time.Sleep(200 * time.Microsecond)
			continue
		}
		if err == nil {
			continue
		}
		return err
	}
	return nil
}

// ReadOne читает один байт до дедлайна. Блокирующее чтение с таймаутом VTIME:
// ядро возвращает байт сразу по приходу (без polling-пауз) — это важно для
// побайтового эхо-обмена (меньше вероятность ErrRS_eco у МАП).
func (p *Port) ReadOne(deadline time.Time) (byte, bool) {
	if p.fd < 0 {
		return 0, false
	}
	var buf [1]byte
	for time.Now().Before(deadline) {
		n, err := syscall.Read(p.fd, buf[:])
		if n == 1 {
			return buf[0], true
		}
		if err != nil && err != syscall.EINTR && err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			return 0, false
		}
		// n==0 (таймаут VTIME) или EINTR/EAGAIN — короткая пауза и повтор
		time.Sleep(2 * time.Millisecond)
	}
	return 0, false
}

// ReadUntilNL читает байты до первого '\n' (включительно) или дедлайна.
// Если echo=true — каждый принятый байт возвращается эхом в порт (Микроарт).
func (p *Port) ReadUntilNL(deadline time.Time, max int, echo bool) ([]byte, bool) {
	out := make([]byte, 0, max)
	for len(out) < max {
		b, ok := p.ReadOne(deadline)
		if !ok {
			return out, false
		}
		if echo {
			_ = p.WriteAll([]byte{b})
		}
		out = append(out, b)
		if b == 0x0A {
			return out, true
		}
	}
	return out, false
}
