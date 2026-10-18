package transport

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// socketPairPort — Port на одном конце AF_UNIX socketpair: проверяем Read/WriteEcho
// без реального UART.
func socketPairPort(t *testing.T) (*Port, int) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &Port{fd: fds[0]}
	t.Cleanup(func() {
		_ = syscall.Close(fds[0])
		_ = syscall.Close(fds[1])
	})
	return p, fds[1]
}

func writeFD(t *testing.T, fd int, b []byte) {
	t.Helper()
	if _, err := syscall.Write(fd, b); err != nil {
		t.Fatal(err)
	}
}

func TestReadReturnsBatch(t *testing.T) {
	p, peer := socketPairPort(t)
	writeFD(t, peer, []byte("abcdef"))
	buf := make([]byte, 16)
	start := time.Now()
	n, err := p.Read(buf, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 || string(buf[:n]) != "abcdef" {
		t.Fatalf("Read: n=%d %q", n, buf[:n])
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("пачка прочитана за %s — похоже на побайтовое чтение", d)
	}
}

func TestReadDeadline(t *testing.T) {
	p, _ := socketPairPort(t)
	start := time.Now()
	_, err := p.Read(make([]byte, 8), time.Now().Add(40*time.Millisecond))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("ожидали ErrTimeout, получили %v", err)
	}
	if d := time.Since(start); d < 30*time.Millisecond || d > time.Second {
		t.Fatalf("дедлайн сработал не по границе: %s", d)
	}
}

func TestReadEOFDoesNotSpin(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &Port{fd: fds[0]}
	defer syscall.Close(fds[0])
	if err := syscall.Close(fds[1]); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = p.Read(make([]byte, 8), time.Now().Add(time.Second))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ожидали io.EOF, получили %v", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("EOF обработан за %s — busy-loop", d)
	}
}

func TestFdOutsideFdSet(t *testing.T) {
	p := &Port{fd: 1024}
	if err := p.waitReadable(time.Now().Add(time.Second)); err == nil {
		t.Fatal("ожидали явную ошибку для fd >= 1024")
	}
}

// openPTY открывает пару master/slave; возвращает slave-путь и master-fd.
func openPTY(t *testing.T) (string, int) {
	t.Helper()
	mfd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("нет /dev/ptmx: %v", err)
	}
	var unlock int32
	if err := ioctl(mfd, 0x40045431 /* TIOCSPTLCK */, uintptr(unsafe.Pointer(&unlock))); err != nil {
		syscall.Close(mfd)
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var n uint32
	if err := ioctl(mfd, 0x80045430 /* TIOCGPTN */, uintptr(unsafe.Pointer(&n))); err != nil {
		syscall.Close(mfd)
		t.Skipf("TIOCGPTN: %v", err)
	}
	return fmt.Sprintf("/dev/pts/%d", n), mfd
}

func TestSetBaudVMINAndPacketRead(t *testing.T) {
	slavePath, master := openPTY(t)
	defer syscall.Close(master)

	p, err := Open(slavePath)
	if err != nil {
		t.Skipf("Open(%s): %v", slavePath, err)
	}
	defer p.Close()
	if err := p.SetBaud(19200); err != nil {
		t.Fatal(err)
	}

	var tio syscall.Termios
	if err := ioctl(p.fd, syscall.TCGETS, uintptr(unsafe.Pointer(&tio))); err != nil {
		t.Fatal(err)
	}
	if tio.Cc[syscall.VMIN] != 1 || tio.Cc[syscall.VTIME] != 0 {
		t.Fatalf("VMIN=%d VTIME=%d, ожидалось 1/0",
			tio.Cc[syscall.VMIN], tio.Cc[syscall.VTIME])
	}

	if _, err := syscall.Write(master, []byte("packet")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, err := p.Read(buf, time.Now().Add(time.Second))
	if err != nil || n != 6 || string(buf[:n]) != "packet" {
		t.Fatalf("Read: n=%d %q err=%v", n, buf[:n], err)
	}
}

func TestReadEOFOnOpenPort(t *testing.T) {
	slavePath, master := openPTY(t)
	p, err := Open(slavePath)
	if err != nil {
		syscall.Close(master)
		t.Skipf("Open(%s): %v", slavePath, err)
	}
	defer p.Close()

	flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(p.fd), syscall.F_GETFL, 0)
	if e != 0 || flags&syscall.O_NONBLOCK == 0 {
		syscall.Close(master)
		t.Fatalf("Open не выставил O_NONBLOCK: flags=%#x err=%v", flags, e)
	}
	if err := syscall.Close(master); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = p.Read(make([]byte, 8), time.Now().Add(time.Second))
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) {
		t.Fatalf("ожидали io.EOF/EIO после закрытия master, получили %v", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("EOF обработан за %s", d)
	}
}
