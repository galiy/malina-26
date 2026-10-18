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
	2400:   syscall.B2400,
	9600:   syscall.B9600,
	19200:  syscall.B19200,
	57600:  syscall.B57600,
	115200: syscall.B115200,
	230400: syscall.B230400,
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

	// flushBeforeFrame — сбрасывать буфер перед каждым кадром MicroArt.
	// Включается для моделей, у которых после ответа остаются лишние байты:
	// без сброса побайтовое эхо сбивается и кадр приходится повторять.
	flushBeforeFrame bool
}

// SetFlushBeforeFrame включает/выключает сброс буфера перед кадром.
func (p *Port) SetFlushBeforeFrame(v bool) { p.flushBeforeFrame = v }

// FlushBeforeFrame сбрасывает буфер, если режим включён.
func (p *Port) FlushBeforeFrame() {
	if p.flushBeforeFrame {
		p.Flush()
	}
}

// ErrBusy — порт занят другим держателем (flock LOCK_EX не взяли).
var ErrBusy = errors.New("порт занят")

// Open открывает порт и переводит его на скорость по умолчанию (115200).
//
// O_RDWR, а не O_RDONLY: иначе tcsetattr не применяет скорость на части
// USB-serial адаптеров. Открываем в блокирующем режиме; таймаут чтения
// задаётся через VTIME.
//
// Взаимоисключение демонов на одной линии: при открытии берём `flock(LOCK_EX|LOCK_NB)`
// (снимается при Close). Если порт уже занят другим держателем — возвращаем ErrBusy;
// если flock не поддерживается (прочее), открытие продолжается как раньше.
func Open(path string) (*Port, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	if lerr := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); lerr != nil {
		if lerr == syscall.EWOULDBLOCK || lerr == syscall.EAGAIN {
			syscall.Close(fd)
			return nil, ErrBusy
		}
		// прочее (неподдерживается) — работаем без lock
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
		// n==0 — истёк VTIME (100 мс): read уже блокировался, повторный Sleep не нужен.
		// Короткая пауза — только для неблокирующего режима (EAGAIN), чтобы не крутить busy-loop.
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			time.Sleep(time.Millisecond)
		}
	}
	return 0, false
}

// ReadUntilNL читает байты до первого '\n' (включительно) или дедлайна.
// Микроарт: при echo=true принятые байты возвращаются эхом в порт.
//
// Оптимизация обмена (протокол не изменён): чтение — ПАКЕТНОЕ (один syscall.Read
// на доступную пачку до 64 байт), эхо — ОДНИМ WriteAll на пачку до '\n'
// включительно. Устраняет syscall на каждый байт. Байты после '\n' в прочитанной
// пачке игнорируются (уже считаны из ядра, в буфере не остаются); на исправной
// линии устройство после кадра замолкает, так что буфер не загрязняется.
func (p *Port) ReadUntilNL(deadline time.Time, max int, echo bool) ([]byte, bool) {
	if p.fd < 0 {
		return nil, false
	}
	out := make([]byte, 0, max)
	// окна МАП/КЭС небольшие — пачка 64 Б с запасом покрывает ответ целиком,
	// дальнейшее увеличение буфера профита не даёт (проверено).
	var buf [64]byte
	for len(out) < max {
		if !time.Now().Before(deadline) {
			return out, false
		}
		want := len(buf)
		if r := max - len(out); r < want {
			want = r
		}
		n, err := syscall.Read(p.fd, buf[:want])
		if n > 0 {
			// эхо — одним write на всю принятую пачку до '\n' включительно
			end, hasNL := n, false
			for i := 0; i < n; i++ {
				if buf[i] == 0x0A {
					end, hasNL = i+1, true
					break
				}
			}
			if echo {
				// Не смогли вернуть эхо — устройство осталось в рассогласовании по эху,
				// такой ответ нельзя считать успешным: возвращаем nil, чтобы вызывающий
				// сбросил линию (flush) и повторил обмен.
				if err := p.WriteAll(buf[:end]); err != nil {
					return nil, false
				}
			}
			out = append(out, buf[:end]...)
			if hasNL {
				return out, true
			}
			continue
		}
		if err != nil && err != syscall.EINTR && err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			return out, false
		}
		// n==0 — истёк VTIME (100 мс), ждать дополнительно не нужно.
		// Пауза — только для неблокирующего режима (EAGAIN).
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			time.Sleep(time.Millisecond)
		}
	}
	return out, false
}
