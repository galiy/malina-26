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

// Window возвращает до `lines` строк, заканчивающихся `offset` строк от конца
// (offset=0 -> самый свежий блок), признак наличия более старых строк и ok.
// Читает файл «с конца» ограниченными блоками, не загружая его целиком.
func Window(name string, offset, lines int) ([]string, bool, bool) {
	f, err := os.Open(Dir + "/" + name)
	if err != nil {
		return nil, false, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, false
	}

	need := offset + lines
	const chunk = 64 * 1024
	const maxBytes = 4 << 20 // предохранительный лимит чтения
	var buf []byte
	pos := st.Size()
	for pos > 0 && len(buf) < maxBytes {
		rs := int64(chunk)
		if pos < rs {
			rs = pos
		}
		pos -= rs
		b := make([]byte, rs)
		if _, err := f.ReadAt(b, pos); err != nil && err != io.EOF {
			return nil, false, false
		}
		buf = append(b, buf...)
		if bytes.Count(buf, []byte("\n")) > need {
			break
		}
	}
	reachedStart := pos == 0

	s := string(buf)
	if !reachedStart {
		// Первая строка может быть обрезана границей блока — отбрасываем её.
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	all := strings.Split(s, "\n")
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}

	end := len(all) - offset
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
	older := !reachedStart || start > 0
	return all[start:end], older, true
}
