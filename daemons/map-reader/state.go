package main

import (
	"fmt"
	"sync"
	"time"
)

// MapEntry — один МАП (порт+unit), занимающий слот в shm.
type MapEntry struct {
	key      string
	port     string
	unit     int
	proto    string
	baud     int
	info     *mapInfo
	slotIdx  int
	gen      uint64
	slot     *Slot
	cmd      chan *writeJob // очередь команд записи к пулеру этого порта
	syncWin  int64          // последнее проверенное окно синхронизации времени
	lastSeen time.Time
}

// Registry — активные МАП. Все обращения к полям записей — под mu.
type Registry struct {
	mu    sync.Mutex
	m     map[string]*MapEntry
	alloc *slotAlloc
	shm   *Shm
}

func newRegistry(alloc *slotAlloc, shm *Shm) *Registry {
	return &Registry{m: make(map[string]*MapEntry), alloc: alloc, shm: shm}
}

func keyOf(port string, unit int) string { return fmt.Sprintf("%s#%d", port, unit) }

// Add занимает слот и регистрирует МАП. false — если слотов нет.
// cmd — канал команд пулера порта; присваивается до публикации записи в реестре,
// чтобы команда записи не увидела запись с nil-каналом.
func (r *Registry) Add(port string, unit int, proto string, baud int, info *mapInfo, cmd chan *writeJob) (*MapEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := keyOf(port, unit)
	if e, ok := r.m[key]; ok {
		e.info = info
		e.lastSeen = time.Now()
		e.cmd = cmd
		return e, true
	}
	idx, gen, slot, ok := r.alloc.alloc()
	if !ok {
		return nil, false
	}
	e := &MapEntry{
		key: key, port: port, unit: unit, proto: proto, baud: baud,
		info: info, slotIdx: idx, gen: gen, slot: slot, cmd: cmd, lastSeen: time.Now(),
	}
	r.m[key] = e
	return e, true
}

func (r *Registry) Remove(e *MapEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.m[e.key]; ok && cur == e {
		r.alloc.free(e.slotIdx)
		delete(r.m, e.key)
	}
}

// Touch обновляет lastSeen и публикует запись списка.
func (r *Registry) Touch(e *MapEntry) {
	r.mu.Lock()
	e.lastSeen = time.Now()
	meta := listMeta{
		Model:        e.info.Model[0],
		Protocol:     protoCode(e.proto),
		Unit:         uint16(e.unit),
		Gen:          e.gen,
		Baud:         uint32(e.baud),
		Serial:       e.info.Serial,
		SerialLetter: e.info.SerialLetter,
		Port:         e.port,
		LastSeen:     e.lastSeen.Unix(),
	}
	r.mu.Unlock()
	r.shm.Item(e.slotIdx).Publish(meta)
}

// Model возвращает модель МАП (потокобезопасно).
func (r *Registry) Model(e *MapEntry) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return e.info.Model
}

// Meta возвращает мету контракта (потокобезопасно).
func (r *Registry) Meta(e *MapEntry) contractMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return contractMeta{
		Port:     e.port,
		Model:    e.info.Model,
		Protocol: e.proto,
		Unit:     e.unit,
		Gen:      e.gen,
		LastSeen: e.lastSeen,
	}
}

func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}

// Find возвращает запись по (id слота, gen) — для маршрутизации команды записи.
func (r *Registry) Find(id int, gen uint64) (*MapEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.m {
		if e.slotIdx == id && e.gen == gen {
			return e, true
		}
	}
	return nil, false
}

func (r *Registry) Has(port string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.m {
		if e.port == port {
			return true
		}
	}
	return false
}

// ReleaseAll освобождает все слоты (при остановке).
func (r *Registry) ReleaseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.m {
		r.alloc.free(e.slotIdx)
	}
	r.m = make(map[string]*MapEntry)
}

// MapView — запись map-list для API.
type MapView struct {
	ID           int    `json:"id"`
	Gen          uint64 `json:"gen"`
	Port         string `json:"port"`
	Unit         int    `json:"unit"`
	Protocol     string `json:"protocol"`
	Baud         int    `json:"baud"`
	Model        string `json:"model"`
	Serial       uint16 `json:"serial"`
	SerialLetter string `json:"serial_letter"`
	State        string `json:"state"`
	LastSeen     int64  `json:"last_seen"`
}

type StateDoc struct {
	Updated int64     `json:"updated"`
	Count   int       `json:"count"`
	Maps    []MapView `json:"maps"`
}

func (r *Registry) doc() StateDoc {
	return r.shm.ListDoc()
}
