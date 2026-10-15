package web

import (
	"encoding/binary"
	"time"
)

// Чтение бинарного контракта bms-reader из POSIX shared memory.
// Формат синхронизирован с daemons/bms-reader/shm.go:
//
//	[header 64][list: 16×200][slots: 16×(32+107)]
const (
	bmsShmMagic    = 0x424D5342 // "BSMB"
	bmsShmVersion  = 1
	bmsShmHeader   = 64
	bmsSlots       = 16
	bmsListItemLen = 200
	bmsSlotHdr     = 32
	// bmsSlotData — ёмкость слота (кратна 8 под 64-битные atomic на ARM);
	// фактический payload — bmscontract.Size (107 Б), читается по полю len.
	bmsSlotData = 112
	bmsSlotSize = bmsSlotHdr + bmsSlotData
)

// Смещения записи списка (200 Б) — как в daemons/bms-reader/shm.go.
const (
	bmsOffID       = 16  // string[50]
	bmsOffBase     = 66  // string[50]
	bmsOffSource   = 116 // string[64]
	bmsOffTypeID   = 180 // u8
	bmsOffState    = 181 // u8
	bmsOffFraming  = 182 // u8
	bmsOffGen      = 184 // uint64 LE
	bmsOffLastSeen = 192 // uint64 LE
)

// bmsShmPath — путь к сегменту bms-reader (переменная для тестов).
var bmsShmPath = "/dev/shm/malina-bms-state"

// BmsInfo — запись списка BMS.
type BmsInfo struct {
	ID       int    `json:"id"`        // индекс слота (0..15)
	Gen      uint64 `json:"gen"`       // генерация слота
	Serial   string `json:"serial"`    // наш ID (BASE-NNNNN)
	Base     string `json:"base"`      // BASE от probe
	Source   string `json:"source"`    // дескриптор источника
	TypeID   int    `json:"type_id"`   // внутренний код типа
	TypeName string `json:"bms_type"`  // имя типа (AntBMS-DSP / EnBMS-RS485)
	Framing  int    `json:"framing"`   // 0 native, 1 m-rtu, 2 m-tcp
	LastSeen int64  `json:"last_seen"` // unix
}

// BmsList — документ списка BMS.
type BmsList struct {
	Updated int64     `json:"updated"`
	Count   int       `json:"count"`
	Bms     []BmsInfo `json:"bms"`
}

// bmsTypeName — внутренний код типа → имя.
func bmsTypeName(code int) string {
	switch code {
	case 1:
		return "AntBMS-DSP"
	case 2:
		return "EnBMS-RS485"
	default:
		return "unknown"
	}
}

func trimZ(b []byte) string {
	i := 0
	for i < len(b) && b[i] != 0 {
		i++
	}
	return string(b[:i])
}

// loadBmsList читает список BMS из shm (отсутствие сегмента → пустой список).
func loadBmsList() BmsList {
	bl := BmsList{Bms: []BmsInfo{}, Updated: time.Now().Unix()}
	data, err := readShmAt(bmsShmPath, 0, int64(bmsShmHeader+bmsSlots*bmsListItemLen))
	if err != nil || len(data) < bmsShmHeader {
		return bl
	}
	if binary.LittleEndian.Uint32(data[0:4]) != bmsShmMagic ||
		binary.LittleEndian.Uint32(data[4:8]) != bmsShmVersion {
		return bl
	}
	for i := 0; i < bmsSlots; i++ {
		off := bmsShmHeader + i*bmsListItemLen
		if off+bmsListItemLen > len(data) {
			break
		}
		if it, ok := decodeBmsItem(i, data[off:off+bmsListItemLen]); ok {
			bl.Bms = append(bl.Bms, it)
		}
	}
	bl.Count = len(bl.Bms)
	return bl
}

func decodeBmsItem(i int, b []byte) (BmsInfo, bool) {
	if binary.LittleEndian.Uint32(b[0:4]) != 1 { // валиден только state==1
		return BmsInfo{}, false
	}
	if binary.LittleEndian.Uint64(b[8:16])&1 != 0 { // seqlock: идёт запись
		return BmsInfo{}, false
	}
	return BmsInfo{
		ID:       i,
		Gen:      binary.LittleEndian.Uint64(b[bmsOffGen : bmsOffGen+8]),
		Serial:   trimZ(b[bmsOffID : bmsOffID+50]),
		Base:     trimZ(b[bmsOffBase : bmsOffBase+50]),
		Source:   trimZ(b[bmsOffSource : bmsOffSource+64]),
		TypeID:   int(b[bmsOffTypeID]),
		TypeName: bmsTypeName(int(b[bmsOffTypeID])),
		Framing:  int(b[bmsOffFraming]),
		LastSeen: int64(binary.LittleEndian.Uint64(b[bmsOffLastSeen : bmsOffLastSeen+8])),
	}, true
}

// bmsRawPayload возвращает бинарный контракт слота (107 Б) при совпадении gen.
func bmsRawPayload(id int, gen uint64) []byte {
	if id < 0 || id >= bmsSlots {
		return nil
	}
	off := int64(bmsShmHeader + bmsSlots*bmsListItemLen + id*bmsSlotSize)
	slot, err := readShmAt(bmsShmPath, off, int64(bmsSlotSize))
	if err != nil || len(slot) < bmsSlotSize {
		return nil
	}
	if binary.LittleEndian.Uint32(slot[0:4]) != 1 { // валиден только state==1
		return nil
	}
	if binary.LittleEndian.Uint64(slot[16:24])&1 != 0 { // seqlock: идёт запись
		return nil
	}
	if binary.LittleEndian.Uint64(slot[8:16]) != gen {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(slot[24:28]))
	if n <= 0 || n > bmsSlotData {
		return nil
	}
	return append([]byte(nil), slot[bmsSlotHdr:bmsSlotHdr+n]...)
}
