package main

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

// settingsPath — конфиг map-reader (создаётся деплоем; по умолчанию МАП включён).
const settingsPath = "/malina/etc/malina/map-reader.json"

// settings — настройки демона: включён/уровень логирования.
type settings struct {
	Enabled  bool   `json:"enabled"`
	LogLevel string `json:"log_level"`
}

// defaultSettings — умолчание по ТЗ: МАП ВКЛЮЧЁН, уровень info.
func defaultSettings() settings {
	return settings{Enabled: true, LogLevel: "info"}
}

// loadSettings читает map-reader.json; при отсутствии/невалидности — умолчания.
func loadSettings() settings {
	s := defaultSettings()
	b, err := os.ReadFile(settingsPath)
	if err != nil || len(b) == 0 {
		return s
	}
	if json.Unmarshal(b, &s) != nil {
		return defaultSettings()
	}
	if _, ok := logx.ParseLevel(s.LogLevel); !ok {
		s.LogLevel = defaultSettings().LogLevel
	}
	return s
}

// settingsHolder — потокобезопасный держатель текущих настроек.
type settingsHolder struct {
	mu          sync.Mutex
	s           settings
	forcedDebug bool // -debug: уровень debug поверх конфига (в т.ч. после reload)
}

func newSettingsHolder(s settings) *settingsHolder { return &settingsHolder{s: s} }

// setForcedDebug включает форс уровня debug.
func (h *settingsHolder) setForcedDebug() { h.mu.Lock(); h.forcedDebug = true; h.mu.Unlock() }

// applyLevel выставляет уровень логгера с учётом форса debug.
func (h *settingsHolder) applyLevel(lx *logx.Logger) {
	h.mu.Lock()
	fd := h.forcedDebug
	lvl := h.s.LogLevel
	h.mu.Unlock()
	if fd {
		lx.SetLevel(logx.Debug)
	} else {
		lx.SetLevelString(lvl)
	}
}

func (h *settingsHolder) get() settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.s
}

func (h *settingsHolder) set(s settings) {
	h.mu.Lock()
	h.s = s
	h.mu.Unlock()
}

// reloadSettings перечитывает конфиг, применяет уровень логирования и (buf 1)
// сигналит супервизору о возможной смене enabled.
func (h *settingsHolder) reloadSettings(lx *logx.Logger, reload chan<- struct{}) settings {
	s := loadSettings()
	h.set(s)
	h.applyLevel(lx)
	select {
	case reload <- struct{}{}:
	default:
	}
	return s
}
