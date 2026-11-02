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

	o := newOrchestrator(&bmsShm{seg: seg}, nil, make(chan struct{}), nil)
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

// TestSlotReuseAfterLoss — вернувшееся на тот же источник/адрес устройство с тем
// же ID получает прежний слот (и новый gen), а не новый: на главной странице
// плитка оживает, а не дублируется.
func TestSlotReuseAfterLoss(t *testing.T) {
	o, _ := newTestOrch(t)
	src := "serial:/dev/ttyUSB0"
	st := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот не добавлен")
	}
	d := o.devBySlot[0]
	oldGen := d.gen
	o.loseDevice(d)
	if len(o.hints) != 1 {
		t.Fatalf("подсказок=%d, ожидалась 1", len(o.hints))
	}

	// Источник потерян целиком и найден заново: srcState новый, desc прежний.
	st2 := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st2, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот не добавлен после возврата")
	}
	nd, ok := o.devBySlot[0]
	if !ok {
		t.Fatalf("прежний слот 0 не занят: %+v", o.devBySlot)
	}
	if nd.gen == oldGen {
		t.Fatal("gen должен смениться при переиспользовании слота")
	}
	if len(o.devBySlot) != 1 {
		t.Fatalf("активных=%d, ожидалось 1", len(o.devBySlot))
	}
	if len(o.hints) != 0 {
		t.Fatalf("подсказка не снята: %+v", o.hints)
	}
	o.publishList(nd, 1)
	recs := o.seg.list()
	if len(recs) != 1 || recs[0].Gen != nd.gen {
		t.Fatalf("shm-список: %+v", recs)
	}
}

// TestSlotReuseRequiresSameKey — подсказка не переиспользуется для другого
// источника/адреса даже при совпадающем BASE (дубли BASE разрешены).
func TestSlotReuseRequiresSameKey(t *testing.T) {
	o, _ := newTestOrch(t)
	src := "serial:/dev/ttyUSB0"
	st := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот addr=0 не добавлен")
	}
	if !o.addSlot(st, probeSlot{Addr: 1, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот addr=1 не добавлен")
	}
	o.loseDevice(o.devBySlot[0])

	// Другой адрес с тем же BASE: подсказка addr=0 ему не принадлежит.
	if !o.addSlot(st, probeSlot{Addr: 2, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот addr=2 не добавлен")
	}
	if _, ok := o.devBySlot[0]; ok {
		t.Fatal("слот addr=0 выдан чужому адресу")
	}
	if d := o.devBySlot[2]; d == nil || d.addr != 2 {
		t.Fatalf("addr=2 должен получить новый слот 2: %+v", o.devBySlot)
	}
	if len(o.hints) != 1 {
		t.Fatalf("подсказка addr=0 должна сохраниться: %+v", o.hints)
	}

	// Возврат addr=0 забирает свою подсказку.
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот addr=0 не возвращён")
	}
	if d := o.devBySlot[0]; d == nil || d.addr != 0 {
		t.Fatalf("addr=0 не получил прежний слот: %+v", o.devBySlot)
	}
}

// TestSlotHintDroppedWhenTaken — если свободных слотов нет и подсказанный слот
// забирает другое устройство, возврат первого не перезаписывает чужой слот.
func TestSlotHintDroppedWhenTaken(t *testing.T) {
	o, _ := newTestOrch(t)
	src := "serial:/dev/ttyUSB0"
	st := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот не добавлен")
	}
	o.loseDevice(o.devBySlot[0])
	if h, ok := o.hints[slotKey{src: src, addr: 0, typeID: typeAntBMS, id: "AntBMS-DSP-160"}]; !ok || h != 0 {
		t.Fatalf("нет подсказки на слот 0: %+v", o.hints)
	}

	// Пул исчерпан: единственный свободный слот — подсказанный.
	o.mu.Lock()
	o.freeSlots = []int{0}
	o.mu.Unlock()

	stOther := &srcState{src: &source{desc: "serial:/dev/ttyUSB1", tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(stOther, probeSlot{Addr: 0, Base: "AntBMS-DSP-320"}) {
		t.Fatal("слот для другого устройства не выдан")
	}
	if len(o.hints) != 0 {
		t.Fatalf("подсказка на выданный слот не удалена: %+v", o.hints)
	}
	if d := o.devBySlot[0]; d == nil || d.id != "AntBMS-DSP-320" {
		t.Fatalf("слот 0 должен остаться за другим устройством: %+v", o.devBySlot)
	}

	// Освободился другой слот: возврат получает его, чужой слот не трогает.
	o.mu.Lock()
	o.freeSlots = append(o.freeSlots, 1)
	o.mu.Unlock()
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("возврат устройства не добавлен")
	}
	if d := o.devBySlot[1]; d == nil || d.id != "AntBMS-DSP-160" {
		t.Fatalf("возврат должен занять свободный слот 1: %+v", o.devBySlot)
	}
	if d := o.devBySlot[0]; d == nil || d.id != "AntBMS-DSP-320" {
		t.Fatalf("слот 0 перезаписан возвратом: %+v", o.devBySlot)
	}
}

// TestSlotReuseRequiresSameSource — при смене пути адаптера (ttyUSB0→ttyUSB1)
// подсказка не применяется: слот считается новым.
func TestSlotReuseRequiresSameSource(t *testing.T) {
	o, _ := newTestOrch(t)
	st := &srcState{src: &source{desc: "serial:/dev/ttyUSB0", tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот не добавлен")
	}
	o.loseDevice(o.devBySlot[0])

	stOther := &srcState{src: &source{desc: "serial:/dev/ttyUSB1", tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(stOther, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("слот для нового источника не выдан")
	}
	if _, ok := o.devBySlot[0]; ok {
		t.Fatalf("подсказка сработала для другого источника: %+v", o.devBySlot)
	}
	if d := o.devBySlot[1]; d == nil || d.st != stOther {
		t.Fatalf("ожидался новый слот 1: %+v", o.devBySlot)
	}
}

// TestSlotReuseRequiresSameType — совпадение src/addr/BASE при другом типе BMS
// не переиспользует слот.
func TestSlotReuseRequiresSameType(t *testing.T) {
	o, _ := newTestOrch(t)
	src := "serial:/dev/ttyUSB0"
	st := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: antBMS{}}
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "BMS-A"}) {
		t.Fatal("слот не добавлен")
	}
	o.loseDevice(o.devBySlot[0])

	stOther := &srcState{src: &source{desc: src, tr: &fakeTransport{}}, bt: enBMS{}}
	if !o.addSlot(stOther, probeSlot{Addr: 0, Base: "BMS-A"}) {
		t.Fatal("слот для другого типа не выдан")
	}
	if _, ok := o.devBySlot[0]; ok {
		t.Fatalf("подсказка сработала для другого типа: %+v", o.devBySlot)
	}
	if d := o.devBySlot[1]; d == nil || d.st.bt.TypeID() != typeEnBMS {
		t.Fatalf("ожидался новый слот 1 для другого типа: %+v", o.devBySlot)
	}
}
