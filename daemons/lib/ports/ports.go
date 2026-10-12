// Package ports — общие (device-agnostic) примитивы работы с портами:
// сбор кандидатов, ключ rdev, поиск портов, занятых другими процессами.
//
// Оркестрацию (перебор, проба, backoff) ведёт сам демон.
package ports

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// RdevOf возвращает rdev символьного устройства (0 — не удалось).
func RdevOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Rdev)
	}
	return 0
}

// ListPorts собирает кандидатов по glob-шаблонам, дедуп по rdev
// (by-id ↔ ttyUSB/ttyACM/…). Возвращает только символьные устройства.
func ListPorts(patterns []string) []string {
	seen := make(map[uint64]bool)
	var out []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			st, err := os.Stat(m)
			if err != nil || st.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			rd := RdevOf(st)
			if rd == 0 || seen[rd] {
				continue
			}
			seen[rd] = true
			out = append(out, m)
		}
	}
	return out
}

// fdAccessMode читает режим доступа fd из /proc/<pid>/fdinfo/<n> (flags & O_ACCMODE).
func fdAccessMode(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(ln, "flags:") {
			continue
		}
		f := strings.TrimSpace(strings.TrimPrefix(ln, "flags:"))
		v, err := strconv.ParseInt(f, 8, 64) // флаги в fdinfo — восьмеричные
		if err != nil {
			return 0, false
		}
		return int(v) & 3, true // O_ACCMODE: 0=RDONLY,1=WRONLY,2=RDWR
	}
	return 0, false
}

// BusyRdevs — множество rdev символьных устройств, открытых ДРУГИМИ процессами
// (обход /proc выполняется один раз на цикл, а не на каждый порт).
func BusyRdevs(self int) map[uint64]bool {
	set := make(map[uint64]bool)
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return set
	}
	for _, pr := range procs {
		if !pr.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(pr.Name())
		if err != nil || pid == self {
			continue
		}
		fdDir := "/proc/" + pr.Name() + "/fd"
		infoDir := "/proc/" + pr.Name() + "/fdinfo"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			// Держатели, открытые только на чтение (напр., эмулятор держит slave
			// O_RDONLY), порт НЕ занимают.
			if acc, ok := fdAccessMode(infoDir + "/" + fd.Name()); ok && acc == 0 {
				continue
			}
			l, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			fst, err := os.Stat(l)
			if err != nil || fst.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			if s, ok := fst.Sys().(*syscall.Stat_t); ok {
				set[uint64(s.Rdev)] = true
			}
		}
	}
	return set
}
