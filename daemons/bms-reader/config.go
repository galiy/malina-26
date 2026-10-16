package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Пути и лимиты (docs/bms-reader-arch.md §13/§14).
const (
	shmPath   = "/dev/shm/malina-bms-state"
	sockPath  = "/run/malina-bms/control.sock"
	gwPath    = "/malina/etc/malina/bms-gateways.json"
	identPath = "/malina/etc/malina/bms-ident.json"
	rawSock   = "/run/malina-ts-raw/tsdb.sock"

	maxDevices = 16 // глобальный MaxDevices (и число слотов shm)
)

// Gateway — TCP-шлюз BMS (docs/bms-reader.md §1, arch §8).
type Gateway struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Mode string `json:"mode"` // "modbus" | "none"
}

// gatewaysFile — содержимое bms-gateways.json.
//
// Enabled/LogLevel — настройки демона (вкл/выкл опрос и уровень логов). При
// отсутствии полей: Enabled=false (умолчание «выключен»), LogLevel=info.
type gatewaysFile struct {
	Enabled  bool      `json:"enabled"`
	LogLevel string    `json:"log_level"`
	Gateways []Gateway `json:"gateways"`
}

// identEntry — запись реестра идентификаторов (arch §9).
type identEntry struct {
	Base    string `json:"base"`              // BASE от probe
	ID      string `json:"id"`                // BASE-NNNNN
	Binding string `json:"binding,omitempty"` // by-path / host:port (привязка)
}

// identFile — содержимое bms-ident.json.
type identFile struct {
	Counter uint64       `json:"counter"`
	Entries []identEntry `json:"entries"`
}

// loadGateways читает список шлюзов (пустой файл/отсутствие — пустой список).
func loadGateways() (gatewaysFile, error) {
	var g gatewaysFile
	b, err := os.ReadFile(gwPath)
	if err != nil {
		if os.IsNotExist(err) {
			return g, nil
		}
		return g, err
	}
	if len(b) == 0 {
		return g, nil
	}
	if err := json.Unmarshal(b, &g); err != nil {
		return g, fmt.Errorf("bms-gateways.json: %w", err)
	}
	return g, nil
}

// loadIdent читает реестр идентификаторов (отсутствие — пустой).
func loadIdent() (identFile, error) {
	var f identFile
	b, err := os.ReadFile(identPath)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if len(b) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("bms-ident.json: %w", err)
	}
	return f, nil
}

// saveIdent атомарно пишет реестр (temp + rename + fsync файла и каталога).
func saveIdent(f identFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(identPath, b)
}

// atomicWrite — temp+rename+fsync (arch §9/§13).
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
