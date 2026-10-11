package main

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// shm: раскладка kes-reader. Механика — lib/shm; контракт kes-list/payload —
// provisional (см. docs/kes-reader.md §4, docs/kes-contract.md — план).
const (
	kesShmMagic     = 0x3153454B // "KES1"
	kesShmVersion   = 1
	kesShmHeader    = shm.Header
	kesSlots        = 9
	kesListItemSize = 72
	kesSlotData     = 16384

	contractPortSize = 32
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
	Model    string
	UID      uint16
	Gen      uint64
	LastSeen time.Time
}

// listMeta — запись kes-list (компактно, itemSize=72).
type listMeta struct {
	Model    byte
	Protocol uint8
	UID      uint16
	Baud     uint32
	Gen      uint64
	Port     string
	LastSeen int64
}

func publishItem(it *ListItem, m listMeta) {
	it.Publish(func(b []byte) {
		b[4] = m.Model
		b[5] = m.Protocol
		binary.LittleEndian.PutUint64(b[16:24], m.Gen)
		binary.LittleEndian.PutUint32(b[24:28], m.Baud)
		binary.LittleEndian.PutUint16(b[28:30], m.UID)
		copy(b[32:32+contractPortSize], []byte(m.Port))
		binary.LittleEndian.PutUint64(b[64:72], uint64(m.LastSeen))
	})
}

// KesView — запись kes-list для API.
type KesView struct {
	ID       int    `json:"id"`
	Gen      uint64 `json:"gen"`
	Port     string `json:"port"`
	Protocol string `json:"protocol"`
	Baud     int    `json:"baud"`
	Model    string `json:"model"`
	UID      uint16 `json:"uid"`
	State    string `json:"state"`
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
		d.Kes = append(d.Kes, KesView{
			ID:       i,
			Gen:      binary.LittleEndian.Uint64(b[16:24]),
			Protocol: "microart",
			Baud:     int(binary.LittleEndian.Uint32(b[24:28])),
			Model:    string([]byte{b[4]}),
			UID:      binary.LittleEndian.Uint16(b[28:30]),
			Port:     trimZero(b[32 : 32+contractPortSize]),
			State:    "online",
			LastSeen: int64(binary.LittleEndian.Uint64(b[64:72])),
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
