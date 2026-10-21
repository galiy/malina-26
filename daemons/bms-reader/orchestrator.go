package main

import (
	"os"
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
	scanIdle      = 10 * time.Second // нет активных устройств
	scanActive    = time.Minute      // есть активные
	rescanPeriod  = 30 * time.Second
)

// source — открытый источник (serial/TCP), транспорт разделяется слотами шины.
type source struct {
	desc   string
	serial string
	host   string
	port   int
	tr     Transport
	mu     sync.Mutex // сериализация обмена на шине
}

// srcState — состояние шины: тип + активные слоты + один пулер-поток.
type srcState struct {
	src  *source
	bt   bmsType
	mu   sync.Mutex
	devs []*device
}

type device struct {
	id   string
	base string
	addr int
	slot int
	gen  uint64
	miss int // подряд неудачных опросов (только goroutine этого источника)
	st   *srcState
}

// boState — backoff/бан источника.
type boState struct {
	step  int
	until time.Time
}

type devEvent struct {
	d    *device
	lost bool
}

type orchestrator struct {
	seg  *bmsShm
	sink *stat.Sink

	mu        sync.Mutex
	states    map[string]*srcState
	devBySlot map[int]*device // активные устройства: слот уникален, ID — нет
	freeSlots []int
	backoff   map[string]*boState
	gws       gatewaysFile

	genMu sync.Mutex
	gen   uint64

	events chan devEvent

	stop   chan struct{}
	pollWg sync.WaitGroup // пулеры источников (для джойна при остановке)
}

// stopped — закрыт ли канал остановки.
func (o *orchestrator) stopped() bool {
	select {
	case <-o.stop:
		return true
	default:
		return false
	}
}

// waitPollers ждёт завершения всех пулеров источников.
func (o *orchestrator) waitPollers() { o.pollWg.Wait() }

// closeAll закрывает транспорты всех активных источников (при остановке) и
// очищает карты состояния — иначе fd/порт остаются удержанными.
func (o *orchestrator) closeAll() {
	o.mu.Lock()
	sts := make([]*srcState, 0, len(o.states))
	for _, st := range o.states {
		sts = append(sts, st)
	}
	o.states = make(map[string]*srcState)
	o.devBySlot = make(map[int]*device)
	// freeSlots не пересобираем: после closeAll оркестратор не переиспользуется
	// (main создаёт новый), а сброс мог бы раздать слоты «зависшему» run().
	o.mu.Unlock()
	for _, st := range sts {
		if st.src != nil && st.src.tr != nil {
			_ = st.src.tr.Close()
		}
	}
}

func newOrchestrator(seg *bmsShm, sink *stat.Sink, stop chan struct{}) *orchestrator {
	free := make([]int, 0, maxDevices)
	for i := 0; i < maxDevices; i++ {
		free = append(free, i)
	}
	return &orchestrator{
		seg: seg, sink: sink,
		states: make(map[string]*srcState), devBySlot: make(map[int]*device),
		freeSlots: free, backoff: make(map[string]*boState),
		events: make(chan devEvent, 64), stop: stop,
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

// run — единственный оркестраторский цикл: скан/рескан/события и ЕДИНСТВЕННЫЙ
// писатель shm-списка (arch §12).
//
// scan/rescan выполняются синхронно в этом цикле (probe/open могут длиться секунды),
// на это время events (lastSeen/lost) ждут в буфере. Это осознанно: сохраняется
// инвариант «один писатель списка»; корректность при задержке обеспечена
// идемпотентностью loseDevice (Р2-1). Р2-5 принято как известное ограничение.
func (o *orchestrator) run() {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	lastScan := time.Time{}
	o.scan()
	lastScan = time.Now()
	var lastRescan time.Time
	for {
		select {
		case <-o.stop:
			return
		case <-tick.C:
			now := time.Now()
			if now.Sub(lastRescan) >= rescanPeriod {
				o.rescan()
				lastRescan = now
			}
			if now.Sub(lastScan) >= o.scanInterval() {
				o.scan()
				lastScan = now
			}
		case ev := <-o.events:
			o.handleEvent(ev)
		}
	}
}

// scanInterval — 10 с без активных устройств, 1 мин при активных (arch §10).
func (o *orchestrator) scanInterval() time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.devBySlot) > 0 {
		return scanActive
	}
	return scanIdle
}

