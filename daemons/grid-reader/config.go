// grid-reader — опрос сетевых инверторов Deye/Sofar (Solarman V5).
package main

import (
	"encoding/json"
	"os"
	"strings"
)

// settingsPath — конфиг демона. Создаётся деплоем/вебом; по умолчанию выключен.
const settingsPath = "/malina/etc/malina/grid-reader.json"

const maxInverters = 16

type inverter struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Type      string `json:"type"` // deye|sofar
	Placement string `json:"placement"`
	LoggerSN  uint32 `json:"logger_sn"`
	Valid     bool   `json:"-"`
}

type settings struct {
	Enabled    bool       `json:"enabled"`
	LogLevel   string     `json:"log_level"`
	PollPeriod int        `json:"poll_period"` // 10|20|30
	Placements []string   `json:"placements"`
	Inverters  []inverter `json:"inverters"`
}

func defaultSettings() settings {
	return settings{Enabled: false, LogLevel: "info", PollPeriod: 10}
}

// loadSettings читает и валидирует конфиг. Невалидные устройства отбрасываются
// с предупреждением; лишние (>16) — тоже.
func loadSettings() (settings, []string) { return loadSettingsFrom(settingsPath) }

func loadSettingsFrom(path string) (settings, []string) {
	s := defaultSettings()
	var warns []string
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s); err != nil {
		warns = append(warns, "конфиг невалиден: "+err.Error())
		return defaultSettings(), warns
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	switch s.PollPeriod {
	case 10, 20, 30:
	default:
		warns = append(warns, "poll_period: допустимо 10/20/30, принято 10")
		s.PollPeriod = 10
	}
	// Не компактим список: индекс = слот shm, чтобы веб мог сопоставить
	// настройку и запись списка. Невалидные записи остаются с Valid=false.
	if len(s.Inverters) > maxInverters {
		warns = append(warns, "инверторов больше 16 — лишние игнорируются")
		s.Inverters = s.Inverters[:maxInverters]
	}
	for i := range s.Inverters {
		d := &s.Inverters[i]
		d.IP = strings.TrimSpace(d.IP)
		d.Name = strings.TrimSpace(d.Name)
		d.Placement = strings.TrimSpace(d.Placement)
		d.Type = strings.ToLower(strings.TrimSpace(d.Type))
		if d.Port == 0 {
			d.Port = 8899
		}
		d.Valid = d.IP != "" && (d.Type == "deye" || d.Type == "sofar") && d.Port >= 1 && d.Port <= 65535 && d.LoggerSN != 0
		if !d.Valid {
			warns = append(warns, "инвертор #"+itoa(i+1)+" пропущен: проверьте ip/type/port/logger_sn")
			continue
		}
		if d.Name == "" {
			d.Name = d.IP
		}
	}
	return s, warns
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
