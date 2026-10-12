package main

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// shm: раскладка kes-reader. Механика — lib/shm; байтовый контракт kes-list/payload
// — docs/kes-contract.md (кратко: docs/kes-reader.md §4) — читается/пишется в этом же
// модуле и зафиксирован тестом (см. kes_test.go, golden-тест контракта).
const (
	kesShmMagic     = 0x3153454B // "KES1"
	kesShmVersion   = 1
	kesShmHeader    = shm.Header
	kesSlots        = 32
	kesListItemSize = 72
	kesSlotData     = 16384

	contractPortSize = 32
)

// Смещения записи kes-list (72 Б) — зафиксированы, чтобы код/тесты не расходились
// с docs/kes-reader.md §4 и golden-тестом (защита от дрейфа, «H2» ревью).
const (
	listGenOff      = 16 // uint64 LE
	listBaudOff     = 24 // uint32 LE
	listSerialOff   = 28 // uint16 LE
	listPortOff     = 32 // string[contractPortSize] (NUL)
	listLastSeenOff = 64 // int64 LE
)

type (
	Shm      = shm.Segment
	Slot     = shm.Slot
	ListItem = shm.Item
)

func shmOpen(path string, slots int) (*Shm, error) {
	if slots != kesSlots {
		return nil, errors.New("shm: unsupported slot count")
	}
	return shm.Open(path, shm.Layout{
		Slots:    slots,
		ItemSize: kesListItemSize,
		SlotData: kesSlotData,
		Magic:    kesShmMagic,
		Version:  kesShmVersion,
	})
}

// contractMeta — мета контракта КЭС (слот).
type contractMeta struct {
	Port     string
	Serial   uint16
	Gen      uint64
	LastSeen time.Time
}

// listMeta — запись kes-list (компактно, itemSize=72).
type listMeta struct {
	Serial   uint16
	Baud     uint32
	Gen      uint64
	Port     string
	LastSeen int64
}

func publishItem(it *ListItem, m listMeta) {
	it.Publish(func(b []byte) {
		binary.LittleEndian.PutUint64(b[listGenOff:listGenOff+8], m.Gen)
		binary.LittleEndian.PutUint32(b[listBaudOff:listBaudOff+4], m.Baud)
		binary.LittleEndian.PutUint16(b[listSerialOff:listSerialOff+2], m.Serial)
		copy(b[listPortOff:listPortOff+contractPortSize], []byte(m.Port))
		binary.LittleEndian.PutUint64(b[listLastSeenOff:listLastSeenOff+8], uint64(m.LastSeen))
	})
}

// KesView — запись kes-list для API.
type KesView struct {
	ID       int    `json:"id"`
	Gen      uint64 `json:"gen"`
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	Serial   uint16 `json:"serial"`
	LastSeen int64  `json:"last_seen"`
}

// StateDoc — документ состояния kes-list (для control-сокета).
type StateDoc struct {
	Updated int64     `json:"updated"`
	Count   int       `json:"count"`
	Kes     []KesView `json:"kes"`
}

// listDoc строит kes-list из бинарных записей сегмента.
func listDoc(s *Shm) StateDoc {
	d := StateDoc{Updated: time.Now().Unix()}
	data := s.Data()
	for i := 0; i < s.Slots(); i++ {
		off := kesShmHeader + i*s.ItemSize()
		b := data[off : off+s.ItemSize()]
		if binary.LittleEndian.Uint32(b[0:4]) != 1 {
			continue
		}
		if binary.LittleEndian.Uint64(b[8:16])&1 != 0 {
			continue // seqlock: идёт запись — пропускаем неконсистентную запись
		}
		d.Kes = append(d.Kes, KesView{
			ID:       i,
			Gen:      binary.LittleEndian.Uint64(b[listGenOff : listGenOff+8]),
			Baud:     int(binary.LittleEndian.Uint32(b[listBaudOff : listBaudOff+4])),
			Serial:   binary.LittleEndian.Uint16(b[listSerialOff : listSerialOff+2]),
			Port:     trimZero(b[listPortOff : listPortOff+contractPortSize]),
			LastSeen: int64(binary.LittleEndian.Uint64(b[listLastSeenOff : listLastSeenOff+8])),
		})
	}
	d.Count = len(d.Kes)
	return d
}

func trimZero(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
