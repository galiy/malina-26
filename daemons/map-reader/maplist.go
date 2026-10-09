package main

import (
	"encoding/binary"
	"errors"
	"sync/atomic"
	"unsafe"
)

// MapListItem — элемент списка МАП (бинарная запись listItemSize=72 Б).
type MapListItem struct {
	ID           int
	Gen          uint64
	Port         string
	Model        string
	Protocol     string
	Unit         int
	Baud         int
	Serial       uint16
	SerialLetter byte
	State        string
	LastSeen     int64
}

// MapList — коллекция активных МАП (бинарь в сегменте shm). Собирается
// читателем сканированием записей списка (state==1, seqlock стабилен).
type MapList struct {
	Updated int64
	Count   int
	Items   []MapListItem
}

// decodeListItem разбирает одну запись списка с проверкой seqlock.
func decodeListItem(i int, b []byte) (MapListItem, bool) {
	if atomic.LoadUint32((*uint32)(unsafe.Pointer(&b[0]))) != 1 {
		return MapListItem{}, false
	}
	for retry := 0; retry < 8; retry++ {
		seq := (*uint64)(unsafe.Pointer(&b[8]))
		s1 := atomic.LoadUint64(seq)
		if s1&1 != 0 {
			return MapListItem{}, false
		}
		it := MapListItem{
			ID:           i,
			Model:        string([]byte{b[4]}),
			Protocol:     protoName(b[5]),
			Unit:         int(binary.LittleEndian.Uint16(b[6:8])),
			Gen:          binary.LittleEndian.Uint64(b[16:24]),
			Baud:         int(binary.LittleEndian.Uint32(b[24:28])),
			Serial:       binary.LittleEndian.Uint16(b[28:30]),
			SerialLetter: b[30],
			Port:         trimZero(b[32 : 32+contractPortSize]),
			LastSeen:     int64(binary.LittleEndian.Uint64(b[64:72])),
			State:        "online",
		}
		if atomic.LoadUint64(seq) != s1 {
			continue
		}
		return it, true
	}
	return MapListItem{}, false
}

// DecodeList собирает MapList из сегмента shm (заголовок + блок записей списка).
func DecodeList(data []byte) (MapList, error) {
	if len(data) < shmHeader {
		return MapList{}, errors.New("short segment")
	}
	if binary.LittleEndian.Uint32(data[0:4]) != shmMagic {
		return MapList{}, errors.New("bad magic")
	}
	slots := int(binary.LittleEndian.Uint32(data[12:16]))
	var ml MapList
	for i := 0; i < slots; i++ {
		off := shmHeader + i*listItemSize
		if off+listItemSize > len(data) {
			break
		}
		if it, ok := decodeListItem(i, data[off:off+listItemSize]); ok {
			ml.Items = append(ml.Items, it)
		}
	}
	ml.Count = len(ml.Items)
	return ml, nil
}
