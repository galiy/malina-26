package main

import (
	"path/filepath"
	"testing"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// TestDuplicateBaseSlots — два устройства с одинаковым BASE (одинаковый ID)
// живут в разных слотах; потеря одного не освобождает слот другого.
func TestDuplicateBaseSlots(t *testing.T) {
	seg, err := shm.Open(filepath.Join(t.TempDir(), "bms-state"), bmsLayout())
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()

	o := newOrchestrator(&bmsShm{seg: seg}, nil, make(chan struct{}))
	st := &srcState{
		src: &source{desc: "serial:/dev/ttyUSB0", tr: &fakeTransport{}},
		bt:  antBMS{},
	}

	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот 0 не добавлен")
	}
	if !o.addSlot(st, probeSlot{Addr: 1, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот 1 не добавлен")
	}
	if len(o.devBySlot) != 2 {
		t.Fatalf("активных=%d, ожидалось 2", len(o.devBySlot))
	}
	d0, ok0 := o.devBySlot[0]
	d1, ok1 := o.devBySlot[1]
	if !ok0 || !ok1 {
		t.Fatalf("слоты: %v %v", ok0, ok1)
	}
	if d0.id != d1.id {
		t.Fatalf("ID должны совпадать: %q != %q", d0.id, d1.id)
	}

	// Обе записи в shm: одинаковый ID/BASE, разные слоты/gen.
	o.publishList(d0, 1)
	o.publishList(d1, 1)
	recs := o.seg.list()
	if len(recs) != 2 {
		t.Fatalf("записей в shm=%d, ожидалось 2", len(recs))
	}
	if recs[0].ID != recs[1].ID || recs[0].Base != recs[1].Base {
		t.Fatalf("ID/BASE должны совпадать: %+v", recs)
	}
	if recs[0].Gen == recs[1].Gen {
		t.Fatalf("gen должны различаться: %+v", recs)
	}

	o.loseDevice(d0)
	o.loseDevice(d0) // повторная потеря идемпотентна
	if _, ok := o.devBySlot[0]; ok {
		t.Fatal("слот 0 не освобождён")
	}
	if _, ok := o.devBySlot[1]; !ok {
		t.Fatal("слот 1 потерян вместе с дублем")
	}
	if len(st.devs) != 1 {
		t.Fatalf("devs=%d, ожидалось 1", len(st.devs))
	}
	if len(o.freeSlots) != maxDevices-1 {
		t.Fatalf("freeSlots=%d, ожидалось %d", len(o.freeSlots), maxDevices-1)
	}

	// Heartbeat от потерянного устройства не должен «воскресить» слот.
	o.handleEvent(devEvent{d: d0})
	if len(o.seg.list()) != 1 {
		t.Fatalf("после heartbeat потерянного устройства записей=%d, ожидалось 1", len(o.seg.list()))
	}

	// Телеметрия со старым gen не должна попасть в переиспользованный слот.
	if o.seg.publishTelemetry(d0.slot, d0.gen, []byte{1, 2, 3}) {
		t.Fatal("телеметрия старого устройства принята после потери")
	}
	o.seg.allocSlot(d0.slot, 999)
	if o.seg.publishTelemetry(d0.slot, d0.gen, []byte{1, 2, 3}) {
		t.Fatal("телеметрия старого gen принята после перевыдачи слота")
	}
	if !o.seg.publishTelemetry(d0.slot, 999, []byte{1, 2, 3}) {
		t.Fatal("телеметрия нового gen не принята")
	}

	o.loseDevice(d1)
	if len(o.devBySlot) != 0 {
		t.Fatalf("активных=%d, ожидалось 0", len(o.devBySlot))
	}
}
