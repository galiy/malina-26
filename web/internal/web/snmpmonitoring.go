package web

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// monitoringConfig — настройка периодической отправки SNMP-трапов («Мониторинг»,
// docs/snmp.md §2.9). Порт/community фиксированные (162/public), в конфиг не входят.
type monitoringConfig struct {
	Enabled  bool   `json:"enabled"`
	IP       string `json:"ip"`
	Interval int    `json:"interval"` // секунды, 30..3600
}

// DefaultMonitoringPath — версионируемый файл настройки на /malina.
const DefaultMonitoringPath = "/malina/etc/malina/snmp-monitoring.json"

const (
	minMonitoringInterval = 30
	maxMonitoringInterval = 3600
)

// loadMonitoringConfig читает настройку (отсутствие файла — выключено).
func loadMonitoringConfig() monitoringConfig {
	return loadMonitoringConfigPath(DefaultMonitoringPath)
}

func loadMonitoringConfigPath(path string) monitoringConfig {
	b, err := os.ReadFile(path)
	if err != nil {
		return monitoringConfig{}
	}
	var c monitoringConfig
	if json.Unmarshal(b, &c) != nil {
		return monitoringConfig{}
	}
	// Нормировка интервала при чтении (защита от ручной правки/старого формата):
	// выключаем, если вне диапазона 30..3600, иначе крутим цикл не чаще раза в минуту.
	if c.Interval != 0 {
		if c.Interval < minMonitoringInterval || c.Interval > maxMonitoringInterval {
			c.Interval = 0
		}
	}
	return c
}

// saveMonitoringConfig атомарно пишет настройку (0600).
func saveMonitoringConfig(path string, c monitoringConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// validateMonitoringConfig проверяет настройку (§2.9/§2.10). Возвращает ключ
// ошибки (для редиректа sys.err.<key>).
func validateMonitoringConfig(c monitoringConfig) (string, bool) {
	if c.Enabled && c.IP == "" {
		return "monitoring_ip", false
	}
	if c.IP != "" && net.ParseIP(c.IP) == nil {
		return "monitoring_ip", false
	}
	if c.Enabled && (c.Interval < minMonitoringInterval || c.Interval > maxMonitoringInterval) {
		return "monitoring_interval", false
	}
	if c.Interval != 0 && (c.Interval < minMonitoringInterval || c.Interval > maxMonitoringInterval) {
		return "monitoring_interval", false
	}
	return "", true
}

// runMonitoring — цикл периодической отправки трапов. Конфиг читается каждый цикл
// (применение без пересборки, реконфиг на лету). Выключено — фон практически не
// тикает (проверка раз в минуту), опрос shm не идёт (§1.6).
func (s *Server) runMonitoring(ctx context.Context) {
	for {
		cfg := loadMonitoringConfig()
		// Валидная отправка требует: включено, IP задан и корректный интервал.
		// При любом нарушении (в т.ч. Interval==0 от ручной правки) не шлём и
		// не крутим busy-loop — проверяем раз в минуту.
		if !cfg.Enabled || cfg.IP == "" || cfg.Interval == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
				continue
			}
		}
		interval := time.Duration(cfg.Interval) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			n, err := sendPeriodicTraps(cfg.IP)
			if err != nil {
				s.audit.Event("snmp_monitoring", map[string]any{"host": cfg.IP, "sent": n, "result": "err", "error": err.Error()})
			} else if n > 0 {
				s.audit.Event("snmp_monitoring", map[string]any{"host": cfg.IP, "sent": n, "result": "ok"})
			}
		}
	}
}

// handleSystemMonitoring — сохранение настройки «Мониторинг» (POST /system/monitoring).
func (s *Server) handleSystemMonitoring(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	c := monitoringConfig{
		Enabled:  r.FormValue("enabled") == "1",
		IP:       strings.TrimSpace(r.FormValue("ip")),
		Interval: 0,
	}
	if v := strings.TrimSpace(r.FormValue("interval")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Interval = n
		}
	}
	if key, ok := validateMonitoringConfig(c); !ok {
		s.audit.Event("system_settings", map[string]any{"what": "monitoring", "result": "err", "reason": key, "ip": clientIP(r)})
		s.redirectSystem(w, r, "err="+key)
		return
	}
	if err := saveMonitoringConfig(s.opts.SNMPMonitoring, c); err != nil {
		s.audit.Event("system_settings", map[string]any{"what": "monitoring", "result": "err", "reason": "generic", "ip": clientIP(r)})
		s.redirectSystem(w, r, "err=generic")
		return
	}
	s.audit.Event("system_settings", map[string]any{"what": "monitoring", "enabled": c.Enabled, "ip": c.IP, "interval": c.Interval, "result": "ok", "client": clientIP(r)})
	s.redirectSystem(w, r, "ok=monitoring")
}
