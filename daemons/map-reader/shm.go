package main

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// shm: раскладка map-reader (байтовый контракт см. docs/map-contract.md).
// Механика сегмента/секлока — в lib/shm; здесь — только размеры и map-поля.
const (
	shmMagic     = 0x314D4150
	shmVersion   = 3
	shmHeader    = shm.Header
	shmSlots     = 9
	listItemSize = 72
	slotData     = 16384
)

// Алиасы на общие типы (механика — lib/shm).
type (
	Shm      = shm.Segment
	Slot     = shm.Slot
	ListItem = shm.Item
)

func shmOpen(path string, slots int) (*Shm, error) {
	if slots != shmSlots { // раскладка/аллокатор рассчитаны на shmSlots
		return nil, errors.New("shm: unsupported slot count")
	}
	return shm.Open(path, shm.Layout{
		Slots:    slots,
		ItemSize: listItemSize,
		SlotData: slotData,
		Magic:    shmMagic,
		Version:  shmVersion,
	})
}

// listMeta — поля записи map-list (компактная запись listItemSize=72 Б).
type listMeta struct {
	Model        byte
	Protocol     uint8
	Unit         uint16
	Gen          uint64
	Baud         uint32
	Serial       uint16
	SerialLetter byte
	Port         string
	LastSeen     int64
}

// publishItem публикует запись map-list с seqlock (поля поверх lib/shm).
func publishItem(it *ListItem, m listMeta) {
	it.Publish(func(b []byte) {
		b[4] = m.Model
		b[5] = m.Protocol
		binary.LittleEndian.PutUint16(b[6:8], m.Unit)
		binary.LittleEndian.PutUint64(b[16:24], m.Gen)
		binary.LittleEndian.PutUint32(b[24:28], m.Baud)
		binary.LittleEndian.PutUint16(b[28:30], m.Serial)
		b[30] = m.SerialLetter
		copy(b[32:32+contractPortSize], []byte(m.Port))
		binary.LittleEndian.PutUint64(b[64:72], uint64(m.LastSeen))
	})
}

// listDoc строит список (для control-сокета) из бинарных записей.
func listDoc(s *Shm) StateDoc {
	d := StateDoc{Updated: time.Now().Unix()}
	data := s.Data()
	for i := 0; i < s.Slots(); i++ {
		off := shmHeader + i*s.ItemSize()
		if item, ok := decodeListItem(i, data[off:off+s.ItemSize()]); ok {
			d.Maps = append(d.Maps, MapView{
				ID: item.ID, Gen: item.Gen, Port: item.Port, Unit: item.Unit,
				Protocol: item.Protocol, Baud: item.Baud, Model: item.Model,
				Serial: item.Serial, SerialLetter: string([]byte{item.SerialLetter}),
				State: item.State, LastSeen: item.LastSeen,
			})
		}
	}
	d.Count = len(d.Maps)
	return d
}

func protoName(p uint8) string {
	if p == 1 {
		return "modbus"
	}
	return "microart"
}
