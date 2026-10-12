package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

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

// openPTY создаёт пару PTY (master/slave), настраивает slave в raw 8N1 на speed
// и возвращает путь к slave (его подключает kes-reader/MAP2Gui как COM/tty).
func openPTY(speed int) (slavePath string, master *os.File, hold *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return "", nil, nil, err
	}
	var unlock uint32
	if err = ioctlPtr(m.Fd(), tiocsptlck, unsafe.Pointer(&unlock)); err != nil {
		m.Close()
		return "", nil, nil, err
	}
	var ptn uint32
	if err = ioctlPtr(m.Fd(), tiocgptn, unsafe.Pointer(&ptn)); err != nil {
		m.Close()
		return "", nil, nil, err
	}
	slavePath = fmt.Sprintf("/dev/pts/%d", ptn)
	// Доступ для непривилегированных клиентов (Wine под пользователем и т.п.):
	// PTY, открытый root'ом, иначе принадлежит root:tty и недоступен пользователю.
	_ = os.Chmod(slavePath, 0o666)
	// raw 8N1 (через transport) — иначе master в EIO без slave.
	if sp, oerr := transport.Open(slavePath); oerr == nil {
		_ = sp.SetBaud(speed)
		sp.Close()
	}
	// Держим slave O_RDONLY: не даёт master уйти в EIO, но не «занимает» порт
	// (читатели/сканеры считают занятыми только RDWR/WRONLY).
	if h, oerr := os.OpenFile(slavePath, os.O_RDONLY|syscall.O_NOCTTY, 0); oerr == nil {
		hold = h
	}
	return slavePath, m, hold, nil
}

// ensureLink создаёт симлинк linkDir/name → slavePath. Если симлинк уже есть:
// «битый» (цель недоступна — остаток после падения) — удаляем и создаём заново;
// «живой» (цель существует — вероятно, эмулятор уже запущен) — возвращаем ошибку,
// чтобы не плодить дубли.
func ensureLink(linkDir, name, slavePath string) (string, error) {
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		return "", err
	}
	dst := linkDir + "/" + name
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if _, serr := os.Stat(dst); serr == nil {
			return "", fmt.Errorf("%s уже существует (эмулятор уже запущен?)", dst)
		}
		_ = os.Remove(dst) // битый симлинк — остаток после падения
	}
	if err := os.Symlink(slavePath, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// sweepStale удаляет «битые» симлинки dir/prefix* (остатки после падения).
func sweepStale(dir, prefix string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		p := dir + "/" + name
		if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(p); err != nil { // цель недоступна → битый
			_ = os.Remove(p)
		}
	}
}
