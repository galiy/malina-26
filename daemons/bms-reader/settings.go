package main

import (
	"sync"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

// settings — настройки bms-reader (из bms-reader.json; legacy bms-gateways.json — fallback).
type settings struct {
	Enabled  bool
	LogLevel string
}

// stateHolder — потокобезопасный держатель настроек и шлюзов.
type stateHolder struct {
	mu          sync.Mutex
	s           settings
	gws         gatewaysFile
	forcedDebug bool // -debug: уровень debug поверх конфига (в т.ч. после reload)
}

// setForcedDebug включает форс уровня debug.
func (h *stateHolder) setForcedDebug() { h.mu.Lock(); h.forcedDebug = true; h.mu.Unlock() }

// applyLevel выставляет уровень логгера с учётом форса debug.
func (h *stateHolder) applyLevel(lx *logx.Logger) {
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

// loadState читает bms-reader.json (legacy bms-gateways.json — fallback). Умолчание без файла:
// enabled=false («выключен»), log_level=info.
func loadState() (stateHolder, error) {
	g, err := loadGateways()
	if err != nil {
		return stateHolder{}, err
	}
	lvl := g.LogLevel
	if _, ok := logx.ParseLevel(lvl); !ok {
		lvl = "info"
	}
	return stateHolder{s: settings{Enabled: g.Enabled, LogLevel: lvl}, gws: g}, nil
}

func (h *stateHolder) get() (settings, gatewaysFile) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.s, h.gws
}

func (h *stateHolder) set(s settings, g gatewaysFile) {
	h.mu.Lock()
	h.s, h.gws = s, g
	h.mu.Unlock()
}

// reloadSettings перечитывает конфиг, применяет уровень логирования и (buf 1)
// сигналит супервизору.
func (h *stateHolder) reloadSettings(lx *logx.Logger, reload chan<- struct{}) {
	g, err := loadGateways()
	if err != nil {
		lx.Errorf("reload: %v", err)
		return
	}
	lvl := g.LogLevel
	if _, ok := logx.ParseLevel(lvl); !ok {
		lvl = "info"
	}
	h.set(settings{Enabled: g.Enabled, LogLevel: lvl}, g)
	h.applyLevel(lx)
	select {
	case reload <- struct{}{}:
	default:
	}
}
