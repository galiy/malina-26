package netcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Dir — каталог сетевых конфигов на rw-разделе.
const Dir = "/malina/etc/malina/network"

// AP — параметры точки доступа.
type AP struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
	Country    string `json:"country"`
	Channel    int    `json:"channel"`
}

// DHCP — параметры раздачи адресов (LAN/AP).
type DHCP struct {
	ServerIP string `json:"server_ip"` // IP-адрес устройства в этой сети
	Mask     string `json:"mask"`      // маска, напр. 255.255.255.0
	Start    string `json:"dhcp_start"`
	End      string `json:"dhcp_end"`
}

// Eth — параметры Ethernet.
type Eth struct {
	Mode    string   `json:"mode"` // dhcp | static
	IP      string   `json:"ip"`
	Mask    string   `json:"mask"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
}

// STA — параметры Wi-Fi клиента.
type STA struct {
	SSID       string   `json:"ssid"`
	Passphrase string   `json:"passphrase"`
	Security   string   `json:"security"`
	IPMode     string   `json:"ip_mode"` // dhcp | static
	IP         string   `json:"ip"`
	Mask       string   `json:"mask"`
	Gateway    string   `json:"gateway"`
	DNS        []string `json:"dns"`
}

// Mode1 — «Точка доступа».
type Mode1 struct {
	AP     AP   `json:"ap"`
	LAN    DHCP `json:"lan"`
	Eth    Eth  `json:"eth"`
	NAT    bool `json:"nat"`
	Portal bool `json:"portal"`
}

// Mode2 — «Wi-Fi клиент».
type Mode2 struct {
	STA STA  `json:"sta"`
	LAN DHCP `json:"eth"`
	// Serve — разрешить наш DHCP на eth0 (по умолчанию false: только клиент).
	Serve bool `json:"serve"`
}

// Active возвращает активный режим (mode1|mode2).
func Active() string {
	b, err := os.ReadFile(filepath.Join(Dir, "active"))
	if err != nil {
		return "mode1"
	}
	s := strings.TrimSpace(string(b))
	if s != "mode1" && s != "mode2" {
		return "mode1"
	}
	return s
}

// SetActive сохраняет активный режим.
func SetActive(mode string) error {
	if mode != "mode1" && mode != "mode2" {
		mode = "mode1"
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir, "active"), []byte(mode+"\n"), 0o644)
}

func load(name string, v any) error {
	b, err := os.ReadFile(filepath.Join(Dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func save(name string, v any) error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir, name), b, 0o600)
}

// LoadMode1 читает конфиг режима 1 (или значения по умолчанию).
func LoadMode1() Mode1 {
	m := DefaultMode1()
	_ = load("mode1.json", &m)
	return m
}

// LoadMode2 читает конфиг режима 2 (или значения по умолчанию).
func LoadMode2() Mode2 {
	m := DefaultMode2()
	_ = load("mode2.json", &m)
	return m
}

// SaveMode1 сохраняет конфиг режима 1.
func SaveMode1(m Mode1) error { return save("mode1.json", m) }

// SaveMode2 сохраняет конфиг режима 2.
func SaveMode2(m Mode2) error { return save("mode2.json", m) }

// DefaultMode1 — значения по умолчанию режима 1.
func DefaultMode1() Mode1 {
	return Mode1{
		AP:     AP{SSID: "Malina", Passphrase: "", Country: "US", Channel: 0},
		LAN:    DHCP{ServerIP: "10.10.100.254", Mask: "255.255.255.0", Start: "10.10.100.1", End: "10.10.100.200"},
		Eth:    Eth{Mode: "dhcp"},
		NAT:    true,
		Portal: true,
	}
}

// DefaultMode2 — значения по умолчанию режима 2.
func DefaultMode2() Mode2 {
	return Mode2{
		STA:   STA{Security: "wpa2", IPMode: "dhcp"},
		LAN:   DHCP{ServerIP: "10.10.100.254", Mask: "255.255.255.0", Start: "10.10.100.1", End: "10.10.100.200"},
		Serve: true,
	}
}

// Fallback — настраиваемые пороги фолбэка (секунды).
type Fallback struct {
	NoLinkSec int `json:"nolink_sec"` // нет линков STA+eth дольше этого -> режим 1
	ScanSec   int `json:"scan_sec"`   // период скана целевой сети в резервном AP
}

// DefaultFallback — 10 минут по умолчанию.
func DefaultFallback() Fallback { return Fallback{NoLinkSec: 600, ScanSec: 600} }

// LoadFallback читает пороги (или значения по умолчанию).
func LoadFallback() Fallback {
	f := DefaultFallback()
	_ = load("fallback.json", &f)
	if f.NoLinkSec <= 0 {
		f.NoLinkSec = 600
	}
	if f.ScanSec <= 0 {
		f.ScanSec = 600
	}
	return f
}
