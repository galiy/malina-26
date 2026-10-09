package main

import "sync"

// slotAlloc — аллокатор слотов в демоне (один писатель-процесс).
type slotAlloc struct {
	mu   sync.Mutex
	used [shmSlots]bool
	shm  *Shm
}

func newSlotAlloc(shm *Shm) *slotAlloc {
	return &slotAlloc{shm: shm}
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
		g := sl.Prepare()
		a.shm.Item(i).reset()
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

func (a *slotAlloc) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, u := range a.used {
		if u {
			n++
		}
	}
	return n
}
