package main

import (
	"log"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/stat"
)

// Тайминги (arch §4/§10).
var (
	backoffLadder = []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	banDuration   = 5 * time.Minute
	scanInterval  = 5 * time.Second
	rescanPeriod  = 30 * time.Second
)

// source — открытый источник (serial/TCP), разделяемый слотами одной шины.
type source struct {
	desc   string
	serial string
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

// boState — backoff/бан источника.
type boState struct {
	step   int
	until  time.Time
	banned bool
}

type orchestrator struct {
	seg  *bmsShm
	sink *stat.Sink
	idr  *idRegistry

	mu        sync.Mutex
	devs      map[string]*device
	freeSlots []int
	busy      map[string]bool
	backoff   map[string]*boState
	activeSrc map[string]*source
	srcType   map[string]bmsType
	gws       gatewaysFile

	stop chan struct{}
}

func newOrchestrator(seg *bmsShm, sink *stat.Sink, idr *idRegistry, stop chan struct{}) *orchestrator {
	free := make([]int, 0, maxDevices)
	for i := 0; i < maxDevices; i++ {
		free = append(free, i)
	}
	return &orchestrator{
		seg: seg, sink: sink, idr: idr,
		devs: make(map[string]*device), freeSlots: free,
		busy: make(map[string]bool), backoff: make(map[string]*boState),
		activeSrc: make(map[string]*source), srcType: make(map[string]bmsType), stop: stop,
	}
}

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

var serialPatterns = []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/serial/by-id/*"}

// run — сканирование свободных источников + периодический рескан активных шин.
func (o *orchestrator) run() {
	scan := time.NewTicker(scanInterval)
	rescan := time.NewTicker(rescanPeriod)
	defer scan.Stop()
	defer rescan.Stop()
	o.scan()
	for {
		select {
		case <-o.stop:
			return
		case <-scan.C:
			o.scan()
		case <-rescan.C:
			o.rescan()
		}
	}
}

func (o *orchestrator) scan() {
	for _, path := range ports.ListPorts(serialPatterns) {
		desc := "serial:" + path
		if o.isBusy(desc) {
			continue
		}
		o.trySource(&source{desc: desc, serial: path})
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

// inBackoff сообщает, пропускать ли источник сейчас.
func (o *orchestrator) inBackoff(desc string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.backoff[desc]
	if !ok {
		return false
	}
	if time.Now().Before(st.until) {
		return true
	}
	return false
}

// trySource открывает источник и прогоняет probe-последовательность (§19).
func (o *orchestrator) trySource(src *source) {
	if o.inBackoff(src.desc) {
		return
	}
	if src.serial != "" {
		for _, baud := range unionSpeeds() {
			tr, err := openSerial(src.serial, baud)
			if err != nil {
				continue
			}
			if o.probeAndAct(src, tr) {
				return
			}
			tr.Close()
		}
		return
	}
	tr, err := openTCP(src.host, src.port)
	if err != nil {
		o.bumpBackoff(src.desc, false)
		return
	}
	if !o.probeAndAct(src, tr) {
		tr.Close()
	}
}

// probeAndAct возвращает true, если источних обработан (found/notMine).
func (o *orchestrator) probeAndAct(src *source, tr Transport) bool {
	matched, slots, sawNotMine := o.probeAll(tr)
	switch {
	case matched != nil:
		o.clearBackoff(src.desc)
		src.tr = tr
		o.addDevices(src, matched, slots)
		return true
	case sawNotMine:
		o.bumpBackoff(src.desc, true) // бан 5 мин
		return true
	default:
		o.bumpBackoff(src.desc, false)
		return false
	}
}

// probeAll вызывает probe-функции по порядку (arch §19).
func (o *orchestrator) probeAll(tr Transport) (bmsType, []probeSlot, bool) {
	sawNotMine := false
	for _, bt := range bmsTypesOrder {
		res, err := bt.Probe(tr)
		if err != nil {
			continue
		}
		if res.Kind == probeFound {
			return bt, res.Slots, false
		}
		if res.Kind == probeNotMine {
			sawNotMine = true
		}
	}
	return nil, nil, sawNotMine
}

// addDevices занимает слоты, регистрирует ID и запускает пулеры.
func (o *orchestrator) addDevices(src *source, bt bmsType, slots []probeSlot) {
	for _, s := range slots {
		o.mu.Lock()
		if len(o.freeSlots) == 0 || o.deviceByAddr(src.desc, s.Addr) != nil {
			o.mu.Unlock()
			continue
		}
		slot := o.freeSlots[0]
		o.freeSlots = o.freeSlots[1:]
		o.busy[src.desc] = true
		o.activeSrc[src.desc] = src
		o.srcType[src.desc] = bt
		o.mu.Unlock()

		id, _ := o.idr.assignID(s.Base, src.desc)
		gen := uint64(time.Now().UnixMilli())
		o.seg.allocSlot(slot, gen)
		d := &device{id: id, base: s.Base, src: src, bt: bt, addr: s.Addr, slot: slot, gen: gen}
		o.mu.Lock()
		o.devs[id] = d
		o.mu.Unlock()
		o.publishList(d)
		log.Printf("bms-reader: + %s (%s) на %s addr=%d slot=%d", id, bt.Name(), src.desc, s.Addr, slot)
		go o.poller(d)
	}
}

func (o *orchestrator) deviceByAddr(desc string, addr int) *device {
	for _, d := range o.devs {
		if d.src.desc == desc && d.addr == addr {
			return d
		}
	}
	return nil
}

// rescan — периодический перебор адресов активных шин (добавление новых слотов).
func (o *orchestrator) rescan() {
	o.mu.Lock()
	srcs := make([]*source, 0, len(o.activeSrc))
	types := make(map[string]bmsType, len(o.srcType))
	for desc, s := range o.activeSrc {
		srcs = append(srcs, s)
		types[desc] = o.srcType[desc]
	}
	o.mu.Unlock()

	for _, src := range srcs {
		bt := types[src.desc]
		if bt == nil {
			continue
		}
		src.mu.Lock()
		res, err := bt.Probe(src.tr)
		src.mu.Unlock()
		if err == nil && res.Kind == probeFound {
			o.addDevices(src, bt, res.Slots)
		}
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
		o.publishList(d)
	}
}

func (o *orchestrator) publishList(d *device) {
	rec := listRecord{
		ID: d.id, Base: d.base, Source: d.src.desc, TypeID: d.bt.TypeID(),
		State: 1, Framing: d.bt.Framing(), Gen: d.gen, LastSeen: uint64(time.Now().Unix()),
	}
	o.seg.publishList(d.slot, rec)
}

// loseDevice освобождает слот; если на источнике не осталось устройств — закрывает.
func (o *orchestrator) loseDevice(d *device) {
	o.mu.Lock()
	delete(o.devs, d.id)
	o.freeSlots = append(o.freeSlots, d.slot)
	remain := false
	for _, x := range o.devs {
		if x.src.desc == d.src.desc {
			remain = true
			break
		}
	}
	if !remain {
		delete(o.busy, d.src.desc)
		delete(o.activeSrc, d.src.desc)
		delete(o.srcType, d.src.desc)
	}
	o.mu.Unlock()
	o.seg.release(d.slot)
	if !remain {
		o.bumpBackoff(d.src.desc, false) // потеря устройства → отстойник
		d.src.tr.Close()
	}
}

func (o *orchestrator) clearBackoff(desc string) {
	o.mu.Lock()
	delete(o.backoff, desc)
	o.mu.Unlock()
}

// bumpBackoff повышает ступень отстойника; ban=true — бан 5 мин.
func (o *orchestrator) bumpBackoff(desc string, ban bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := o.backoff[desc]
	if st == nil {
		st = &boState{}
		o.backoff[desc] = st
	}
	if ban {
		st.banned = true
		st.until = time.Now().Add(banDuration)
		return
	}
	st.step++
	if st.step > len(backoffLadder) {
		st.step = len(backoffLadder)
	}
	st.until = time.Now().Add(backoffLadder[st.step-1])
}

// unionSpeeds — объединение скоростей всех типов (для открытия serial).
func unionSpeeds() []int {
	seen := map[int]bool{}
	var out []int
	for _, bt := range bmsTypesOrder {
		for _, s := range bt.Speeds() {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
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
