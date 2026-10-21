package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadReaderCfgDefaults — отсутствие файла → дефолт (enabled, info).
func TestLoadReaderCfgDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "map-reader.json")
	if c := loadReaderCfg(p, true); !c.Enabled || c.LogLevel != "info" {
		t.Fatalf("map default: %+v", c)
	}
	if c := loadReaderCfg(p, false); c.Enabled || c.LogLevel != "info" {
		t.Fatalf("kes default: %+v", c)
	}
}

// TestReaderCfgRoundTrip — сохранение/загрузка + нормализация уровня.
func TestReaderCfgRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kes-reader.json")
	if err := saveReaderCfg(p, readerCfg{Enabled: true, LogLevel: "warn"}); err != nil {
		t.Fatal(err)
	}
	if c := loadReaderCfg(p, false); !c.Enabled || c.LogLevel != "warn" {
		t.Fatalf("round-trip: %+v", c)
	}
	// невалидный уровень → info
	if err := os.WriteFile(p, []byte(`{"enabled":true,"log_level":"verbose"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := loadReaderCfg(p, false); c.LogLevel != "info" {
		t.Fatalf("невалидный уровень не сброшен: %+v", c)
	}
}

// TestBmsGatewaysPreserveFields — enabled/log_level не теряются при записи шлюзов,
// а log_level нормализуется (пусто → info).
func TestBmsGatewaysPreserveFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bms-gateways.json")
	f := bmsGatewaysFile{
		Enabled:  true,
		LogLevel: "debug",
		Gateways: []bmsGateway{{Host: "192.0.2.1", Port: 502, Mode: "modbus"}},
	}
	if err := saveBmsGateways(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := loadBmsGateways(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.LogLevel != "debug" || len(got.Gateways) != 1 {
		t.Fatalf("потеря полей: %+v", got)
	}
	// пустой log_level нормализуется в info
	if err := os.WriteFile(p, []byte(`{"enabled":false,"gateways":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadBmsGateways(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.LogLevel != "info" {
		t.Fatalf("log_level не нормализован: %q", got.LogLevel)
	}
}

// TestSaveBmsSettingsSeparate — приёмник и прокси пишутся раздельно:
// запись одного файла не меняет второй. Reload живёт в обработчиках страницы
// и здесь не проверяется.
func TestSaveBmsSettingsSeparate(t *testing.T) {
	dir := t.TempDir()
	s := &Server{opts: Options{
		BmsConfigPath:      filepath.Join(dir, "bms-reader.json"),
		BmsProxyConfigPath: filepath.Join(dir, "bms-proxy.json"),
	}}
	cfg := bmsGatewaysFile{Enabled: true, LogLevel: "debug", Gateways: []bmsGateway{{Host: "192.0.2.1", Port: 502, Mode: "modbus"}}}
	np := bmsProxyCfg{Enabled: true, Baud: 19200, LogLevel: "warn", AddrMode: "1-16", UDPEnabled: true, UDPListen: "0.0.0.0:502"}
	if err := saveBmsGateways(s.bmsCfgPath(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveBmsProxyCfg(s.bmsProxyCfgPath(), np); err != nil {
		t.Fatal(err)
	}
	proxyBefore, _ := os.ReadFile(s.bmsProxyCfgPath())

	cfg.LogLevel = "info"
	cfg.Gateways = nil
	if err := saveBmsGateways(s.bmsCfgPath(), cfg); err != nil {
		t.Fatal(err)
	}
	proxyAfter, _ := os.ReadFile(s.bmsProxyCfgPath())
	if string(proxyBefore) != string(proxyAfter) {
		t.Fatal("сохранение приёмника изменило bms-proxy.json")
	}

	readerBefore, _ := os.ReadFile(s.bmsCfgPath())
	np.Baud = 57600
	if err := saveBmsProxyCfg(s.bmsProxyCfgPath(), np); err != nil {
		t.Fatal(err)
	}
	readerAfter, _ := os.ReadFile(s.bmsCfgPath())
	if string(readerBefore) != string(readerAfter) {
		t.Fatal("сохранение прокси изменило bms-reader.json")
	}
	if p := loadBmsProxyCfg(s.bmsProxyCfgPath()); p.Baud != 57600 {
		t.Fatalf("baud прокси: %d", p.Baud)
	}
	if g, err := loadBmsGateways(s.bmsCfgPath()); err != nil || g.LogLevel != "info" || len(g.Gateways) != 0 {
		t.Fatalf("приёмник: %+v, err=%v", g, err)
	}
}
