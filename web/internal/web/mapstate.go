package web

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"
)

// Чтение бинарного контракта map-reader из POSIX shared memory.
// Формат — docs/map-contract.md (синхронизирован с daemons/map-reader/shm.go).
const (
	mapShmPath     = "/dev/shm/malina-map-state"
	mapShmMagic    = 0x314D4150
	mapShmVersion  = 3
	mapShmHeader   = 64
	mapShmSlots    = 9
	mapListItemLen = 72
	mapSlotHdr     = 32
	mapSlotData    = 16384
	mapSlotSize    = mapSlotHdr + mapSlotData
)

// MapInfo — запись списка МАП (компактный блок списка).
type MapInfo struct {
	ID           int    `json:"id"`
	Gen          uint64 `json:"gen"`
	Port         string `json:"port"`
	Unit         int    `json:"unit"`
	Model        string `json:"model"`
	Protocol     string `json:"protocol"`
	Baud         int    `json:"baud"`
	Serial       uint16 `json:"serial"`
	SerialLetter string `json:"serial_letter"`
	State        string `json:"state"`
	LastSeen     int64  `json:"last_seen"`
}

// SerialLabel — серийный номер с буквой (информационное поле).
func (m MapInfo) SerialLabel() string {
	return strconv.FormatUint(uint64(m.Serial), 10) + m.SerialLetter
}

// MenuLabel — имя подпункта меню: серийник + порт + скорость.
func (m MapInfo) MenuLabel() string {
	return m.SerialLabel() + " · " + m.Port + " · " + strconv.FormatUint(uint64(m.Baud), 10)
}

type MapList struct {
	Updated int64     `json:"updated"`
	Count   int       `json:"count"`
	Maps    []MapInfo `json:"maps"`
}

func loadMapList() MapList {
	ml := MapList{Maps: []MapInfo{}, Updated: time.Now().Unix()}
	data, err := os.ReadFile(mapShmPath)
	if err != nil || len(data) < mapShmHeader {
		return ml
	}
	if binary.LittleEndian.Uint32(data[0:4]) != mapShmMagic ||
		binary.LittleEndian.Uint32(data[4:8]) != mapShmVersion {
		return ml
	}
	for i := 0; i < mapShmSlots; i++ {
		off := mapShmHeader + i*mapListItemLen
		if off+mapListItemLen > len(data) {
			break
		}
		if it, ok := decodeMapListItem(i, data[off:off+mapListItemLen]); ok {
			ml.Maps = append(ml.Maps, it)
		}
	}
	ml.Count = len(ml.Maps)
	return ml
}

func decodeMapListItem(i int, b []byte) (MapInfo, bool) {
	if binary.LittleEndian.Uint32(b[0:4]) != 1 {
		return MapInfo{}, false
	}
	if binary.LittleEndian.Uint64(b[8:16])&1 != 0 { // seqlock: идёт запись
		return MapInfo{}, false
	}
	model := ""
	if b[4] != 0 {
		model = string([]byte{b[4]})
	}
	proto := "microart"
	if b[5] == 1 {
		proto = "modbus"
	}
	letter := ""
	if b[30] != 0 {
		letter = string([]byte{b[30]})
	}
	return MapInfo{
		ID:           i,
		Model:        model,
		Protocol:     proto,
		Unit:         int(binary.LittleEndian.Uint16(b[6:8])),
		Gen:          binary.LittleEndian.Uint64(b[16:24]),
		Baud:         int(binary.LittleEndian.Uint32(b[24:28])),
		Serial:       binary.LittleEndian.Uint16(b[28:30]),
		SerialLetter: letter,
		Port:         strings.TrimRight(string(b[32:64]), "\x00"),
		LastSeen:     int64(binary.LittleEndian.Uint64(b[64:72])),
		State:        "online",
	}, true
}

// mapRawPayload возвращает бинарный payload слота id (MapData) или nil,
// если слот не активен/некорректен.
func mapRawPayload(id int) []byte {
	if id < 0 || id >= mapShmSlots {
		return nil
	}
	data, err := os.ReadFile(mapShmPath)
	if err != nil || len(data) < mapShmHeader {
		return nil
	}
	off := mapShmHeader + mapShmSlots*mapListItemLen + id*mapSlotSize
	if off+mapSlotSize > len(data) {
		return nil
	}
	slot := data[off : off+mapSlotSize]
	if binary.LittleEndian.Uint32(slot[0:4]) != 1 { // state
		return nil
	}
	if binary.LittleEndian.Uint64(slot[16:24])&1 != 0 { // seqlock
		return nil
	}
	plen := int(binary.LittleEndian.Uint32(slot[24:28]))
	if plen <= 0 || plen > mapSlotData || mapSlotHdr+plen > len(slot) {
		return nil
	}
	out := make([]byte, plen)
	copy(out, slot[mapSlotHdr:mapSlotHdr+plen])
	return out
}

// findMap ищет запись списка по id.
func findMap(id int) (MapInfo, bool) {
	for _, m := range loadMapList().Maps {
		if m.ID == id {
			return m, true
		}
	}
	return MapInfo{}, false
}
