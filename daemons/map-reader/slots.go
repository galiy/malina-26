package main

import (
	"sync"
	"time"
)

// slotAlloc — аллокатор слотов в демоне (один писатель-процесс).
type slotAlloc struct {
	mu      sync.Mutex
	used    [shmSlots]bool
	lastGen uint64
	shm     *Shm
}

func newSlotAlloc(shm *Shm) *slotAlloc {
	return &slotAlloc{shm: shm}
}

// nextGen — gen = текущий timestamp (мс, помещается в JS Number), монотонный.
func (a *slotAlloc) nextGen() uint64 {
	g := uint64(time.Now().UnixMilli())
	if g <= a.lastGen {
		g = a.lastGen + 1
	}
	a.lastGen = g
	return g
}

// alloc занимает наименьший свободный слот, возвращает индекс, gen и Slot.
func (a *slotAlloc) alloc() (int, uint64, *Slot, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := 0; i < shmSlots; i++ {
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
	if i < 0 || i >= shmSlots {
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
