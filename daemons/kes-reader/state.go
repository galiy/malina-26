package main

import (
	"sync"
	"time"
)

// kesEntry — один КЭС (порт), занимающий слот в shm.
type kesEntry struct {
	key      string
	port     string
	baud     int
	info     *kesInfo
	slotIdx  int
	gen      uint64
	slot     *Slot
	cmd      chan *writeJob
	lastSeen time.Time
}

// Registry — активные КЭС. Все обращения к полям записей — под mu.
type Registry struct {
	mu    sync.Mutex
	m     map[string]*kesEntry
	alloc *slotAlloc
	shm   *Shm
}

func newRegistry(alloc *slotAlloc, shm *Shm) *Registry {
	return &Registry{m: make(map[string]*kesEntry), alloc: alloc, shm: shm}
}

func (r *Registry) Add(port string, baud int, info *kesInfo, cmd chan *writeJob) (*kesEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.m[port]; ok {
		e.info = info
		e.lastSeen = time.Now()
		e.cmd = cmd
		return e, true
	}
	idx, gen, slot, ok := r.alloc.alloc()
	if !ok {
		return nil, false
	}
	e := &kesEntry{key: port, port: port, baud: baud, info: info, slotIdx: idx, gen: gen, slot: slot, cmd: cmd, lastSeen: time.Now()}
	r.m[port] = e
	return e, true
}

func (r *Registry) Remove(e *kesEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.m[e.key]; ok && cur == e {
		r.alloc.free(e.slotIdx)
		delete(r.m, e.key)
	}
}

// Touch обновляет lastSeen и публикует запись kes-list.
func (r *Registry) Touch(e *kesEntry) {
	r.mu.Lock()
	e.lastSeen = time.Now()
	meta := listMeta{
		Serial:   r.effectiveSerial(e),
		Baud:     uint32(e.baud),
		Gen:      e.gen,
		Port:     e.port,
		LastSeen: e.lastSeen.Unix(),
	}
	r.mu.Unlock()
	publishItem(r.shm.Item(e.slotIdx), meta)
}

// SetInfo обновляет идентификацию записи под мьютексом (для горутины опроса).
func (r *Registry) SetInfo(e *kesEntry, info *kesInfo) {
	r.mu.Lock()
	e.info = info
	r.mu.Unlock()
}

// CmdOf возвращает канал команд записи записи под мьютексом.
func (r *Registry) CmdOf(e *kesEntry) chan *writeJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return e.cmd
}

// effectiveSerial — серийник с подстановкой idx+1 при нулевом значении (legacy, §1b).
func (r *Registry) effectiveSerial(e *kesEntry) uint16 {
	if e.info != nil && e.info.Serial != 0 {
		return e.info.Serial
	}
	return uint16(e.slotIdx + 1)
}

// Meta возвращает мету контракта (потокобезопасно).
func (r *Registry) Meta(e *kesEntry) contractMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return contractMeta{Port: e.port, Serial: r.effectiveSerial(e), Gen: e.gen, LastSeen: e.lastSeen}
}

func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}

func (r *Registry) Has(port string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.m[port]
	return ok
}

func (r *Registry) Find(id int, gen uint64) (*kesEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.m {
		if e.slotIdx == id && e.gen == gen {
			return e, true
		}
	}
	return nil, false
}

func (r *Registry) ReleaseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.m {
		r.alloc.free(e.slotIdx)
	}
	r.m = make(map[string]*kesEntry)
}

func (r *Registry) doc() StateDoc { return listDoc(r.shm) }

// slotAlloc — аллокатор слотов (один писатель-процесс).
type slotAlloc struct {
	mu      sync.Mutex
	used    [kesSlots]bool
	lastGen uint64
	shm     *Shm
}

func newSlotAlloc(shm *Shm) *slotAlloc { return &slotAlloc{shm: shm} }

func (a *slotAlloc) nextGen() uint64 {
	g := uint64(time.Now().UnixMilli())
	if g <= a.lastGen {
		g = a.lastGen + 1
	}
	a.lastGen = g
	return g
}

func (a *slotAlloc) alloc() (int, uint64, *Slot, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := 0; i < kesSlots; i++ {
		if a.used[i] {
			continue
		}
		sl := a.shm.Slot(i)
		g := a.nextGen()
		sl.PrepareGen(g)
		a.shm.Item(i).Clear()
		a.used[i] = true
		return i, g, sl, true
	}
	return 0, 0, nil, false
}

func (a *slotAlloc) free(i int) {
	if i < 0 || i >= kesSlots {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.used[i] {
		return
	}
	a.shm.Slot(i).Release()
	a.shm.Item(i).Release()
	a.used[i] = false
}
