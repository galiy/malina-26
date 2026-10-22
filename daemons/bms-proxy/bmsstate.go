package main

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// Чтение бинарного контракта bms-reader из POSIX shared memory (/dev/shm/…).
// Раскладка синхронизирована с daemons/bms-reader/shm.go и
// web/internal/web/bmsstate.go (docs/bms-proxy.md §2/§3):
//
//	[header 64][list: 16×200][slots: 16×(32+112)]
const (
	shmMagic    = 0x424D5342 // "BSMB"
	shmVersion  = 1
	shmHeader   = 64
	bmsSlots    = 16
	bmsListLen  = 200
	bmsSlotHdr  = 32
	bmsSlotData = 112
	bmsSlotSize = bmsSlotHdr + bmsSlotData
)

// Смещения записи списка (200 Б) — daemons/bms-reader/shm.go encodeItem.
const (
	itOffID       = 16  // string[50] — логический ID (BASE)
	itOffBase     = 66  // string[50]
	itOffSource   = 116 // string[64]
	itOffTypeID   = 180 // u8
	itOffState    = 181 // u8 (0=lost, 1=ok)
	itOffFraming  = 182 // u8
	itOffGen      = 184 // uint64 LE
	itOffLastSeen = 192 // uint64 LE (unix, сек)
)

// readShm читает сегмент bms-reader целиком. Ошибка/отсутствие → nil.
func readShm(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if len(b) < shmHeader {
		return nil
	}
	if binary.LittleEndian.Uint32(b[0:4]) != shmMagic ||
		binary.LittleEndian.Uint32(b[4:8]) != shmVersion {
		return nil
	}
	return b
}

// readSlotTelemetry читает телеметрию слота с проверкой занятости, свежести и
// generation. Слот — стабильный адрес инвертора (ADR), а не индекс компактного
// списка: выпадение одной BMS не переназначает номера другим.
func readSlotTelemetry(data []byte, slot int, now time.Time) []byte {
	if data == nil || len(data) < shmHeader+bmsSlots*bmsListLen {
		return nil
	}
	if slot < 0 || slot >= bmsSlots {
		return nil
	}
	off := shmHeader + slot*bmsListLen
	it := data[off : off+bmsListLen]
	if binary.LittleEndian.Uint32(it[0:4]) != 1 { // слот свободен
		return nil
	}
	// Упрощённый seqlock: достаточно проверить бит записи. Поля записи — 64-бит
	// выровненные, а payload защищён отдельным seqlock+CRC в readTelemetry.
	if binary.LittleEndian.Uint64(it[8:16])&1 != 0 { // идёт запись
		return nil
	}
	if it[itOffState] != 1 { // state=0 → lost
		return nil
	}
	lastSeen := int64(binary.LittleEndian.Uint64(it[itOffLastSeen : itOffLastSeen+8]))
	if now.Sub(time.Unix(lastSeen, 0)) > liveWindow {
		return nil // bms-reader не публикует heartbeat — данные недостоверны
	}
	gen := binary.LittleEndian.Uint64(it[itOffGen : itOffGen+8])
	return readTelemetry(data, slot, gen)
}

// activeTelemetry — телеметрия активного слота вместе с типом BMS из списка
// shm (typeId: 1=AntBMS, 2=EnBMS). Нужен для Vektor-маппинга alarm.
type activeTelemetry struct {
	TypeID uint8
	Tel    bmscontract.Telemetry
}

// readActiveTelemetries декодирует телеметрию всех активных слотов: state=1 и
// last_seen не старше liveWindow (4 с). Нужен Vektor-режиму для блока состояния
// и синтетических напряжений ячеек (docs/bms-proxy.md §5.1).
func readActiveTelemetries(data []byte, now time.Time) []activeTelemetry {
	if data == nil || len(data) < shmHeader+bmsSlots*bmsListLen {
		return nil
	}
	var out []activeTelemetry
	for slot := 0; slot < bmsSlots; slot++ {
		payload := readSlotTelemetry(data, slot, now)
		if payload == nil {
			continue
		}
		tel, err := bmscontract.Decode(payload)
		if err != nil {
			continue
		}
		typeID := data[shmHeader+slot*bmsListLen+itOffTypeID]
		out = append(out, activeTelemetry{TypeID: typeID, Tel: tel})
	}
	return out
}

// readTelemetry читает payload слота (контракт 107 Б) и сверяет gen.
// Возвращает nil, если слот пуст/невалиден/ген не совпал (docs/bms-proxy.md §5).
func readTelemetry(data []byte, slot int, gen uint64) []byte {
	if data == nil {
		return nil
	}
	if slot < 0 || slot >= bmsSlots {
		return nil
	}
	off := shmHeader + bmsSlots*bmsListLen + slot*bmsSlotSize
	if off+bmsSlotSize > len(data) {
		return nil
	}
	s := data[off : off+bmsSlotSize]
	if binary.LittleEndian.Uint32(s[0:4]) != 1 { // слот свободен
		return nil
	}
	if binary.LittleEndian.Uint64(s[16:24])&1 != 0 { // seqlock: идёт запись
		return nil
	}
	if binary.LittleEndian.Uint64(s[8:16]) != gen { // другая генерация
		return nil
	}
	n := int(binary.LittleEndian.Uint32(s[24:28]))
	if n != 107 { // ровно бинарный контракт bms v1
		return nil
	}
	payload := append([]byte(nil), s[bmsSlotHdr:bmsSlotHdr+n]...)
	if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(s[28:32]) {
		return nil // рваный/повреждённый снимок
	}
	return payload
}
