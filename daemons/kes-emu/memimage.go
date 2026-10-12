package main

import (
	"strings"
	"sync"
)

// memImageBytes — размер образа карты памяти: EEPROM (0x400) + RAM (kesImageSize).
const memImageBytes = 0x400 + kesImageSize

// ramOffset — смещение RAM-адреса в образе карты памяти.
func ramOffset(ramAddr uint16) uint16 { return 0x400 + ramAddr }

// devOffset — смещение адреса устройства в образе карты памяти.
// EEPROM: адрес как есть; RAM: адрес >= 0x400 → RAM-индекс (addr-0x1F0).
func devOffset(addr uint16) uint16 {
	if addr >= 0x400 {
		return ramOffset(addr - 0x1F0)
	}
	return addr
}

// MemImage — карта памяти устройства (весь образ: EEPROM + RAM).
type MemImage struct {
	mu sync.RWMutex
	b  []byte
}

// NewMemImage декодирует HEX-карту; при неверной длине — пустой образ.
func NewMemImage(hexs string) *MemImage {
	m := &MemImage{b: make([]byte, memImageBytes)}
	for off := 0; off < memImageBytes; off++ {
		i := off * 2
		if i+2 > len(hexs) {
			break
		}
		m.b[off] = byte(fromHex(hexs[i])<<4 | fromHex(hexs[i+1]))
	}
	return m
}

// Hex — карта HEX-строкой (верхний регистр).
func (m *MemImage) Hex() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return toHex(m.b)
}

// At — байт по смещению.
func (m *MemImage) At(off uint16) byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if int(off) < len(m.b) {
		return m.b[off]
	}
	return 0
}

// Set — записать байт по смещению.
func (m *MemImage) Set(off uint16, v byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if int(off) < len(m.b) {
		m.b[off] = v
	}
}

func fromHex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func toHex(b []byte) string {
	const digits = "0123456789ABCDEF"
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, v := range b {
		sb.WriteByte(digits[v>>4])
		sb.WriteByte(digits[v&0x0F])
	}
	return sb.String()
}
