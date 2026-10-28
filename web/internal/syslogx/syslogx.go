// Package syslogx — минимальный вывод в системный syslog (/dev/log) для
// утилит web-модуля (malina-web, malina-netd), которые пишут стандартным
// log.Printf. Строки уходят в rsyslog с тегом программы и дальше на удалённый
// log-хост по общему форварду. Если syslog недоступен (или отключён для
// тестов), вывод идёт в stderr.
package syslogx

import (
	"io"
	"log/syslog"
	"os"
	"strings"
	"sync"
	"time"
)

// reconnectInterval — минимальный интервал между попытками переподключения к
// /dev/log после обрыва.
const reconnectInterval = 10 * time.Second

// useSyslog сообщает, надо ли пытаться писать в syslog.
// Отключается для тестовых бинарников (*.test) и переменной SYSLOGX_STDERR=1.
func useSyslog() bool {
	if os.Getenv("SYSLOGX_STDERR") == "1" {
		return false
	}
	return !strings.HasSuffix(os.Args[0], ".test")
}

type rawWriter struct {
	mu       sync.Mutex
	sys      *syslog.Writer
	tag      string
	lastDial time.Time
}

// ensureSysLocked гарантирует наличие связи с /dev/log, если возможно.
// Повторные попытки — не чаще reconnectInterval.
func (r *rawWriter) ensureSysLocked() bool {
	if r.sys != nil {
		return true
	}
	if !useSyslog() {
		return false
	}
	if !r.lastDial.IsZero() && time.Since(r.lastDial) < reconnectInterval {
		return false
	}
	r.lastDial = time.Now()
	if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, r.tag); err == nil {
		r.sys = w
		return true
	}
	return false
}

func (r *rawWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ensureSysLocked() {
		if _, err := r.sys.Write(p); err == nil {
			return len(p), nil
		}
		r.sys.Close()
		r.sys = nil
	}
	return os.Stderr.Write(p)
}

// Raw возвращает io.Writer для log.SetOutput: строки пишутся как есть в syslog
// с данным тегом, при недоступности — в stderr. Подключение ленивое: при
// стартовой недоступности /dev/log связь установится на первой записи.
func Raw(tag string) io.Writer {
	if !useSyslog() {
		return os.Stderr
	}
	return &rawWriter{tag: tag}
}
