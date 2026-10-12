package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

func newRNG() *rand.Rand { return rand.New(rand.NewSource(time.Now().UnixNano())) }

// newSerial возвращает свободный ненулевой серийник (до 65534 попыток).
func newSerial(rnd *rand.Rand, used map[uint16]bool) uint16 {
	for i := 0; i < 0xFFFE; i++ {
		sn := uint16(1 + rnd.Intn(0xFFFE))
		if sn != 0 && !used[sn] {
			return sn
		}
	}
	return 0
}

// Хранилище состояния эмулятора: kes-emu.json с коллекцией объектов.
//
// Объект: серийник + карта памяти HEX-строкой — весь образ:
// EEPROM (0x400) ++ RAM (kesImageSize). При старте: сперва карта, затем константы.
// Пишется при создании объекта и при разрешённой записи (свободная EEPROM / команда реле).

const storeName = "kes-emu.json"

// confirmedEEPROM — подтверждённые ячейки EEPROM (запись запрещена).
var confirmedEEPROM = map[uint16]bool{
	0x0001: true, // Pow
	0x0002: true, // Ver ПО
	0x0005: true, // мощность модели
	0x000C: true, // Pic + Модель
	0x000D: true, // Net
	0x009B: true, // напряжение модели
	0x02C9: true, // серийник мл.
	0x02CA: true, // серийник ст.
}

// relayStateAddr — ячейка состояния реле (RAM-адрес карты).
const relayStateAddr = 0x3E3

type devRecord struct {
	Serial uint16 `json:"serial"`
	// Mem — вся память HEX-строкой: EEPROM 0x000..0x3FF, затем RAM 0x000..,
	// по 2 символа на байт (memImageBytes·2 символов).
	Mem string `json:"mem"`
}

type storeFile struct {
	Version int         `json:"version"`
	Devices []devRecord `json:"devices"`
}

// Store — персистентное хранилище (один писатель-процесс).
type Store struct {
	mu   sync.Mutex
	path string
	f    storeFile
	img  []*MemImage // образы памяти (параллельно f.Devices)
}

// storagePath выбирает каталог: override, иначе /malina, /settings, иначе /tmp.
func storagePath(dir string) string {
	if dir != "" {
		return filepath.Join(dir, storeName)
	}
	for _, d := range []string{"/malina", "/settings"} {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return filepath.Join(d, storeName)
		}
	}
	return filepath.Join("/tmp", storeName)
}

// openStore читает/создаёт файл и гарантирует не менее n объектов.
func openStore(dir string, n int) (*Store, error) { return openStorePath(storagePath(dir), n) }

func openStorePath(path string, n int) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(s.path)
	existed := err == nil && len(b) > 0
	if existed {
		_ = json.Unmarshal(b, &s.f)
	}
	s.f.Version = 1
	rnd := newRNG()
	// Серийники: гарантируем уникальность (в т.ч. чиним дубли/нули из файла).
	used := map[uint16]bool{}
	fixed := 0
	for i := range s.f.Devices {
		sn := s.f.Devices[i].Serial
		if sn == 0 || used[sn] {
			sn = newSerial(rnd, used)
			s.f.Devices[i].Serial = sn
			fixed++
		}
		used[sn] = true
	}
	added := 0
	for len(s.f.Devices) < n { // новый серийник генерируется ТОЛЬКО здесь
		sn := newSerial(rnd, used)
		used[sn] = true
		s.f.Devices = append(s.f.Devices, devRecord{Serial: sn})
		added++
	}
	s.img = make([]*MemImage, len(s.f.Devices))
	for i := range s.f.Devices {
		s.img[i] = NewMemImage(s.f.Devices[i].Mem)
	}
	if !existed || added > 0 || fixed > 0 {
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) save() error {
	for i := range s.f.Devices {
		if i < len(s.img) {
			s.f.Devices[i].Mem = s.img[i].Hex()
		}
	}
	b, _ := json.MarshalIndent(s.f, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Record возвращает копию записи i.
func (s *Store) Record(i int) devRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.f.Devices) {
		return devRecord{}
	}
	d := s.f.Devices[i]
	if i < len(s.img) {
		d.Mem = s.img[i].Hex()
	}
	return d
}

// Image — карта памяти устройства (образ).
func (s *Store) Image(i int) *MemImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.img) {
		return NewMemImage("")
	}
	return s.img[i]
}

// Save сохраняет карту памяти целиком как есть.
func (s *Store) Save() {
	s.mu.Lock()
	_ = s.save()
	s.mu.Unlock()
}

// MemByte — байт карты по смещению off.
func (r devRecord) MemByte(off uint16) byte {
	i := int(off) * 2
	if i+2 > len(r.Mem) {
		return 0
	}
	v, err := strconv.ParseUint(r.Mem[i:i+2], 16, 8)
	if err != nil {
		return 0
	}
	return byte(v)
}
