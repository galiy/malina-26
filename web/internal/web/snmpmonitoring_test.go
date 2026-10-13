package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/galiy/malina-26/web/internal/audit"
)

func newAuthedContext(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authKey, true))
}

func TestValidateMonitoring(t *testing.T) {
	good := []monitoringConfig{
		{Enabled: true, IP: "192.168.1.10", Interval: 300},
		{Enabled: true, IP: "10.0.0.1", Interval: 30},
		{Enabled: false, IP: "", Interval: 0},
		{Enabled: true, IP: "172.16.0.1", Interval: 3600},
	}
	for _, c := range good {
		if k, ok := validateMonitoringConfig(c); !ok {
			t.Fatalf("должно быть валидно %+v (key=%s)", c, k)
		}
	}
	bad := []monitoringConfig{
		{Enabled: true, IP: "", Interval: 300},              // IP обязателен
		{Enabled: true, IP: "not-an-ip", Interval: 300},     // некорректный IP
		{Enabled: true, IP: "192.168.1.10", Interval: 5},    // < 30
		{Enabled: true, IP: "192.168.1.10", Interval: 7200}, // > 3600
		{Enabled: false, IP: "192.168.1.10", Interval: 5},   // interval вне диапазона
	}
	for _, c := range bad {
		if _, ok := validateMonitoringConfig(c); ok {
			t.Fatalf("должно быть НЕвалидно %+v", c)
		}
	}
}

func TestMonitoringSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snmp-monitoring.json")
	c := monitoringConfig{Enabled: true, IP: "192.168.1.10", Interval: 600}
	if err := saveMonitoringConfig(path, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadMonitoringConfigPath(path)
	if got != c {
		t.Fatalf("load=%+v want=%+v", got, c)
	}
	// отсутствие файла → выключено
	if got := loadMonitoringConfigPath(filepath.Join(t.TempDir(), "none.json")); got.Enabled {
		t.Fatalf("нет файла → должно быть выключено")
	}
}

func TestHandleSystemMonitoring(t *testing.T) {
	dir := t.TempDir()
	s := &Server{opts: Options{SNMPMonitoring: filepath.Join(dir, "snmp-monitoring.json")}, audit: audit.NewWithSink(false, nil)}
	form := url.Values{}
	form.Set("enabled", "1")
	form.Set("ip", "192.168.1.10")
	form.Set("interval", "300")
	r := httptest.NewRequest(http.MethodPost, "/system/monitoring", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = newAuthedContext(r)
	w := httptest.NewRecorder()
	s.handleSystemMonitoring(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("ожидали редирект, получили %d", w.Code)
	}
	got := loadMonitoringConfigPath(s.opts.SNMPMonitoring)
	if !got.Enabled || got.IP != "192.168.1.10" || got.Interval != 300 {
		t.Fatalf("после сохранения: %+v", got)
	}
}

func TestHandleSystemMonitoringInvalidIP(t *testing.T) {
	s := &Server{opts: Options{SNMPMonitoring: filepath.Join(t.TempDir(), "snmp-monitoring.json")}, audit: audit.NewWithSink(false, nil)}
	form := url.Values{}
	form.Set("enabled", "1")
	form.Set("ip", "bad")
	form.Set("interval", "300")
	r := httptest.NewRequest(http.MethodPost, "/system/monitoring", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = newAuthedContext(r)
	w := httptest.NewRecorder()
	s.handleSystemMonitoring(w, r)
	if !strings.Contains(w.Header().Get("Location"), "err=monitoring_ip") {
		t.Fatalf("ожидали err=monitoring_ip, получили %q", w.Header().Get("Location"))
	}
	got := loadMonitoringConfigPath(s.opts.SNMPMonitoring)
	if got.Enabled {
		t.Fatalf("невалидный ввод не должен сохраняться: %+v", got)
	}
}

func TestTrapDefForField(t *testing.T) {
	if d, ok := trapDefForField("unet"); !ok {
		t.Fatal("unet должен иметь нотификацию")
	} else if d.mult != 1 {
		t.Fatalf("unet mult=%v", d.mult)
	}
	if _, ok := trapDefForField("t1"); ok {
		t.Fatal("t1 (Temp_Grad1) без нотификации — trap не отправляется")
	}
	// фронт и Ok проверяются на уровне OID
	if v := trapMIBValue(12.3, 0.1); v != 123 {
		t.Fatalf("trapMIBValue=%d", v)
	}
}
