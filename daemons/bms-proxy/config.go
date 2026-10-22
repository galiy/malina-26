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
	probeListen = 2 * time.Second  // ждать запрос на порту (есть ли наш порт)
	banPort     = 10 * time.Second // бан «не наш» порта
	// liveWindow — страховка от зависшего bms-reader: данные старше порога не
	// отдаём. Штатно heartbeat идёт раз в секунду; запас 4 с покрывает один
	// медленный цикл опроса (arch §10).
	liveWindow   = 4 * time.Second
	idleListener = 5 * time.Second        // 5 с без запросов → слушатель завершается
	scanCycle    = 2 * time.Second        // цикл перебора портов в состоянии поиска
	readTick     = 200 * time.Millisecond // шаг чтения: отзывчивость к stop/reload
	// responseDelay — пауза перед ответом. Реальный EnBMS начинает отвечать
	// через 32–34 мс после запроса (замер 2026-10-22, 57600); инвертору нужно
	// время на переключение RS485 в приём. Отвечая за 0.7 мс, мы рискуем
	// потерять первый байт кадра и получить ретрай через 2 с.
	responseDelay = 30 * time.Millisecond
)

// Допустимые скорости порта (docs/bms-proxy.md §8). Задаются явно в настройке.
var allowedBauds = []int{2400, 9600, 19200, 57600, 115200, 230400}

// Допустимые виды адресации инвертора (docs/bms-proxy.md §5/§8):
// addrMode0 — «0…15» (слот = ADR), addrMode1 — «1…16» (слот = ADR-1).
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

	// UDPEnabled — явный переключатель UDP-слушателя; UDPListen — адрес прослушивания
	// (например "0.0.0.0:502"). Слушатель запускается только при UDPEnabled=true и
	// непустом адресе (docs/bms-proxy.md §7а).
	UDPEnabled bool   `json:"udp_enabled"`
	UDPListen  string `json:"udp_listen"`
}

// udpListenAddr — адрес для UDP-слушателя: пусто, если выключен или адрес не задан.
func (s settings) udpListenAddr() string {
	if !s.UDPEnabled {
		return ""
	}
	return strings.TrimSpace(s.UDPListen)
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
	return fmt.Sprintf("enabled=%v baud=%d log=%s addr=%s udp=%v(%s) cfg=%s sock=%s",
		c.Enabled, c.Baud, c.LogLevel, c.AddrMode, c.UDPEnabled, c.UDPListen, c.cfgPath, c.sockPath)
}
