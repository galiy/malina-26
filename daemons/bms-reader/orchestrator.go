package main

import (
	"log"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/stat"
)

// source — открытый источник (serial/TCP), разделяемый слотами одной шины.
type source struct {
	desc   string
	serial string
	baud   int
	host   string
	port   int
	tr     Transport
	mu     sync.Mutex // сериализация обмена на шине
}

type device struct {
	id   string
	base string
	src  *source
	bt   bmsType
	addr int
	slot int
	gen  uint64
}

type orchestrator struct {
	seg  *bmsShm
	sink *stat.Sink
	idr  *idRegistry

	mu        sync.Mutex
	devs      map[string]*device
	freeSlots []int
	busy      map[string]bool // занятые источники (desc)
	skipUntil map[string]time.Time
	gws       gatewaysFile

	stop chan struct{}
}

// setGateways обновляет список шлюзов (reload).
func (o *orchestrator) setGateways(g gatewaysFile) {
	o.mu.Lock()
	o.gws = g
	o.mu.Unlock()
}

func (o *orchestrator) gateways() gatewaysFile {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.gws
}

func newOrchestrator(seg *bmsShm, sink *stat.Sink, idr *idRegistry, stop chan struct{}) *orchestrator {
	free := make([]int, 0, maxDevices)
	for i := 0; i < maxDevices; i++ {
		free = append(free, i)
	}
	return &orchestrator{
		seg: seg, sink: sink, idr: idr,
		devs: make(map[string]*device), freeSlots: free,
		busy: make(map[string]bool), skipUntil: make(map[string]time.Time), stop: stop,
	}
}

var serialPatterns = []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/serial/by-id/*"}

// run — цикл сканирования источников.
func (o *orchestrator) run() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	o.scan()
	for {
		select {
		case <-o.stop:
			return
		case <-tick.C:
			o.scan()
		}
	}
}

func (o *orchestrator) scan() {
	for _, path := range ports.ListPorts(serialPatterns) {
		if o.isBusy("serial:" + path) {
			continue
		}
		o.trySource(&source{desc: "serial:" + path, serial: path, baud: 19200})
	}
	for _, g := range o.gateways().Gateways {
		desc := "tcp:" + g.Host + ":" + itoa(g.Port)
		if o.isBusy(desc) {
			continue
		}
		o.trySource(&source{desc: desc, host: g.Host, port: g.Port})
	}
}

func (o *orchestrator) isBusy(desc string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.busy[desc]
}

// trySource открывает источник и прогоняет последовательность probe (arch §19).
func (o *orchestrator) trySource(src *source) {
	if t, ok := o.skip(src.desc); ok {
		_ = t
		return
	}
	var tr Transport
	var err error
	if src.serial != "" {
		tr, err = openSerial(src.serial, src.baud)
	} else {
		tr, err = openTCP(src.host, src.port)
	}
	if err != nil {
		return
	}
	src.tr = tr

	for _, bt := range bmsTypesOrder {
		res, err := bt.Probe(tr)
		if err != nil {
			continue
		}
		switch res.Kind {
		case probeFound:
			o.addDevices(src, bt, res.Slots)
			return
		case probeNotMine:
			// другое семейство/диалект мог бы опознать иной probe — продолжаем
			continue
		default:
			continue
		}
	}
	// не опознали: закрываем и пробуем позже
	tr.Close()
}

// addDevices занимает слоты, регистрирует ID и запускает пулеры.
func (o *orchestrator) addDevices(src *source, bt bmsType, slots []probeSlot) {
	for _, s := range slots {
		o.mu.Lock()
		if len(o.freeSlots) == 0 {
			o.mu.Unlock()
			src.tr.Close()
			return
		}
		slot := o.freeSlots[0]
		o.freeSlots = o.freeSlots[1:]
		o.busy[src.desc] = true
		o.mu.Unlock()

		id, _ := o.idr.assignID(s.Base, src.desc)
		gen := uint64(time.Now().UnixMilli())
		o.seg.allocSlot(slot, gen)
		d := &device{id: id, base: s.Base, src: src, bt: bt, addr: s.Addr, slot: slot, gen: gen}
		o.mu.Lock()
		o.devs[id] = d
		o.mu.Unlock()
		o.publishList(d, 1)
		log.Printf("bms-reader: + %s (%s) на %s addr=%d slot=%d", id, bt.Name(), src.desc, s.Addr, slot)
		go o.poller(d)
	}
}

// poller — цикл опроса одного слота (1 с).
func (o *orchestrator) poller(d *device) {
	for {
		select {
		case <-o.stop:
			return
		case <-time.After(1 * time.Second):
		}
		d.src.mu.Lock()
		tel, err := d.bt.Poll(d.src.tr, d.addr)
		d.src.mu.Unlock()
		if err != nil {
			if err != errTimeout {
				log.Printf("bms-reader: - %s: %v", d.id, err)
			}
			o.loseDevice(d)
			return
		}
		o.seg.publishTelemetry(d.slot, bmscontract.Encode(&tel))
		publishRaw(o.sink, d.id, tel)
		o.publishList(d, 1)
	}
}

func (o *orchestrator) publishList(d *device, state uint8) {
	rec := listRecord{
		ID: d.id, Base: d.base, Source: d.src.desc, TypeID: d.bt.TypeID(),
		State: state, Framing: d.bt.Framing(), Gen: d.gen, LastSeen: uint64(time.Now().Unix()),
	}
	o.seg.publishList(d.slot, rec)
}

// loseDevice освобождает слот и возвращает источник в пул.
func (o *orchestrator) loseDevice(d *device) {
	o.mu.Lock()
	delete(o.devs, d.id)
	o.freeSlots = append(o.freeSlots, d.slot)
	// источник больше не занят (транспорт будет переоткрыт при след. скане)
	delete(o.busy, d.src.desc)
	o.mu.Unlock()
	o.seg.release(d.slot)
	d.src.tr.Close()
}

func (o *orchestrator) skip(desc string) (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	t, ok := o.skipUntil[desc]
	if !ok {
		return time.Time{}, false
	}
	if time.Now().After(t) {
		delete(o.skipUntil, desc)
		return time.Time{}, false
	}
	return t, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
