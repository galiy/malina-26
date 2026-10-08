package logtail

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// Dir — каталог с лог-файлами.
const Dir = "/var/log"

// Whitelist — список логов, доступных в UI.
var Whitelist = []string{
	"syslog",
	"auth.log",
	"kern.log",
	"user.log",
	"cron.log",
	"mail.log",
	"daemon.log",
	"messages",
	"dpkg.log",
}

// Allowed проверяет, есть ли имя лога в whitelist.
func Allowed(name string) bool {
	for _, w := range Whitelist {
		if w == name {
			return true
		}
	}
	return false
}

// Window возвращает до `lines` строк, заканчивающихся на `offset` строк от конца
// (offset=0 -> самый свежий блок), плюс общее число строк.
func Window(name string, offset, lines int) ([]string, int, bool) {
	b, err := os.ReadFile(Dir + "/" + name)
	if err != nil {
		return nil, 0, false
	}
	all := strings.Split(string(b), "\n")
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	total := len(all)
	end := total - offset
	if end < 0 {
		end = 0
	}
	start := end - lines
	if start < 0 {
		start = 0
	}
	if start > end {
		start = end
	}
	return all[start:end], total, true
}

// Tail возвращает последние n строк /var/log/<name>; ok=false если недоступно.
func Tail(name string, n int) ([]string, bool) {
	f, err := os.Open(Dir + "/" + name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	lines, err := tailFile(f, n)
	if err != nil {
		return nil, false
	}
	return lines, true
}

func tailFile(f *os.File, n int) ([]string, error) {
	const chunk = 8192
	const cap = 1 << 20 // предохранительный лимит 1 МиБ
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	var buf []byte
	pos := size
	for pos > 0 && bytes.Count(buf, []byte("\n")) <= n && len(buf) < cap {
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		b := make([]byte, readSize)
		if _, err := f.ReadAt(b, pos); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(b, buf...)
	}
	lines := strings.Split(string(buf), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
