// Package logx — небольшой leveled-логгер для демонов: уровни
// debug/info/warn/error, сменяемый на лету порог, префикс процесса.
//
// Logger реализует io.Writer, поэтому его можно назначить выхлопом стандартного
// log (log.SetOutput): строки, пришедшие через standard log, трактуются как INFO.
// Это позволяет постепенно переводить существующие log.Printf на уровни.
package logx

import (
	"fmt"
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
	mu     sync.Mutex
	prefix string
	level  Level
	out    *os.File
}

// New создаёт логгер с префиксом (например, "map-reader: "), порог INFO, вывод
// в stderr.
func New(prefix string) *Logger {
	return &Logger{prefix: prefix, level: Info, out: os.Stderr}
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
	fmt.Fprintf(l.out, "%s%s %-5s %s\n",
		l.prefix, time.Now().Format("2006/01/02 15:04:05"), levelNames[lvl], msg)
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
