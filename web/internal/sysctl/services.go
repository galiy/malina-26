package sysctl

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ErrNotManaged — unit не входит в список управляемых.
var ErrNotManaged = errors.New("unit not managed")

// ErrBadAction — недопустимое действие.
var ErrBadAction = errors.New("bad action")

// managedUnits — службы/таймеры, доступные в UI (просмотр и start/stop).
// Управление ограничено только этим списком.
var managedUnits = []string{
	"malina-net.service",
	"malina-hostapd.service",
	"malina-dnsmasq.service",
	"malina-wpa.service",
	"malina-net-monitor.service",
	"malina-net-scan.service",
	"malina-net-fallback.timer",
	"malina-logrotate.timer",
	"malina-web.service",
	"dhclient-eth0.service",
	"malina-dhclient-wlan0.service",
	"systemd-timesyncd.service",
	"rsyslog.service",
	"ssh.service",
	"cron.service",
}

// Service — состояние systemd-unit-а для UI.
type Service struct {
	Unit        string `json:"unit"`
	Description string `json:"description"`
	Active      string `json:"active"`  // active/inactive/failed/activating/deactivating/unknown
	Sub         string `json:"sub"`     // running/exited/dead/failed/…
	Enabled     string `json:"enabled"` // enabled/disabled/static
	Note        string `json:"note"`    // i18n-ключ комментария ("" — без комментария)
	Group       string `json:"group"`   // "app" (управляемые) | "system" (только статус)
}

// svcNotes — комментарии к управляемым unit-ам (ключ нужен, чтобы текст
// локализовался на клиенте).
var svcNotes = map[string]string{
	// Критичные для ОС — не останавливать.
	"ssh.service":               "svc.note.critical",
	"rsyslog.service":           "svc.note.critical",
	"systemd-timesyncd.service": "svc.note.critical",
	"cron.service":              "svc.note.critical",
	// Ядро сети «Малины».
	"malina-net.service": "svc.note.core",
	// Сама веб-админка.
	"malina-web.service": "svc.note.self",
	// Таймеры — работают автоматически по расписанию.
	"malina-net-fallback.timer": "svc.note.timer",
	"malina-logrotate.timer":    "svc.note.timer",
	// Управляются скриптами malina-netd — не запускать вручную.
	"malina-hostapd.service":        "svc.note.autostart",
	"malina-dnsmasq.service":        "svc.note.autostart",
	"malina-wpa.service":            "svc.note.autostart",
	"malina-net-monitor.service":    "svc.note.autostart",
	"malina-net-scan.service":       "svc.note.autostart",
	"dhclient-eth0.service":         "svc.note.autostart",
	"malina-dhclient-wlan0.service": "svc.note.autostart",
}

// IsManaged проверяет, входит ли unit в список управляемых.
func IsManaged(unit string) bool {
	for _, u := range managedUnits {
		if u == unit {
			return true
		}
	}
	return false
}

// ListServices опрашивает systemd о состоянии управляемых unit-ов.
func ListServices() []Service {
	out := make([]Service, 0, len(managedUnits))
	for _, u := range managedUnits {
		s := showUnit(u)
		s.Group = "app"
		out = append(out, s)
	}
	return out
}

// ListAll возвращает управляемые службы ("app") и все прочие службы ("system",
// только для просмотра — без управления).
func ListAll() []Service {
	app := ListServices()
	seen := make(map[string]bool, len(app))
	for _, s := range app {
		seen[s.Unit] = true
	}
	return append(app, listSystemServices(seen)...)
}

// listSystemServices читает все .service (включая неактивные) одним вызовом.
func listSystemServices(exclude map[string]bool) []Service {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service",
		"--all", "--no-legend", "--plain", "--no-pager").Output()
	if err != nil {
		return nil
	}
	var res []Service
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		unit := f[0]
		if exclude[unit] {
			continue
		}
		res = append(res, Service{
			Unit:        unit,
			Description: strings.Join(f[4:], " "),
			Active:      f[2],
			Sub:         f[3],
			Group:       "system",
		})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Unit < res[j].Unit })
	return res
}

func showUnit(unit string) Service {
	s := Service{Unit: unit}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "show", unit,
		"-p", "Description", "-p", "ActiveState", "-p", "SubState", "-p", "UnitFileState")
	b, err := cmd.Output()
	if err != nil {
		s.Active = "unknown"
		return s
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "Description":
			s.Description = v
		case "ActiveState":
			s.Active = v
		case "SubState":
			s.Sub = v
		case "UnitFileState":
			s.Enabled = v
		}
	}
	if s.Active == "" {
		s.Active = "unknown"
	}
	s.Note = svcNotes[unit]
	return s
}

// ServiceAction выполняет start/stop для управляемого unit-а.
func ServiceAction(unit, action string) error {
	if !IsManaged(unit) {
		return ErrNotManaged
	}
	if action != "start" && action != "stop" {
		return ErrBadAction
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", action, unit).Run()
}
