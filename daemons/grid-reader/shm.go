package main

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

const (
	gridShmMagic    = 0x31445247 // "GRD1"
	gridShmVersion  = 1
	gridSlots       = maxInverters
	gridItemSize    = 160
	gridSlotData    = 64
	gridSerialSize  = 50
	gridAddrSize    = 64
	gridTelemetrySz = 55 // contract payload 50 + energy_today 2 + energy_total 3
)

// Смещения записи списка (160 Б), см. docs/grid-reader.md.
const (
	listGenOff      = 136
	listSerialOff   = 16
	listAddrOff     = 66
	listKindOff     = 130
	listStatusOff   = 131
	listLastSeenOff = 144
)

type Shm = shm.Segment
type Slot = shm.Slot
type ListItem = shm.Item

func shmOpen(path string) (*Shm, error) {
	if gridSlots != maxInverters {
		return nil, errors.New("shm: неверное число слотов")
	}
	return shm.Open(path, shm.Layout{
		Slots:    gridSlots,
		ItemSize: gridItemSize,
		SlotData: gridSlotData,
		Magic:    gridShmMagic,
		Version:  gridShmVersion,
	})
}

type listMeta struct {
	Serial   string
	Addr     string
	Kind     uint8
	Status   uint8
	Gen      uint64
	LastSeen int64
}

func publishItem(it *ListItem, m listMeta) {
	it.Publish(func(b []byte) {
		for i := listSerialOff; i < listSerialOff+gridSerialSize; i++ {
			b[i] = 0
		}
		for i := listAddrOff; i < listAddrOff+gridAddrSize; i++ {
			b[i] = 0
		}
		copy(b[listSerialOff:listSerialOff+gridSerialSize], []byte(m.Serial))
		copy(b[listAddrOff:listAddrOff+gridAddrSize], []byte(m.Addr))
		b[listKindOff] = m.Kind
		b[listStatusOff] = m.Status
		binary.LittleEndian.PutUint64(b[listGenOff:listGenOff+8], m.Gen)
		binary.LittleEndian.PutUint64(b[listLastSeenOff:listLastSeenOff+8], uint64(m.LastSeen))
	})
}

type GridView struct {
	ID       int    `json:"id"`
	Gen      uint64 `json:"gen"`
	Serial   string `json:"serial"`
	Addr     string `json:"addr"`
	Kind     uint8  `json:"kind"`
	Status   uint8  `json:"status"`
	LastSeen int64  `json:"last_seen"`
}

type StateDoc struct {
	Updated   int64      `json:"updated"`
	Count     int        `json:"count"`
	Inverters []GridView `json:"inverters"`
}

func listDoc(s *Shm) StateDoc {
	d := StateDoc{Updated: time.Now().Unix()}
	data := s.Data()
	if len(data) < shm.Header || binary.LittleEndian.Uint32(data[0:4]) != gridShmMagic || binary.LittleEndian.Uint32(data[4:8]) != gridShmVersion {
		return d
	}
	for i := 0; i < s.Slots(); i++ {
		off := shm.Header + i*s.ItemSize()
		b := data[off : off+s.ItemSize()]
		if binary.LittleEndian.Uint32(b[0:4]) != 1 {
			continue
		}
		seq1 := binary.LittleEndian.Uint64(b[8:16])
		if seq1&1 != 0 {
			continue
		}
		d.Inverters = append(d.Inverters, GridView{
			ID:       i,
			Gen:      binary.LittleEndian.Uint64(b[listGenOff : listGenOff+8]),
			Serial:   trimZero(b[listSerialOff : listSerialOff+gridSerialSize]),
			Addr:     trimZero(b[listAddrOff : listAddrOff+gridAddrSize]),
			Kind:     b[listKindOff],
			Status:   b[listStatusOff],
			LastSeen: int64(binary.LittleEndian.Uint64(b[listLastSeenOff : listLastSeenOff+8])),
		})
		if binary.LittleEndian.Uint64(b[8:16]) != seq1 {
			d.Inverters = d.Inverters[:len(d.Inverters)-1]
		}
	}
	d.Count = len(d.Inverters)
	return d
}

func trimZero(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
