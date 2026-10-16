package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBmsProxyCfgRoundTrip — сохранение и загрузка bms-proxy.json.
func TestBmsProxyCfgRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bms-proxy.json")
	in := bmsProxyCfg{Enabled: true, Baud: 115200, LogLevel: "debug", AddrMode: "0-15"}
	if err := saveBmsProxyCfg(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadBmsProxyCfg(path)
	if got != in {
		t.Fatalf("round-trip: got %+v, want %+v", got, in)
	}
}

// TestBmsProxyCfgDefaults — отсутствие файла и невалидные значения → дефолты.
func TestBmsProxyCfgDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	if got := loadBmsProxyCfg(path); got != defaultBmsProxyCfg() {
		t.Fatalf("нет файла: got %+v", got)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"enabled":true,"baud":123,"log_level":"verbose"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadBmsProxyCfg(bad)
	if got.Baud != defaultBmsProxyCfg().Baud || got.LogLevel != defaultBmsProxyCfg().LogLevel {
		t.Fatalf("невалидные поля не сброшены: %+v", got)
	}
	if !got.Enabled {
		t.Fatalf("enabled потерян: %+v", got)
	}
}

// TestBmsProxyValidators — допустимые наборы скорости и уровня.
func TestBmsProxyValidators(t *testing.T) {
	for _, b := range bmsProxyBauds {
		if !bmsProxyBaudOK(b) {
			t.Errorf("baud %d должен быть валиден", b)
		}
	}
	for _, b := range []int{0, 1200, 250000} {
		if bmsProxyBaudOK(b) {
			t.Errorf("baud %d не должен быть валиден", b)
		}
	}
	for _, l := range bmsProxyLevels {
		if got, ok := normalizeBmsProxyLevel(l); !ok || got != l {
			t.Errorf("level %q должен быть валиден и каноничен", l)
		}
	}
	// нормализация: регистр и "warning"→"warn".
	if got, ok := normalizeBmsProxyLevel("INFO"); !ok || got != "info" {
		t.Errorf("INFO → %q (ok=%v), ожидалось info", got, ok)
	}
	if got, ok := normalizeBmsProxyLevel("warning"); !ok || got != "warn" {
		t.Errorf("warning → %q (ok=%v), ожидалось warn", got, ok)
	}
	for _, l := range []string{"", "verbose", "critical"} {
		if _, ok := normalizeBmsProxyLevel(l); ok {
			t.Errorf("level %q не должен быть валиден", l)
		}
	}

	// адресация инвертора
	for _, am := range bmsProxyAddrModes {
		if got, ok := normalizeBmsProxyAddrMode(am); !ok || got != am {
			t.Errorf("addr %q должен быть валиден", am)
		}
	}
	if got, ok := normalizeBmsProxyAddrMode("0..15"); !ok || got != "0-15" {
		t.Errorf("0..15 → %q (ok=%v), ожидалось 0-15", got, ok)
	}
	if got, ok := normalizeBmsProxyAddrMode("1_16"); !ok || got != "1-16" {
		t.Errorf("1_16 → %q (ok=%v), ожидалось 1-16", got, ok)
	}
	for _, am := range []string{"", "0-16", "2-16"} {
		if _, ok := normalizeBmsProxyAddrMode(am); ok {
			t.Errorf("addr %q не должен быть валиден", am)
		}
	}
}
