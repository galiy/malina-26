// Package shm — механика POSIX shared memory для демонов ПАК «Малина»: один
// сегмент с seqlock-слотами и блоком компактных записей списка.
//
// Раскладка (значения задаёт демон; байтовый контракт НЕ меняется этим пакетом):
//
//	[header 64B][list: slots×itemSize][slots: slots×slotData]
//
// Заголовок:
//
//	0  magic    uint32
//	4  version  uint32
//	8  slotSize uint32
//	12 slots    uint32
//
// Slot (slotHeader 32B + payload):
//
//	0  state uint32
//	4  reserved uint32
//	8  gen uint64
//	16 seq uint64        (seqlock)
//	24 len uint32
//	28 crc uint32
//	32 payload
//
// Item (itemSize B) — компактная запись списка; формат его полей определяет демон,
// общий только seqlock-каркас:
//
//	0  state uint32
//	8  seq uint64        (seqlock)
//	остальное — по усмотрению демона (заполняется через Publish).
package shm

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	Header      = 64
	SlotHeader  = 32
	MaxSlotData = 16384
)

// Layout описывает размеры сегмента и «магию»/версию заголовка.
type Layout struct {
	Slots    int
	ItemSize int
	SlotData int
	Magic    uint32
	Version  uint32
}

// slotSize — полный размер слота.
func (l Layout) slotSize() int { return SlotHeader + l.SlotData }

// Segment — отображённый сегмент shared memory.
type Segment struct {
	f    *os.File
	data []byte
	lay  Layout
}

// Open создаёт/отображает сегмент и инициализирует заголовок (один писатель-процесс).
func Open(path string, lay Layout) (*Segment, error) {
	if lay.Slots <= 0 || lay.ItemSize <= 0 || lay.SlotData <= 0 || lay.SlotData > MaxSlotData {
		return nil, errors.New("shm: неверная раскладка")
	}
	size := Header + lay.Slots*lay.ItemSize + lay.Slots*lay.slotSize()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o660)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	s := &Segment{f: f, data: data, lay: lay}
	for i := range data { // один писатель-процесс: чистим при старте
		data[i] = 0
	}
	binary.LittleEndian.PutUint32(data[0:4], lay.Magic)
	binary.LittleEndian.PutUint32(data[4:8], lay.Version)
	binary.LittleEndian.PutUint32(data[8:12], uint32(lay.slotSize()))
	binary.LittleEndian.PutUint32(data[12:16], uint32(lay.Slots))
	return s, nil
}

// Close снимает отображение и закрывает файл.
func (s *Segment) Close() {
	if s.data != nil {
		_ = syscall.Munmap(s.data)
		s.data = nil
	}
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}

// Data возвращает весь сегмент (для читателей, напр. DecodeList).
func (s *Segment) Data() []byte { return s.data }

// Slots возвращает число слотов.
func (s *Segment) Slots() int { return s.lay.Slots }

// SlotData возвращает размер payload слота.
func (s *Segment) SlotData() int { return s.lay.SlotData }

// ItemSize возвращает размер записи списка.
func (s *Segment) ItemSize() int { return s.lay.ItemSize }

func (s *Segment) slotOff(i int) int { return Header + s.lay.Slots*s.lay.ItemSize + i*s.lay.slotSize() }
func (s *Segment) itemOff(i int) int { return Header + i*s.lay.ItemSize }

// Slot возвращает слот i.
func (s *Segment) Slot(i int) *Slot {
	off := s.slotOff(i)
	return &Slot{b: s.data[off : off+s.lay.slotSize()]}
}

// Item возвращает запись списка i.
func (s *Segment) Item(i int) *Item {
	off := s.itemOff(i)
	return &Item{b: s.data[off : off+s.lay.ItemSize]}
}

// Slot — слот с seqlock-публикацией бинарного payload.
type Slot struct{ b []byte }

func (sl *Slot) state() *uint32 { return (*uint32)(unsafe.Pointer(&sl.b[0])) }
func (sl *Slot) gen() *uint64   { return (*uint64)(unsafe.Pointer(&sl.b[8])) }
func (sl *Slot) seq() *uint64   { return (*uint64)(unsafe.Pointer(&sl.b[16])) }
func (sl *Slot) lenn() *uint32  { return (*uint32)(unsafe.Pointer(&sl.b[24])) }
func (sl *Slot) crc() *uint32   { return (*uint32)(unsafe.Pointer(&sl.b[28])) }

// PrepareGen готовит слот к записи (state=0 до первой публикации) и задаёт gen.
func (sl *Slot) PrepareGen(gen uint64) {
	atomic.StoreUint32(sl.state(), 0)
	atomic.StoreUint64(sl.seq(), 0)
	atomic.StoreUint32(sl.lenn(), 0)
	atomic.StoreUint64(sl.gen(), gen)
}

// Release помечает слот свободным (seqlock-безопасно: нечётный seq на время).
func (sl *Slot) Release() {
	sq := sl.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	atomic.StoreUint32(sl.lenn(), 0)
	atomic.StoreUint32(sl.state(), 0)
	atomic.StoreUint64(sq, seq+2)
}

// Publish атомарно публикует payload (обрезается до размеров слота).
func (sl *Slot) Publish(payload []byte) {
	max := len(sl.b) - SlotHeader
	if len(payload) > max {
		payload = payload[:max]
	}
	sq := sl.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	copy(sl.b[SlotHeader:], payload)
	atomic.StoreUint32(sl.lenn(), uint32(len(payload)))
	atomic.StoreUint32(sl.crc(), crc32.ChecksumIEEE(payload))
	atomic.StoreUint64(sq, seq+2)
	atomic.StoreUint32(sl.state(), 1) // слот валиден только после публикации
}

// Item — запись списка: общий seqlock-каркас, формат полей задаёт демон.
type Item struct{ b []byte }

func (it *Item) state() *uint32 { return (*uint32)(unsafe.Pointer(&it.b[0])) }
func (it *Item) seq() *uint64   { return (*uint64)(unsafe.Pointer(&it.b[8])) }

// Clear очищает запись при выдаче слота (state остаётся 0 до первой публикации).
func (it *Item) Clear() {
	for i := range it.b {
		it.b[i] = 0
	}
}

// Release помечает запись свободной (seqlock-безопасно).
func (it *Item) Release() {
	sq := it.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	atomic.StoreUint32(it.state(), 0)
	atomic.StoreUint64(sq, seq+2)
}

// Publish атомарно публикует запись: write заполняет байты b (кроме seqlock-полей).
func (it *Item) Publish(write func(b []byte)) {
	sq := it.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	if write != nil {
		write(it.b)
	}
	atomic.StoreUint64(sq, seq+2)
	atomic.StoreUint32(it.state(), 1) // запись валидна только после публикации
}

// Bytes возвращает байты записи.
func (it *Item) Bytes() []byte { return it.b }
