package main

import (
	"log"
	"strings"
	"sync/atomic"
)

// Уровни логирования (docs/bms-proxy.md §8). Уровень задаётся настройкой
// log_level в bms-proxy.json и меняется на лету по reload (без рестарта).
type logLevel int32

const (
	lvlDebug logLevel = iota
	lvlInfo
	lvlWarn
	lvlError
)

// levelNames — человекочитаемые имена уровней (для конфига и логов).
var levelNames = map[logLevel]string{
	lvlDebug: "DEBUG",
	lvlInfo:  "INFO",
	lvlWarn:  "WARN",
	lvlError: "ERROR",
}

// curLevel — текущий порог. По умолчанию INFO.
var curLevel atomic.Int32

func init() { curLevel.Store(int32(lvlInfo)) }

// parseLevel разбирает имя уровня; (info, false), если имя неизвестно.
func parseLevel(s string) (logLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return lvlDebug, true
	case "info", "":
		return lvlInfo, true
	case "warn", "warning":
		return lvlWarn, true
	case "error":
		return lvlError, true
	}
	return lvlInfo, false
}

// applyLogLevel задаёт текущий уровень из строки настройки.
func applyLogLevel(s string) logLevel {
	l, _ := parseLevel(s) // неизвестное → INFO
	curLevel.Store(int32(l))
	return l
}

func logEnabled(l logLevel) bool {
	return l >= logLevel(curLevel.Load())
}

func logf(l logLevel, format string, args ...any) {
	if !logEnabled(l) {
		return
	}
	log.Printf("%-5s "+format, append([]any{levelNames[l]}, args...)...)
}

func debugf(format string, args ...any) { logf(lvlDebug, format, args...) }
func infof(format string, args ...any)  { logf(lvlInfo, format, args...) }
func warnf(format string, args ...any)  { logf(lvlWarn, format, args...) }
func errorf(format string, args ...any) { logf(lvlError, format, args...) }
