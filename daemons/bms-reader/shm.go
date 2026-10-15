package main

import (
	"bytes"
	"encoding/binary"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// Раскладка shm-сегмента (arch §12/§14):
//   - Items: список устройств (метаданные), пишет оркестратор;
//   - Slots: телеметрия слота — бинарный контракт 107 Б, пишет пулер.
//
// Запись списка (item): [seqlock: state@0, seq@8][поля @16..].
const (
	shmMagic   uint32 = 0x424D5342 // "BSMB"
	shmVersion uint32 = 1
	itemSize          = 200
	idMax             = 50
	baseMax           = 50
	sourceMax         = 64
)

// Поля записи списка (смещения внутри item).
const (
	itOffID       = 16
	itOffBase     = itOffID + idMax         // 66
	itOffSource   = itOffBase + baseMax     // 116
	itOffTypeID   = itOffSource + sourceMax // 180
	itOffState    = itOffTypeID + 1         // 181
	itOffFraming  = itOffState + 1          // 182
	itOffGen      = itOffFraming + 2        // 184
	itOffLastSeen = itOffGen + 8            // 192
)

// listRecord — запись списка BMS (для shm и control/list).
type listRecord struct {
	ID       string
	Base     string
	Source   string
	TypeID   uint8 // 1=AntBMS-DSP, 2=EnBMS-RS485 (внутренний код типа)
	State    uint8 // 0=lost, 1=ok
	Framing  uint8 // 0=native, 1=m-rtu, 2=m-tcp
	Gen      uint64
	LastSeen uint64
}

// bmsShm — обёртка сегмента: параллельные списки Item (метаданные) и Slot (107 Б).
type bmsShm struct{ seg *shm.Segment }

func openShm() (*bmsShm, error) {
	seg, err := shm.Open(shmPath, bmsLayout())
	if err != nil {
		return nil, err
	}
	return &bmsShm{seg: seg}, nil
}

func (s *bmsShm) Close() { s.seg.Close() }

// list возвращает занятые записи списка (state != 0).
func (s *bmsShm) list() []listRecord {
	out := make([]listRecord, 0, s.seg.Slots())
	for i := 0; i < s.seg.Slots(); i++ {
		b := s.seg.Item(i).Bytes()
		if binary.LittleEndian.Uint32(b[0:4]) == 0 {
			continue
		}
		out = append(out, decodeItem(b))
	}
	return out
}

// publishList публикует запись списка в item i.
func (s *bmsShm) publishList(i int, r listRecord) {
	s.seg.Item(i).Publish(func(b []byte) { encodeItem(b, r) })
}

// allocSlot готовит item+slot i под устройство (gen).
func (s *bmsShm) allocSlot(i int, gen uint64) {
	s.seg.Item(i).Clear()
	s.seg.Slot(i).PrepareGen(gen)
}

// publishTelemetry публикует 107-Б payload в slot i.
func (s *bmsShm) publishTelemetry(i int, payload []byte) {
	s.seg.Slot(i).Publish(payload)
}

// release освобождает item и slot i.
func (s *bmsShm) release(i int) {
	s.seg.Item(i).Release()
	s.seg.Slot(i).Release()
}

// bmsLayout — раскладка сегмента: список + до maxDevices телеметрийных слотов.
func bmsLayout() shm.Layout {
	return shm.Layout{
		Slots:    maxDevices,
		ItemSize: itemSize,
		SlotData: 107, // bmscontract.Size
		Magic:    shmMagic,
		Version:  shmVersion,
	}
}

// encodeItem заполняет поля записи списка (seqlock-поля не трогает).
func encodeItem(b []byte, r listRecord) {
	if len(b) < itemSize {
		return
	}
	for i := itOffID; i < itemSize; i++ {
		b[i] = 0
	}
	putStr(b[itOffID:itOffID+idMax], r.ID)
	putStr(b[itOffBase:itOffBase+baseMax], r.Base)
	putStr(b[itOffSource:itOffSource+sourceMax], r.Source)
	b[itOffTypeID] = r.TypeID
	b[itOffState] = r.State
	b[itOffFraming] = r.Framing
	binary.LittleEndian.PutUint64(b[itOffGen:], r.Gen)
	binary.LittleEndian.PutUint64(b[itOffLastSeen:], r.LastSeen)
}

// decodeItem читает запись списка.
func decodeItem(b []byte) listRecord {
	var r listRecord
	if len(b) < itemSize {
		return r
	}
	r.ID = getStr(b[itOffID : itOffID+idMax])
	r.Base = getStr(b[itOffBase : itOffBase+baseMax])
	r.Source = getStr(b[itOffSource : itOffSource+sourceMax])
	r.TypeID = b[itOffTypeID]
	r.State = b[itOffState]
	r.Framing = b[itOffFraming]
	r.Gen = binary.LittleEndian.Uint64(b[itOffGen:])
	r.LastSeen = binary.LittleEndian.Uint64(b[itOffLastSeen:])
	return r
}

func putStr(dst []byte, s string) {
	if len(s) > len(dst) {
		s = s[:len(dst)]
	}
	copy(dst, s)
}

func getStr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
