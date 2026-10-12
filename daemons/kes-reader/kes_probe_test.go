package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// TestProbeKesPortPTY — проба порта на реальном PTY с виртуальным КЭС (_Device==5):
// ожидаем найденное устройство на какой-то скорости из kesSpeeds.
func TestProbeKesPortPTY(t *testing.T) {
	f := &fakeKes{}
	f.img[0x0000] = 5 // _Device = КЭС

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	info, baud, err := probeKesPort(p, 200*time.Millisecond, 200*time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatalf("probeKesPort: %v", err)
	}
	if info == nil || info.Device != 5 {
		t.Fatalf("info=%+v, ждали Device=5", info)
	}
	if baud == 0 {
		t.Fatal("baud=0, ждали ненулевую скорость")
	}
}

// TestProbeKesPortForeign — чужое MicroArt (_Device==3, МАП): пробa должна быстро
// вернуться с errForeignMap, чтобы порт сразу отдавали соседнему демону.
func TestProbeKesPortForeign(t *testing.T) {
	f := &fakeKes{}
	f.img[0x0000] = 3 // _Device = МАП (чужое)

	master, slave := openPTYPair(t)
	defer master.Close()
	stop := make(chan struct{})
	defer close(stop)
	go f.serve(master, stop)

	p, err := transport.Open(slave)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	defer p.Close()

	_, _, err = probeKesPort(p, 200*time.Millisecond, 200*time.Millisecond, 2*time.Second)
	if !errors.Is(err, errForeignMap) {
		t.Fatalf("err=%v, ждали errForeignMap", err)
	}
}

// TestSlotAlloc — аллокатор слотов: выделение, возрастание gen, освобождение и
// повторное использование (генерация/освобождение, gap из ревью).
func TestSlotAlloc(t *testing.T) {
	shm, err := shmOpen(filepath.Join(t.TempDir(), "kes-shm"), kesSlots)
	if err != nil {
		t.Fatalf("shmOpen: %v", err)
	}
	defer shm.Close()

	a := newSlotAlloc(shm)
	var gens []uint64
	for i := 0; i < 3; i++ {
		_, gen, _, ok := a.alloc()
		if !ok {
			t.Fatalf("alloc #%d не удался", i)
		}
		gens = append(gens, gen)
	}
	// Gen строго возрастает.
	for i := 1; i < len(gens); i++ {
		if gens[i] <= gens[i-1] {
			t.Fatalf("gen не возрастает: %v", gens)
		}
	}
	if a.used[0] != true || a.used[1] != true || a.used[2] != true {
		t.Fatal("слоты 0..2 должны быть заняты")
	}
	// Освобождение и переиспользование.
	a.free(1)
	if a.used[1] {
		t.Fatal("слот 1 должен освободиться")
	}
	idx, _, _, ok := a.alloc()
	if !ok {
		t.Fatal("realloc после free не удался")
	}
	if idx != 1 {
		t.Fatalf("переиспользованный слот=%d, ждали 1", idx)
	}
	// Освобождаем всё и проверяем исчерпание: заполняем все 32 слота, следующий alloc
	// должен отказать.
	for i := 0; i < kesSlots; i++ {
		a.free(i)
	}
	for i := 0; i < kesSlots; i++ {
		if _, _, _, ok := a.alloc(); !ok {
			t.Fatalf("не влезли все %d слотов (осталось свободных)", kesSlots)
		}
	}
	if _, _, _, ok := a.alloc(); ok {
		t.Fatal("alloc при полностью занятых слотах должен вернуть ok=false")
	}
	// Невалидный индекс free — без паники.
	a.free(-1)
	a.free(kesSlots)
}