// busyRdevs — занятые ЧУЖИМИ процессами символьные устройства (arch §1/§7).
func busyRdevs() map[uint64]bool {
	return ports.BusyRdevs(os.Getpid())
}

func (o *orchestrator) scan() {
	if o.stopped() {
		return
	}
	busy := busyRdevs()
	for _, path := range ports.ListPorts(serialPatterns) {
		if o.stopped() {
			return
		}
		desc := "serial:" + path
		if o.hasState(desc) {
			continue
		}
		if fi, err := os.Stat(path); err == nil && busy[ports.RdevOf(fi)] {
			continue // порт держит map/kes — не трогаем
		}
		o.trySource(&source{desc: desc, serial: path})
	}
	for _, g := range o.gateways().Gateways {
		if o.stopped() {
			return
		}
		desc := "tcp:" + g.Host + ":" + itoa(g.Port)
		if o.hasState(desc) {
			continue
		}
		o.trySource(&source{desc: desc, host: g.Host, port: g.Port})
	}
}

func (o *orchestrator) hasState(desc string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.states[desc]
	return ok
}

// trySource — открыть источник, прогнать probe-последовательность (§19).
func (o *orchestrator) trySource(src *source) {
	if o.stopped() || o.inBackoff(src.desc) {
		return
	}
	if src.serial != "" {
		for _, baud := range unionSpeeds() {
			tr, err := openSerial(src.serial, baud)
			if err != nil {
				continue
			}
			if o.stopped() {
				tr.Close()
				return
			}
			if o.probeAndAct(src, tr) {
				// Источник принят (matched) — транспорт остаётся открытым у пулера.
				// Если услышали ЧУЖОЙ трафик (sawNotMine), источник не зарегистрирован:
				// порт не наш, fd не держим (иначе блокируем другие демоны).
				if !o.hasState(src.desc) {
					tr.Close()
				}
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
	if o.probeAndAct(src, tr) && o.hasState(src.desc) {
		return // источник принят — транспорт остаётся открытым
	}
	tr.Close()
}

func (o *orchestrator) probeAndAct(src *source, tr Transport) bool {
	if o.stopped() {
		return false
	}
	matched, slots, sawNotMine := o.probeAll(tr)
	if o.stopped() {
		return false
	}
	switch {
	case matched != nil:
		o.clearBackoff(src.desc)
		src.tr = tr
		o.addSource(src, matched, slots)
		return true
	case sawNotMine:
		o.bumpBackoff(src.desc, true)
		return true
	default:
		o.bumpBackoff(src.desc, false)
		return false
	}
}

func (o *orchestrator) probeAll(tr Transport) (bmsType, []probeSlot, bool) {
	sawNotMine := false
	for _, bt := range bmsTypesOrder {
		if o.stopped() {
			return nil, nil, false
		}
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

// addSource создаёт состояние шины и (при необходимости) пулер-поток; слоты
// добавляются в него. Вызывается из оркестраторского цикла.
func (o *orchestrator) addSource(src *source, bt bmsType, slots []probeSlot) {
	if o.stopped() {
		return
	}
	o.mu.Lock()
	st, ok := o.states[src.desc]
	if !ok {
		st = &srcState{src: src, bt: bt}
		o.states[src.desc] = st
	}
	o.mu.Unlock()

	added := false
	for _, s := range slots {
		if o.addSlot(st, s) {
			added = true
		}
	}
	if !ok && !added {
		// Свободных слотов нет: не оставляем пустое состояние без пулера,
		// иначе hasState навсегда блокирует повторный scan, а rescan добавит
		// слоты в состояние, которое никто не опрашивает.
		st.mu.Lock()
		empty := len(st.devs) == 0
		st.mu.Unlock()
		o.mu.Lock()
		if cur, exists := o.states[src.desc]; exists && cur == st && empty {
			delete(o.states, src.desc)
		}
		o.mu.Unlock()
		return
	}
	if added && !ok {
		o.pollWg.Add(1)
		go func() {
			defer o.pollWg.Done()
			o.sourcePoller(st)
		}()
	}
	// публикуем список для добавленных устройств (оркестратор — единственный писатель)
	for _, d := range o.snapshot(st) {
		o.publishList(d, 1)
	}
}

// addSlot занимает слот и регистрирует устройство.
//
// Ключ активных устройств — слот, а не ID: несколько BMS могут иметь одинаковый
// BASE/ID (принятый риск), но слот всегда уникален.
func (o *orchestrator) addSlot(st *srcState, s probeSlot) bool {
	st.mu.Lock()
	for _, x := range st.devs {
		if x.addr == s.Addr {
			st.mu.Unlock()
			return false
		}
	}
	st.mu.Unlock()

	o.mu.Lock()
	if len(o.freeSlots) == 0 {
		o.mu.Unlock()
		return false
	}
	slot := o.freeSlots[0]
	o.freeSlots = o.freeSlots[1:]
	o.mu.Unlock()

	id, truncated := sanitizeBaseN(s.Base)
	if truncated {
		lx.Warnf("bms-reader: BASE > %d байт, обрезан: %q", idMax, s.Base)
	}
	gen := o.nextGen()
	o.seg.allocSlot(slot, gen)
	d := &device{id: id, base: id, addr: s.Addr, slot: slot, gen: gen, st: st}
	st.mu.Lock()
	st.devs = append(st.devs, d)
	st.mu.Unlock()
	o.mu.Lock()
	o.devBySlot[slot] = d
	o.mu.Unlock()
	lx.Infof("bms-reader: + %s (%s) на %s addr=%d slot=%d", id, st.bt.Name(), st.src.desc, s.Addr, slot)
	return true
}

func (o *orchestrator) snapshot(st *srcState) []*device {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]*device(nil), st.devs...)
}

// sourcePoller — ОДИН поток на источник: последовательно опрашивает слоты,
// спит до конца секунды; публикует телеметрию (shm+raw) и шлёт heartbeat.
func (o *orchestrator) sourcePoller(st *srcState) {
	// Переиспользуемый таймер вместо time.After каждую секунду (меньше аллокаций).
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		select {
		case <-o.stop:
			return
		default:
		}
		devs := o.snapshot(st)
		if len(devs) == 0 {
			return
		}
		t0 := time.Now()
		for _, d := range devs {
			st.src.mu.Lock()
			tel, err := st.bt.Poll(st.src.tr, d.addr)
			tRecv := time.Now() // время завершения опроса, до кодирования/публикации
			st.src.mu.Unlock()
			if err != nil {
				d.miss++
				if d.miss >= st.bt.MissThreshold() {
					if err != errTimeout {
						lx.Warnf("bms-reader: - %s: %v", d.id, err)
					}
					select {
					case o.events <- devEvent{d: d, lost: true}:
					default:
					}
				}
				continue
			}
			d.miss = 0
			// Устройство могло быть потеряно, пока шёл опрос. Не публикуем
			// телеметрию и heartbeat для уже освобождённого/переиспользованного
			// слота: старый пулер не должен писать в чужой слот.
			if !o.isActive(d) {
				continue
			}
			if !o.seg.publishTelemetry(d.slot, d.gen, bmscontract.Encode(&tel)) {
				continue
			}
			if !o.isActive(d) {
				continue
			}
			publishRaw(o.sink, d.id, tel, tRecv)
			select {
			case o.events <- devEvent{d: d}:
			default:
			}
		}
		// спим до конца секунды
		next := t0.Add(time.Second)
		if wait := time.Until(next); wait > 0 {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
			select {
			case <-o.stop:
				return
			case <-timer.C:
			}
		}
	}
}

// handleEvent — оркестраторский обработчик: heartbeat (lastSeen) и потеря слота.
func (o *orchestrator) handleEvent(ev devEvent) {
	if ev.lost {
		o.loseDevice(ev.d)
		return
	}
	if !o.isActive(ev.d) {
		return // heartbeat от устройства, уже потерянного/переиспользованного
	}
	o.publishList(ev.d, 1)
}

// isActive — устройство всё ещё владеет своим слотом.
func (o *orchestrator) isActive(d *device) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.devBySlot[d.slot] == d
}

func (o *orchestrator) publishList(d *device, state uint8) {
	rec := listRecord{
		ID: d.id, Base: d.base, Source: d.st.src.desc, TypeID: d.st.bt.TypeID(),
		State: state, Framing: d.st.bt.Framing(), Gen: d.gen, LastSeen: uint64(time.Now().Unix()),
	}
	o.seg.publishList(d.slot, rec)
}

// loseDevice освобождает слот; пустой источник закрывается и уходит в отстойник.
// Идемпотентно (Р2-1): повторный lost для уже удалённого устройства игнорируется,
// иначе слот попадёт в freeSlots дважды.
func (o *orchestrator) loseDevice(d *device) {
	o.mu.Lock()
	cur, ok := o.devBySlot[d.slot]
	if !ok || cur != d {
		o.mu.Unlock()
		return
	}
	delete(o.devBySlot, d.slot)
	o.freeSlots = append(o.freeSlots, d.slot)
	o.mu.Unlock()
	o.seg.release(d.slot) // снятие записи из списка (arch §12/§13)

	st := d.st
	st.mu.Lock()
	for i, x := range st.devs {
		if x == d {
			st.devs = append(st.devs[:i], st.devs[i+1:]...)
			break
		}
	}
	empty := len(st.devs) == 0
	st.mu.Unlock()

	if empty {
		o.mu.Lock()
		delete(o.states, st.src.desc)
		o.mu.Unlock()
		o.bumpBackoff(st.src.desc, false)
		// Закрываем под мьютексом источника: пулер мог ещё читать транспорт.
		st.src.mu.Lock()
		st.src.tr.Close()
		st.src.mu.Unlock()
	}
}

// rescan — перебор адресов активных шин (добавление новых слотов).
func (o *orchestrator) rescan() {
	if o.stopped() {
		return
	}
	o.mu.Lock()
	sts := make([]*srcState, 0, len(o.states))
	for _, st := range o.states {
		sts = append(sts, st)
	}
	o.mu.Unlock()
	for _, st := range sts {
		if o.stopped() {
			return
		}
		// Пассивный AntBMS не сканируется: Probe лишь повторно читает поток.
		if _, passive := st.bt.(antBMS); passive {
			continue
		}
		st.src.mu.Lock()
		res, err := st.bt.Probe(st.src.tr)
		st.src.mu.Unlock()
		if o.stopped() {
			return
		}
		if err == nil && res.Kind == probeFound {
			added := false
			for _, s := range res.Slots {
				if o.stopped() {
					return
				}
				if o.addSlot(st, s) {
					added = true
				}
			}
			if added {
				for _, d := range o.snapshot(st) {
					o.publishList(d, 1)
				}
			}
		}
	}
}

// nextGen — монотонный gen = UnixMilli (+1 при совпадении), как в map/kes.
func (o *orchestrator) nextGen() uint64 {
	o.genMu.Lock()
	defer o.genMu.Unlock()
	g := uint64(time.Now().UnixMilli())
	if g <= o.gen {
		g = o.gen + 1
	}
	o.gen = g
	return g
}

func (o *orchestrator) inBackoff(desc string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.backoff[desc]
	return ok && time.Now().Before(st.until)
}

func (o *orchestrator) clearBackoff(desc string) {
	o.mu.Lock()
	delete(o.backoff, desc)
	o.mu.Unlock()
}

func (o *orchestrator) bumpBackoff(desc string, ban bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := o.backoff[desc]
	if st == nil {
		st = &boState{}
		o.backoff[desc] = st
	}
	if ban {
		st.until = time.Now().Add(banDuration)
		return
	}
	st.step++
	if st.step > len(backoffLadder) {
		st.step = len(backoffLadder)
	}
	st.until = time.Now().Add(backoffLadder[st.step-1])
}

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
