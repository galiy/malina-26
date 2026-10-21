package web

import (
	"encoding/binary"
	"hash/crc32"
	"time"
)

// Чтение бинарного контракта bms-reader из POSIX shared memory.
// Формат синхронизирован с daemons/bms-reader/shm.go:
//
//	[header 64][list: 16×200][slots: 16×(32+112)] (payload — 107 Б)
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
	Serial   string `json:"serial"`    // логический ID (BASE)
	Base     string `json:"base"`      // нормализованный BASE (= ID)
	Source   string `json:"source"`    // дескриптор источника
	TypeID   int    `json:"type_id"`   // внутренний код типа
	TypeName string `json:"bms_type"`  // имя типа (AntBMS-DSP / EnBMS-RS485)
	Framing  int    `json:"framing"`   // 0 native, 1 m-rtu, 2 m-tcp
	State    int    `json:"state"`     // 0 lost, 1 ok
	LastSeen int64  `json:"last_seen"` // unix
}

// MenuLabel — подпись устройства для меню: логический ID (BASE). При
// совпадении BASE у нескольких BMS подписи могут совпадать — это принято.
func (b BmsInfo) MenuLabel(tr map[string]string) string {
	return b.Serial
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
		it, ok := decodeBmsItem(i, data[off:off+bmsListItemLen], int64(off))
		if !ok {
			// Гонка с писателем: перечитываем запись один раз.
			if ib, err := readShmAt(bmsShmPath, int64(off), bmsListItemLen); err == nil {
				it, ok = decodeBmsItem(i, ib, int64(off))
			}
		}
		if ok {
			bl.Bms = append(bl.Bms, it)
		}
	}
	bl.Count = len(bl.Bms)
	return bl
}

func decodeBmsItem(i int, b []byte, absOff int64) (BmsInfo, bool) {
	if binary.LittleEndian.Uint32(b[0:4]) != 1 { // валиден только state==1
		return BmsInfo{}, false
	}
	seq := binary.LittleEndian.Uint64(b[8:16])
	if seq&1 != 0 { // seqlock: идёт запись
		return BmsInfo{}, false
	}
	// Перечитываем seq уже после копирования: писатель мог начать/закончить
	// запись между чтением региона и проверкой.
	seqb, err := readShmAt(bmsShmPath, absOff+8, 8)
	if err != nil || binary.LittleEndian.Uint64(seqb) != seq {
		return BmsInfo{}, false
	}
	gen := binary.LittleEndian.Uint64(b[bmsOffGen : bmsOffGen+8])
	genb, err := readShmAt(bmsShmPath, absOff+bmsOffGen, 8)
	if err != nil || binary.LittleEndian.Uint64(genb) != gen {
		return BmsInfo{}, false // слот переиспользован (ABA)
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
		State:    int(b[bmsOffState]),
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
	seq := binary.LittleEndian.Uint64(slot[16:24])
	if seq&1 != 0 { // seqlock: идёт запись
		return nil
	}
	if binary.LittleEndian.Uint64(slot[8:16]) != gen {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(slot[24:28]))
	if n != 107 { // ровно бинарный контракт bms v1
		return nil
	}
	payload := append([]byte(nil), slot[bmsSlotHdr:bmsSlotHdr+n]...)
	// seq не должен измениться за время копирования; CRC ловит рваный payload.
	seqb, err := readShmAt(bmsShmPath, off+16, 8)
	if err != nil || binary.LittleEndian.Uint64(seqb) != seq {
		return nil
	}
	genb, err := readShmAt(bmsShmPath, off+8, 8)
	if err != nil || binary.LittleEndian.Uint64(genb) != gen {
		return nil // слот переиспользован (ABA)
	}
	if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(slot[28:32]) {
		return nil
	}
	return payload
}
