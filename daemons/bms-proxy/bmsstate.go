package main

import (
	"encoding/binary"
	"os"
	"time"
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

// aliveBMS — живая BMS в нашем нумерованном списке: номер → (слот, gen).
type aliveBMS struct {
	Slot int    // индекс слота shm (по нему читается payload)
	Gen  uint64 // генерация слота (сверка с телеметрией)
}

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

// buildAliveList перебирает слоты и возвращает живые BMS (номер → слот,gen).
// Критерий «живой» (docs/bms-proxy.md §4): state==1 и last_seen не старше
// liveWindow. Порядок — по возрастанию номера слота; нумерация 0..N-1.
func buildAliveList(data []byte, now time.Time) []aliveBMS {
	if data == nil || len(data) < shmHeader+bmsSlots*bmsListLen {
		return nil
	}
	live := make([]aliveBMS, 0, bmsSlots)
	for i := 0; i < bmsSlots; i++ {
		off := shmHeader + i*bmsListLen
		it := data[off : off+bmsListLen]
		if binary.LittleEndian.Uint32(it[0:4]) != 1 { // слот свободен
			continue
		}
		if binary.LittleEndian.Uint64(it[8:16])&1 != 0 { // seqlock: идёт запись
			continue
		}
		if it[itOffState] != 1 { // state=0 → lost
			continue
		}
		lastSeen := int64(binary.LittleEndian.Uint64(it[itOffLastSeen : itOffLastSeen+8]))
		if now.Sub(time.Unix(lastSeen, 0)) > liveWindow { // не живая
			continue
		}
		live = append(live, aliveBMS{
			Slot: i,
			Gen:  binary.LittleEndian.Uint64(it[itOffGen : itOffGen+8]),
		})
	}
	return live
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
	if n <= 0 || n > bmsSlotData {
		return nil
	}
	return append([]byte(nil), s[bmsSlotHdr:bmsSlotHdr+n]...)
}
