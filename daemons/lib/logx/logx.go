// Package logx — небольшой leveled-логгер для демонов: уровни
// debug/info/warn/error, сменяемый на лету порог, префикс процесса.
//
// Logger реализует io.Writer, поэтому его можно назначить выхлопом стандартного
// log (log.SetOutput): строки, пришедшие через standard log, трактуются как INFO.
// Это позволяет постепенно переводить существующие log.Printf на уровни.
//
// Вывод идёт в системный syslog (/dev/log) с тегом процесса — так строки
// попадают в rsyslog и уходят на удалённый log-хост по общему форварду. Если
// syslog недоступен (или явно отключён для тестов), пишем в stderr. Раньше
// вывод шёл только в os.Stderr (journald per-service); при сбое этого сокета
// строки демона терялись и не доходили до syslog/лог-хоста.
package logx

import (
	"fmt"
	"io"
	"log/syslog"
	"os"
	"strings"
	"sync"
	"time"
)

// Level — уровень логирования.
type Level int32

const (
	Debug Level = iota
	Info
	Warn
	Error
)

var levelNames = map[Level]string{
	Debug: "DEBUG",
	Info:  "INFO",
	Warn:  "WARN",
	Error: "ERROR",
}

// ParseLevel разбирает имя уровня (без учёта регистра, "warning"→warn);
// (Info,false), если имя неизвестно.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return Debug, true
	case "info", "":
		return Info, true
	case "warn", "warning":
		return Warn, true
	case "error":
		return Error, true
	}
	return Info, false
}

// Logger — leveled-логгер.
type Logger struct {
	mu       sync.Mutex
	prefix   string
	level    Level
	out      *os.File       // fallback, если syslog недоступен
	sys      *syslog.Writer // nil — нет связи с /dev/log
	tag      string         // тег syslog (PROGRAMNAME)
	lastDial time.Time      // последняя попытка подключения к /dev/log
}

// reconnectInterval — минимальный интервал между попытками переподключения к
// /dev/log после обрыва. Разумно-редко, чтобы не долбить сокет.
const reconnectInterval = 10 * time.Second

// New создаёт логгер с префиксом (например, "map-reader: "), порог INFO.
// Вывод — в syslog (/dev/log) с тегом процесса; при недоступности syslog
// (или в тестах / при LOGX_STDERR=1) — в stderr.
func New(prefix string) *Logger {
	l := &Logger{prefix: prefix, level: Info, out: os.Stderr, tag: syslogTag(prefix)}
	l.ensureSysLocked()
	return l
}

// useSyslog сообщает, надо ли пытаться писать в syslog.
// Отключается для тестовых бинарников (*.test) и переменной LOGX_STDERR=1.
func useSyslog() bool {
	if os.Getenv("LOGX_STDERR") == "1" {
		return false
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		return false
	}
	return true
}

// syslogTag выводит тег syslog (PROGRAMNAME) из префикса логгера:
// "map-reader: " → "map-reader".
func syslogTag(prefix string) string {
	t := strings.TrimSpace(prefix)
	t = strings.TrimSuffix(t, ":")
	t = strings.TrimSpace(t)
	if t == "" {
		return "malina"
	}
	return t
}

// ensureSysLocked гарантирует наличие связи с /dev/log, если возможно.
// Вызывается под l.mu (кроме New — там объект ещё не опубликован).
// Повторные попытки — не чаще reconnectInterval.
func (l *Logger) ensureSysLocked() bool {
	if l.sys != nil {
		return true
	}
	if !useSyslog() {
		return false
	}
	if !l.lastDial.IsZero() && time.Since(l.lastDial) < reconnectInterval {
		return false
	}
	l.lastDial = time.Now()
	if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, l.tag); err == nil {
		l.sys = w
		return true
	}
	return false
}

// SetLevel задаёт порог (сообщения ниже порога отбрасываются).
func (l *Logger) SetLevel(lvl Level) {
	l.mu.Lock()
	l.level = lvl
	l.mu.Unlock()
}

// SetLevelString задаёт порог из строки (неизвестное → INFO).
func (l *Logger) SetLevelString(s string) {
	lvl, _ := ParseLevel(s)
	l.SetLevel(lvl)
}

// LevelName — текущее имя порога.
func (l *Logger) LevelName() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return levelNames[l.level]
}

func (l *Logger) logf(lvl Level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lvl < l.level {
		return
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	line := fmt.Sprintf("%s%s %-5s %s",
		l.prefix, time.Now().Format("2006/01/02 15:04:05"), levelNames[lvl], msg)
	if l.ensureSysLocked() {
		if err := l.syslogWrite(lvl, line); err == nil {
			return
		}
		// Связь с /dev/log потеряна — закрываем writer и пишем в stderr;
		// переподключимся на следующей записи (не чаще reconnectInterval).
		l.sys.Close()
		l.sys = nil
	}
	fmt.Fprintln(l.out, line)
}

// syslogWrite отправляет строку с соответствующим уровнем.
func (l *Logger) syslogWrite(lvl Level, line string) error {
	switch lvl {
	case Debug:
		return l.sys.Debug(line)
	case Info:
		return l.sys.Info(line)
	case Warn:
		return l.sys.Warning(line)
	default:
		return l.sys.Err(line)
	}
}

func (l *Logger) Debugf(format string, args ...any) { l.logf(Debug, format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.logf(Info, format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.logf(Warn, format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.logf(Error, format, args...) }

// Write реализует io.Writer для стандартного log: каждая строка — уровень INFO.
func (l *Logger) Write(p []byte) (int, error) {
	l.logf(Info, "%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// rawWriter пишет строки как есть в syslog (LOG_INFO); при потере связи —
// в stderr. Переподключается к /dev/log лениво (не чаще reconnectInterval).
type rawWriter struct {
	mu       sync.Mutex
	sys      *syslog.Writer
	tag      string
	lastDial time.Time
}

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

// Raw возвращает io.Writer для log.SetOutput у демонов, которые сами
// форматируют префикс/уровень/время через стандартный log. Строки пишутся в
// syslog (/dev/log) с данным тегом; fallback — stderr. Подключение ленивое:
// при стартовой недоступности /dev/log связь установится на первой записи.
// Так вывод перестаёт зависеть от per-service journald-сокета (он закрывается
// по простою и теряет строки редко пишущих демонов).
func Raw(tag string) io.Writer {
	if !useSyslog() {
		return os.Stderr
	}
	return &rawWriter{tag: syslogTag(tag)}
}
