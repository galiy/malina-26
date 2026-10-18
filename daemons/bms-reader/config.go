package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Пути и лимиты (docs/bms-reader-arch.md §13/§14).
const (
	shmPath  = "/dev/shm/malina-bms-state"
	sockPath = "/run/malina-bms/control.sock"
	gwPath   = "/malina/etc/malina/bms-reader.json"
	// gwPathLegacy — прежнее имя конфига (bms-gateways.json). Читается как
	// fallback, если нового файла нет.
	gwPathLegacy = "/malina/etc/malina/bms-gateways.json"
	rawSock      = "/run/malina-ts-raw/tsdb.sock"

	maxDevices = 16 // глобальный MaxDevices (и число слотов shm)
)

// Gateway — TCP-шлюз BMS (docs/bms-reader.md §1, arch §8).
type Gateway struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Mode string `json:"mode"` // "modbus" | "none"
}

// gatewaysFile — содержимое bms-reader.json (legacy bms-gateways.json — fallback).
//
// Enabled/LogLevel — настройки демона (вкл/выкл опрос и уровень логов). При
// отсутствии полей: Enabled=false (умолчание «выключен»), LogLevel=info.
type gatewaysFile struct {
	Enabled  bool      `json:"enabled"`
	LogLevel string    `json:"log_level"`
	Gateways []Gateway `json:"gateways"`
}

// loadGateways читает конфиг bms-reader (настройки + шлюзы). Если нового файла
// bms-reader.json нет — читает legacy bms-gateways.json (миграция без потери
// шлюзов); отсутствие обоих — пустой конфиг.
func loadGateways() (gatewaysFile, error) {
	for _, p := range []string{gwPath, gwPathLegacy} {
		g, ok, err := loadGatewaysFile(p)
		if err != nil {
			return g, err
		}
		if ok {
			return g, nil
		}
	}
	return gatewaysFile{}, nil
}

// loadGatewaysFile читает один файл: (g, существует, err).
func loadGatewaysFile(path string) (gatewaysFile, bool, error) {
	var g gatewaysFile
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return g, false, nil
		}
		return g, false, err
	}
	if len(b) == 0 {
		return g, false, nil
	}
	if err := json.Unmarshal(b, &g); err != nil {
		return g, false, fmt.Errorf("%s: %w", path, err)
	}
	return g, true, nil
}
