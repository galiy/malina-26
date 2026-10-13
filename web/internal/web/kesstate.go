package web

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"
)

// Чтение бинарного контракта kes-reader из POSIX shared memory.
// Формат — docs/kes-contract.md (синхронизирован с daemons/kes-reader/shm.go,
// kes.go: контракт kes-list и слота KESE/KESR/KESS).
const (
	kesShmMagic    = 0x3153454B // "KES1"
	kesShmVersion  = 1
	kesShmHeader   = 64
	kesSlots       = 32
	kesListItemLen = 72
	kesSlotHdr     = 32
	kesSlotData    = 16384
	kesSlotSize    = kesSlotHdr + kesSlotData

	kesContractPortSize = 32
)

// kesShmPath — путь к сегменту kes-list/КЭС shm. Переменная (не константа), чтобы
// тесты могли поднять временный сегмент в t.TempDir().
var kesShmPath = "/dev/shm/malina-kes-state"

// Смещения записи kes-list (72 Б) — совпадают с daemons/kes-reader/shm.go.
const (
	kesListGenOff      = 16 // uint64 LE
	kesListBaudOff     = 24 // uint32 LE
	kesListSerialOff   = 28 // uint16 LE
	kesListPortOff     = 32 // string[32] (NUL)
	kesListLastSeenOff = 64 // int64 LE
)

// KesInfo — запись списка КЭС (компактный блок списка, itemSize=72).
type KesInfo struct {
	ID       int    `json:"id"`
	Gen      uint64 `json:"gen"`
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	Serial   uint16 `json:"serial"`
	LastSeen int64  `json:"last_seen"`
}

// SerialLabel — серийный номер КЭС (информационное поле).
func (k KesInfo) SerialLabel() string {
	return strconv.FormatUint(uint64(k.Serial), 10)
}

// KesList — документ списка КЭС.
type KesList struct {
	Updated int64     `json:"updated"`
	Count   int       `json:"count"`
	Kes     []KesInfo `json:"kes"`
}

// readShmAt читает size байт с offset из shm-файла, не читая весь сегмент.
func readShmAt(path string, off, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, size)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

func loadKesList() KesList {
	kl := KesList{Kes: []KesInfo{}, Updated: time.Now().Unix()}
	data, err := readShmAt(kesShmPath, 0, int64(kesShmHeader+kesSlots*kesListItemLen))
	if err != nil || len(data) < kesShmHeader {
		return kl
	}
	if binary.LittleEndian.Uint32(data[0:4]) != kesShmMagic ||
		binary.LittleEndian.Uint32(data[4:8]) != kesShmVersion {
		return kl
	}
	for i := 0; i < kesSlots; i++ {
		off := kesShmHeader + i*kesListItemLen
		if off+kesListItemLen > len(data) {
			break
		}
		if it, ok := decodeKesListItem(i, data[off:off+kesListItemLen]); ok {
			kl.Kes = append(kl.Kes, it)
		}
	}
	kl.Count = len(kl.Kes)
	return kl
}

func decodeKesListItem(i int, b []byte) (KesInfo, bool) {
	if binary.LittleEndian.Uint32(b[0:4]) != 1 {
		return KesInfo{}, false
	}
	if binary.LittleEndian.Uint64(b[8:16])&1 != 0 { // seqlock: идёт запись
		return KesInfo{}, false
	}
	return KesInfo{
		ID:       i,
		Gen:      binary.LittleEndian.Uint64(b[kesListGenOff : kesListGenOff+8]),
		Baud:     int(binary.LittleEndian.Uint32(b[kesListBaudOff : kesListBaudOff+4])),
		Serial:   binary.LittleEndian.Uint16(b[kesListSerialOff : kesListSerialOff+2]),
		Port:     strings.TrimRight(string(b[kesListPortOff:kesListPortOff+kesContractPortSize]), "\x00"),
		LastSeen: int64(binary.LittleEndian.Uint64(b[kesListLastSeenOff : kesListLastSeenOff+8])),
	}, true
}

// kesRawPayload возвращает бинарный payload слота id (KESE+KESR+KESS, 1536 Б) или
// nil, если слот неактивен/некорректен.
func kesRawPayload(id int) []byte {
	if id < 0 || id >= kesSlots {
		return nil
	}
	off := int64(kesShmHeader + kesSlots*kesListItemLen + id*kesSlotSize)
	slot, err := readShmAt(kesShmPath, off, int64(kesSlotSize))
	if err != nil {
		return nil
	}
	if binary.LittleEndian.Uint32(slot[0:4]) != 1 { // state
		return nil
	}
	if binary.LittleEndian.Uint64(slot[16:24])&1 != 0 { // seqlock
		return nil
	}
	plen := int(binary.LittleEndian.Uint32(slot[24:28]))
	if plen <= 0 || plen > kesSlotData || kesSlotHdr+plen > len(slot) {
		return nil
	}
	out := make([]byte, plen)
	copy(out, slot[kesSlotHdr:kesSlotHdr+plen])
	return out
}
