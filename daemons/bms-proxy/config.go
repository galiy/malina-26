package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Пути и лимиты bms-proxy (docs/bms-proxy.md §8).
const (
	configPath = "/malina/etc/malina/bms-proxy.json"
	sockPath   = "/run/malina-bms-proxy/control.sock"
	shmPath    = "/dev/shm/malina-bms-state"
)

// Параметры поведения (docs/bms-proxy.md §6/§7).
const (
	probeListen   = 2 * time.Second        // ждать запрос на порту (есть ли наш порт)
	banPort       = 10 * time.Second       // бан «не наш» порта
	liveWindow    = 2 * time.Second        // порог: BMS живая, если last_seen не старше
	refreshPeriod = 1 * time.Second        // период обновления списка живых BMS
	idleListener  = 5 * time.Second        // 5 с без запросов → слушатель завершается
	scanCycle     = 2 * time.Second        // цикл перебора портов в состоянии поиска
	readTick      = 200 * time.Millisecond // шаг чтения: отзывчивость к stop/reload
)

// Допустимые скорости порта (docs/bms-proxy.md §8). Задаются явно в настройке.
var allowedBauds = []int{2400, 9600, 19200, 57600, 115200, 230400}

// Допустимые виды адресации инвертора (docs/bms-proxy.md §5/§8):
// addrMode0 — «0…15» (ADR == индекс живого списка), addrMode1 — «1…16» (ADR-1).
const (
	addrMode0 = "0-15"
	addrMode1 = "1-16"
)

// settings — настройки bms-proxy (bms-proxy.json).
type settings struct {
	Enabled  bool   `json:"enabled"`
	Baud     int    `json:"baud"`
	LogLevel string `json:"log_level"`
	AddrMode string `json:"addr_mode"`

	// UDPListen — СКРЫТАЯ опция (нет в веб-UI): адрес второго UDP-слушателя,
	// например "0.0.0.0:502". Пусто (по умолчанию) — UDP-слушатель НЕ запускается
	// (docs/bms-proxy.md §7а).
	UDPListen string `json:"udp_listen"`
}

// defaultSettings — значения по умолчанию: выключен, 19200, INFO, адресация 1..16,
// UDP-слушатель выключен.
func defaultSettings() settings {
	return settings{Enabled: false, Baud: 19200, LogLevel: "info", AddrMode: addrMode1}
}

// normalizeAddrMode приводит вид адресации к канонической форме «0-15»/«1-16»
// (принимает "1..16", "1_16" и т.п.); ("", false), если не распознано.
func normalizeAddrMode(s string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.ReplaceAll(t, "..", "-")
	t = strings.ReplaceAll(t, "_", "-")
	t = strings.ReplaceAll(t, " ", "")
	switch t {
	case addrMode0:
		return addrMode0, true
	case addrMode1:
		return addrMode1, true
	}
	return "", false
}

// loadSettings читает bms-proxy.json и возвращает значения по умолчанию,
// если файла нет или он невалиден.
func loadSettings(path string) settings {
	s := defaultSettings()
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if len(b) == 0 {
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return defaultSettings()
	}
	if !validBaud(s.Baud) {
		s.Baud = defaultSettings().Baud
	}
	// Канонизация уровня: "" → info, "INFO"/"warning" → "info"/"warn".
	if lvl, ok := parseLevel(s.LogLevel); ok {
		s.LogLevel = strings.ToLower(levelNames[lvl])
	} else {
		s.LogLevel = defaultSettings().LogLevel
	}
	// Канонизация адресации: ""/неизвестное → 1..16.
	if am, ok := normalizeAddrMode(s.AddrMode); ok {
		s.AddrMode = am
	} else {
		s.AddrMode = defaultSettings().AddrMode
	}
	return s
}

func validBaud(b int) bool {
	for _, v := range allowedBauds {
		if b == v {
			return true
		}
	}
	return false
}

// config — объединённая конфигурация демона (пути + поведение + настройки).
type config struct {
	shmPath  string
	sockPath string
	cfgPath  string

	settings
}

func newConfig() *config {
	return &config{
		shmPath:  shmPath,
		sockPath: sockPath,
		cfgPath:  configPath,
	}
}

func (c *config) String() string {
	return fmt.Sprintf("enabled=%v baud=%d log=%s addr=%s cfg=%s sock=%s",
		c.Enabled, c.Baud, c.LogLevel, c.AddrMode, c.cfgPath, c.sockPath)
}
