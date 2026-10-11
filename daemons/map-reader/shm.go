package main

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// POSIX shared memory: один сегмент.
//
//	[header 64B][list: slots×listItemSize][slots: slots×slotSize]
//
// Заголовок:
//
//	0  magic   uint32
//	4  version uint32
//	8  slotSize uint32
//	12 slots    uint32
//
// ListItem (72B) — компактная запись для map-list (одна на слот):
//
//	0  state uint32       (0 свободен / 1 активен)
//	4  model uint8
//	5  protocol uint8     (0=Микроарт,1=Modbus)
//	6  unit uint16
//	8  seq uint64         (seqlock)
//	16 gen uint64
//	24 baud uint32
//	28 serial uint16
//	30 serialLetter uint8
//	31 reserved uint8
//	32 port [32]byte
//	64 lastSeen uint64
//
// Slot (slotSize B): заголовок + бинарный контракт MapData:
//
//	0  state uint32
//	4  reserved uint32
//	8  gen uint64
//	16 seq uint64
//	24 len uint32
//	28 crc uint32
//	32 payload
const (
	shmMagic     = 0x314D4150
	shmVersion   = 3
	shmHeader    = 64
	shmSlots     = 9
	listItemSize = 72
	slotHdr      = 32
	slotData     = 16384
	slotSize     = slotHdr + slotData
)

type Shm struct {
	f     *os.File
	data  []byte
	slots int
}

func shmOpen(path string, slots int) (*Shm, error) {
	if slots != shmSlots { // раскладка/аллокатор рассчитаны на shmSlots
		return nil, errors.New("shm: unsupported slot count")
	}
	size := shmHeader + slots*listItemSize + slots*slotSize
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
	s := &Shm{f: f, data: data, slots: slots}
	for i := range data { // один писатель-процесс: чистим при старте
		data[i] = 0
	}
	binary.LittleEndian.PutUint32(data[0:4], shmMagic)
	binary.LittleEndian.PutUint32(data[4:8], shmVersion)
	binary.LittleEndian.PutUint32(data[8:12], slotSize)
	binary.LittleEndian.PutUint32(data[12:16], uint32(slots))
	return s, nil
}

func (s *Shm) slotOff(i int) int { return shmHeader + s.slots*listItemSize + i*slotSize }
func (s *Shm) itemOff(i int) int { return shmHeader + i*listItemSize }

// Slot возвращает слот i.
func (s *Shm) Slot(i int) *Slot {
	off := s.slotOff(i)
	return &Slot{b: s.data[off : off+slotSize]}
}

// Item возвращает запись списка i.
func (s *Shm) Item(i int) *ListItem {
	off := s.itemOff(i)
	return &ListItem{b: s.data[off : off+listItemSize]}
}

type Slot struct {
	b []byte
}

func (sl *Slot) state() *uint32 { return (*uint32)(unsafe.Pointer(&sl.b[0])) }
func (sl *Slot) gen() *uint64   { return (*uint64)(unsafe.Pointer(&sl.b[8])) }
func (sl *Slot) seq() *uint64   { return (*uint64)(unsafe.Pointer(&sl.b[16])) }
func (sl *Slot) lenn() *uint32  { return (*uint32)(unsafe.Pointer(&sl.b[24])) }
func (sl *Slot) crc() *uint32   { return (*uint32)(unsafe.Pointer(&sl.b[28])) }

// PrepareGen готовит слот к записи (state=0 до первой публикации) и задаёт gen.
// gen задаёт вызывающий (timestamp, монотонно) — см. slotAlloc.alloc.
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

func (sl *Slot) Publish(payload []byte) {
	if len(payload) > slotData {
		payload = payload[:slotData]
	}
	sq := sl.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	copy(sl.b[slotHdr:], payload)
	atomic.StoreUint32(sl.lenn(), uint32(len(payload)))
	atomic.StoreUint32(sl.crc(), crc32.ChecksumIEEE(payload))
	atomic.StoreUint64(sq, seq+2)
	atomic.StoreUint32(sl.state(), 1) // слот валиден только после публикации
}

// ListItem — компактная запись map-list.
type ListItem struct {
	b []byte
}

func (it *ListItem) state() *uint32 { return (*uint32)(unsafe.Pointer(&it.b[0])) }
func (it *ListItem) seq() *uint64   { return (*uint64)(unsafe.Pointer(&it.b[8])) }

// reset очищает запись при выдаче слота (state остаётся 0 до первой публикации).
func (it *ListItem) reset() {
	for i := range it.b {
		it.b[i] = 0
	}
}

func (it *ListItem) Release() {
	sq := it.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	atomic.StoreUint32(it.state(), 0)
	atomic.StoreUint64(sq, seq+2)
}

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

func (it *ListItem) Publish(m listMeta) {
	sq := it.seq()
	seq := atomic.LoadUint64(sq)
	atomic.StoreUint64(sq, seq+1)
	it.b[4] = m.Model
	it.b[5] = m.Protocol
	binary.LittleEndian.PutUint16(it.b[6:8], m.Unit)
	binary.LittleEndian.PutUint64(it.b[16:24], m.Gen)
	binary.LittleEndian.PutUint32(it.b[24:28], m.Baud)
	binary.LittleEndian.PutUint16(it.b[28:30], m.Serial)
	it.b[30] = m.SerialLetter
	copy(it.b[32:32+contractPortSize], []byte(m.Port))
	binary.LittleEndian.PutUint64(it.b[64:72], uint64(m.LastSeen))
	atomic.StoreUint64(sq, seq+2)
	atomic.StoreUint32(it.state(), 1) // запись валидна только после первой публикации
}

// ListDoc строит список (для control-сокета) из бинарных записей.
func (s *Shm) ListDoc() StateDoc {
	d := StateDoc{Updated: time.Now().Unix()}
	for i := 0; i < s.slots; i++ {
		off := s.itemOff(i)
		if item, ok := decodeListItem(i, s.data[off:off+listItemSize]); ok {
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
