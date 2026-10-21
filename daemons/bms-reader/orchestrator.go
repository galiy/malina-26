package main

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/ports"
	"github.com/galiy/malina-26/daemons/lib/stat"
)

// Тайминги (arch §4/§10).
var (
	// backoffLadder — единая лестница отстойника для NoAnswer и NotMine.
	// После последней ступени запись считается исчерпанной («никогда») до сброса.
	backoffLadder = []time.Duration{
		15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute,
	}
	scanIdle     = 10 * time.Second // нет активных устройств
	scanActive   = time.Minute      // есть активные
	rescanPeriod = 30 * time.Second
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

// boState — состояние отстойника: ступень лестницы либо «никогда».
type boState struct {
	step      int
	until     time.Time
	exhausted bool // лестница пройдена: до сброса не пробуем
}

type devEvent struct {
	d    *device
	lost bool
}

// probeJob — задание воркеру: новый источник (st == nil, adr = -1) либо одна
// дырка активной шины (st != nil, adr >= 0).
type probeJob struct {
	src *source
	st  *srcState
	adr int
}

func (j probeJob) key() string {
	if j.st == nil {
		return j.src.desc
	}
	return backoffKey(j.src.desc, j.adr)
}

// probeOutcome — результат пробы. tr != nil только для найденного нового
// источника: владение передаётся циклу оркестратора.
type probeOutcome struct {
	job     probeJob
	kind    probeKind
	matched bmsType
	slots   []probeSlot
	tr      Transport
	reason  string
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

	// Проба (новые порты и дырки активных шин) выполняется отдельным воркером:
	// цикл оркестратора не блокируется и продолжает публиковать heartbeat.
	jobs     chan probeJob
	results  chan probeOutcome
	inflight map[string]bool

	// rescanCh — внеочередной проход по кандидатам (control-команда rescan):
	// сбрасывает отстойник и сразу повторяет scan+rescan.
	rescanCh chan struct{}

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

// waitPollers ждёт завершения пулеров и воркера проб. После остановки в
// очереди результатов могли остаться открытые транспорты — закрываем их.
func (o *orchestrator) waitPollers() {
	o.pollWg.Wait()
	for {
		select {
		case out := <-o.results:
			if out.tr != nil {
				out.tr.Close()
			}
		default:
			return
		}
	}
}

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

func newOrchestrator(seg *bmsShm, sink *stat.Sink, stop chan struct{}, rescanCh chan struct{}) *orchestrator {
	free := make([]int, 0, maxDevices)
	for i := 0; i < maxDevices; i++ {
		free = append(free, i)
	}
	if rescanCh == nil {
		rescanCh = make(chan struct{}, 1)
	}
	return &orchestrator{
		seg: seg, sink: sink,
		states: make(map[string]*srcState), devBySlot: make(map[int]*device),
		freeSlots: free, backoff: make(map[string]*boState),
		events: make(chan devEvent, 64), rescanCh: rescanCh, stop: stop,
		jobs: make(chan probeJob, 16), results: make(chan probeOutcome, 16),
		inflight: make(map[string]bool),
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

// run — единственный оркестраторский цикл: планирование проб, события и
// писатель метаданных shm-списка (last_seen пишет пулер, arch §12).
//
// Сама проба идёт в отдельном воркере (probeWorker): цикл не блокируется и
// продолжает обрабатывать heartbeat/lost, пока воркер открывает новый порт или
// проверяет дырку. Мьютекс источника на время пробы держится только для той
// шины, которую пробуют; пулеры остальных шин работают независимо.
func (o *orchestrator) run() {
	o.runLoop(o.probeSync, o.scan, o.rescan)
}

// runLoop — тело цикла с подменяемыми операциями (юнит-тесты R9/R10).
func (o *orchestrator) runLoop(probe func(probeJob) probeOutcome, scan, rescan func()) {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	lastScan := time.Time{}
	scan()
	lastScan = time.Now()
	var lastRescan time.Time
	o.pollWg.Add(1)
	go o.probeWorker(probe)
	defer close(o.jobs)
	for {
		select {
		case <-o.stop:
			return
		case out := <-o.results:
			o.applyProbe(out)
		case <-o.rescanCh:
			// Явный rescan: сброс отстойника (включая «никогда») и внеочередной
			// проход по кандидатам.
			o.resetBackoff()
			scan()
			rescan()
			lastScan = time.Now()
			lastRescan = lastScan
		case <-tick.C:
			now := time.Now()
			if now.Sub(lastRescan) >= rescanPeriod {
				rescan()
				lastRescan = now
			}
			if now.Sub(lastScan) >= o.scanInterval() {
				scan()
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

// allCandidates — полный диапазон адресов типа: обход новой шины.
func allCandidates(bt bmsType) []int {
	max := bt.MaxAddr()
	out := make([]int, 0, max+1)
	for a := 0; a <= max; a++ {
		out = append(out, a)
	}
	return out
}

func (o *orchestrator) scan() {
	if o.stopped() {
		return
	}
	busy := busyRdevs()
	paths := ports.ListPorts(serialPatterns)
	gws := o.gateways().Gateways
	// Сброс отстойника для исчезнувших источников: вынули/переткнули адаптер —
	// это изменение топологии, старые выводы недействительны.
	o.pruneBackoff(paths, gws)
	for _, path := range paths {
		if o.stopped() {
			return
		}
		desc := "serial:" + path
		if o.hasState(desc) || o.inBackoff(desc) {
			continue
		}
		if fi, err := os.Stat(path); err == nil && busy[ports.RdevOf(fi)] {
			continue // порт держит map/kes — не трогаем
		}
		o.enqueue(probeJob{src: &source{desc: desc, serial: path}, adr: -1})
	}
	for _, g := range gws {
		if o.stopped() {
			return
		}
		desc := "tcp:" + g.Host + ":" + itoa(g.Port)
		if o.hasState(desc) || o.inBackoff(desc) {
			continue
		}
		o.enqueue(probeJob{src: &source{desc: desc, host: g.Host, port: g.Port}, adr: -1})
	}
}

func (o *orchestrator) hasState(desc string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.states[desc]
	return ok
}

// enqueue ставит пробу в очередь воркера, не блокируя цикл оркестратора.
// Один и тот же кандидат не может быть в работе дважды.
func (o *orchestrator) enqueue(j probeJob) {
	key := j.key()
	o.mu.Lock()
	if o.inflight[key] {
		o.mu.Unlock()
		return
	}
	o.inflight[key] = true
	o.mu.Unlock()
	select {
	case o.jobs <- j:
	default:
		o.mu.Lock()
		delete(o.inflight, key)
		o.mu.Unlock()
	}
}

// probeWorker — единственный поток, который открывает порты и опрашивает
// кандидатов. Цикл оркестратора в это время продолжает публиковать heartbeat,
// поэтому проба нового порта не блокирует пулеры остальных.
func (o *orchestrator) probeWorker(probe func(probeJob) probeOutcome) {
	defer o.pollWg.Done()
	for j := range o.jobs {
		if o.stopped() {
			return
		}
		out := probe(j)
		if o.stopped() {
			if out.tr != nil {
				out.tr.Close()
			}
			return
		}
		select {
		case o.results <- out:
		case <-o.stop:
			if out.tr != nil {
				out.tr.Close()
			}
			return
		}
	}
}

// probeSync выполняет одну пробу. Для нового источника держит транспорт
// открытым при Found и передаёт его в outcome.
func (o *orchestrator) probeSync(j probeJob) probeOutcome {
	start := time.Now()
	out := probeOutcome{job: j, kind: probeNoAnswer, reason: "нет ответа"}
	defer func() {
		where := j.src.desc
		adr := j.adr
		if j.st != nil {
			where = j.st.src.desc
		}
		lx.Debugf("probe t=%d src=%s adr=%d dur_us=%d kind=%d slots=%d reason=%s",
			time.Now().UnixMicro(), where, adr, time.Since(start).Microseconds(), out.kind, len(out.slots), out.reason)
	}()
	if j.st != nil {
		if j.adr < 0 {
			return out
		}
		j.st.src.mu.Lock()
		res, err := j.st.bt.Probe(j.st.src.tr, []int{j.adr})
		j.st.src.mu.Unlock()
		if err == nil {
			out.kind = res.Kind
			out.slots = res.Slots
		}
		return out
	}
	if j.src.serial != "" {
		for _, baud := range unionSpeeds() {
			tr, err := openSerial(j.src.serial, baud)
			if err != nil {
				continue
			}
			if o.stopped() {
				tr.Close()
				return out
			}
			matched, slots, notMine := o.probeAll(tr)
			if matched != nil {
				out.kind = probeFound
				out.matched = matched
				out.slots = slots
				out.tr = tr
				return out
			}
			tr.Close()
			if notMine {
				out.kind = probeNotMine
				out.reason = "чужое устройство"
				return out
			}
		}
		return out
	}
	tr, err := openTCP(j.src.host, j.src.port)
	if err != nil {
		out.reason = "порт не открывается"
		return out
	}
	if o.stopped() {
		tr.Close()
		return out
	}
	matched, slots, notMine := o.probeAll(tr)
	if matched != nil {
		out.kind = probeFound
		out.matched = matched
		out.slots = slots
		out.tr = tr
		return out
	}
	tr.Close()
	if notMine {
		out.kind = probeNotMine
		out.reason = "чужое устройство"
	}
	return out
}

// applyProbe применяет результат пробы в цикле оркестратора: регистрация,
// отстойник, публикация. Здесь же снимается отметка in-flight.
func (o *orchestrator) applyProbe(out probeOutcome) {
	o.mu.Lock()
	delete(o.inflight, out.job.key())
	o.mu.Unlock()
	if o.stopped() {
		if out.tr != nil {
			out.tr.Close()
		}
		return
	}
	j := out.job
	if j.st == nil {
		switch out.kind {
		case probeFound:
			o.clearBackoffPrefix(j.src.desc)
			j.src.tr = out.tr
			o.addSource(j.src, out.matched, out.slots)
			if out.tr != nil && !o.hasState(j.src.desc) {
				out.tr.Close()
			}
		case probeNotMine:
			o.bumpBackoff(j.src.desc, "чужое устройство")
		default:
			o.bumpBackoff(j.src.desc, out.reason)
		}
		return
	}
	if !o.hasState(j.src.desc) {
		return // шина потеряна, результат неактуален
	}
	key := backoffKey(j.src.desc, j.adr)
	switch out.kind {
	case probeFound:
		added := false
		for _, s := range out.slots {
			if o.addSlot(j.st, s) {
				added = true
			}
		}
		o.clearBackoff(key)
		if added {
			for _, d := range o.snapshot(j.st) {
				o.publishList(d, 1)
			}
		}
	case probeNotMine:
		o.bumpBackoff(key, "чужое устройство")
	default:
		o.bumpBackoff(key, out.reason)
	}
}

func (o *orchestrator) probeAll(tr Transport) (bmsType, []probeSlot, bool) {
	sawNotMine := false
	for _, bt := range bmsTypesOrder {
		if o.stopped() {
			return nil, nil, false
		}
		res, err := bt.Probe(tr, allCandidates(bt))
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
	// публикуем список для добавленных устройств (метаданные пишет только
	// оркестратор; last_seen дополнительно обновляет пулер, arch §12)
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
	// Изменение состава слотов — топология шины изменилась: отстойник сброшен.
	o.clearBackoffPrefix(st.src.desc)
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
			tPoll := time.Now()
			st.src.mu.Lock()
			tel, err := st.bt.Poll(st.src.tr, d.addr)
			tRecv := time.Now() // время завершения опроса, до кодирования/публикации
			st.src.mu.Unlock()
			if err != nil {
				lx.Debugf("poll t=%d src=%s slot=%d addr=%d dur_us=%d err=%v miss=%d",
					tRecv.UnixMicro(), st.src.desc, d.slot, d.addr, tRecv.Sub(tPoll).Microseconds(), err, d.miss+1)
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
			lx.Debugf("poll t=%d src=%s slot=%d addr=%d dur_us=%d cells=%d cur=%d soc=%d mode=%d gen=%d",
				tRecv.UnixMicro(), st.src.desc, d.slot, d.addr, tRecv.Sub(tPoll).Microseconds(),
				tel.CellCount, tel.CurrentDA, tel.SocDP, tel.Mode, d.gen)
			// Устройство могло быть потеряно, пока шёл опрос. Не публикуем
			// телеметрию и heartbeat для уже освобождённого/переиспользованного
			// слота: старый пулер не должен писать в чужой слот.
			if !o.isActive(d) {
				continue
			}
			if !o.seg.publishTelemetry(d.slot, d.gen, bmscontract.Encode(&tel)) {
				continue
			}
			// last_seen — время успешного опроса, не времени разбора события.
			// Пишем сразу пулером: занятость цикла и дропы событий не старят BMS.
			// Best-effort: сбой touch не должен гасить публикацию/raw.
			o.seg.touchLastSeen(d.slot, d.gen, tRecv)
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
		// Топология изменилась: адресные записи отстойника сбрасываем, а сам
		// источник пробуем снова по первой ступени (15 с).
		o.clearBackoffPrefix(st.src.desc)
		o.bumpBackoff(st.src.desc, "устройство потеряно")
		// Закрываем под мьютексом источника: пулер мог ещё читать транспорт.
		st.src.mu.Lock()
		st.src.tr.Close()
		st.src.mu.Unlock()
		return
	}
	// Одно из устройств выпало — состав слотов изменился.
	o.clearBackoffPrefix(st.src.desc)
}

// probeCandidates — адреса для рескана: дырки в занятом диапазоне и max+1,
// по возрастанию. Пусто, если свободных кандидатов нет.
func (o *orchestrator) probeCandidates(st *srcState) []int {
	devs := o.snapshot(st)
	if len(devs) == 0 {
		return nil
	}
	occ := make(map[int]bool, len(devs))
	max := -1
	for _, d := range devs {
		occ[d.addr] = true
		if d.addr > max {
			max = d.addr
		}
	}
	var out []int
	for a := 0; a <= max; a++ {
		if !occ[a] {
			out = append(out, a)
		}
	}
	if tail := max + 1; tail <= st.bt.MaxAddr() {
		out = append(out, tail)
	}
	return out
}

// backoffKey — ключ отстойника: адрес конкретной шины либо источник.
func backoffKey(desc string, adr int) string {
	return desc + "#" + itoa(adr)
}

// rescan — проба одного кандидата на каждой активной шине (arch §6.4).
// Кандидат — первая дырка/хвост. Если он в активной паузе, шина в этом проходе
// не пробуется (следующий адрес не ищем). Исчерпанный («никогда») кандидат
// пропускается: «никогда» относится к адресу, а не ко всей шине.
// rescan ставит в очередь пробы дырок по одной на активную шину.
func (o *orchestrator) rescan() {
	for _, j := range o.planRescan() {
		o.enqueue(j)
	}
}

// planRescan вычисляет задания на пробы дырок. Кандидат — первая дырка/max+1.
// Если он в активной паузе, шина в этом проходе не пробуется (следующий адрес
// не ищем). Исчерпанный («никогда») кандидат пропускается: «никогда» относится
// к адресу, а не ко всей шине.
func (o *orchestrator) planRescan() []probeJob {
	if o.stopped() {
		return nil
	}
	o.mu.Lock()
	sts := make([]*srcState, 0, len(o.states))
	for _, st := range o.states {
		sts = append(sts, st)
	}
	o.mu.Unlock()
	var jobs []probeJob
	for _, st := range sts {
		if o.stopped() {
			return jobs
		}
		// Пассивный AntBMS не сканируется: Probe лишь повторно читает поток.
		if _, passive := st.bt.(antBMS); passive {
			continue
		}
		cands := o.probeCandidates(st)
		if len(cands) == 0 {
			continue
		}
		adr := -1
		for _, c := range cands {
			paused, exhausted := o.backoffStatus(backoffKey(st.src.desc, c))
			if exhausted {
				continue
			}
			if !paused {
				adr = c
			}
			break
		}
		if adr < 0 {
			continue
		}
		jobs = append(jobs, probeJob{src: st.src, st: st, adr: adr})
	}
	return jobs
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

// backoffStatus — состояние записи: активная пауза и/или исчерпание.
func (o *orchestrator) backoffStatus(key string) (paused, exhausted bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.backoff[key]
	if !ok {
		return false, false
	}
	if st.exhausted {
		return false, true
	}
	return time.Now().Before(st.until), false
}

// inBackoff — запись в отстойнике (идёт пауза или исчерпана «никогда»).
func (o *orchestrator) inBackoff(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	st, ok := o.backoff[key]
	if !ok {
		return false
	}
	return st.exhausted || time.Now().Before(st.until)
}

func (o *orchestrator) clearBackoff(key string) {
	o.mu.Lock()
	delete(o.backoff, key)
	o.mu.Unlock()
}

// clearBackoffPrefix сбрасывает записи источника и всех его адресов (desc#adr).
// Используется при изменении топологии шины.
func (o *orchestrator) clearBackoffPrefix(desc string) {
	prefix := desc + "#"
	o.mu.Lock()
	for k := range o.backoff {
		if k == desc || strings.HasPrefix(k, prefix) {
			delete(o.backoff, k)
		}
	}
	o.mu.Unlock()
}

// resetBackoff — полный сброс отстойника (явный rescan).
func (o *orchestrator) resetBackoff() {
	o.mu.Lock()
	o.backoff = make(map[string]*boState)
	o.mu.Unlock()
}

// pruneBackoff снимает отстойник с источников, которых больше нет в системе
// (адаптер вынули/переткнули, шлюз удалили) — топология изменилась.
func (o *orchestrator) pruneBackoff(paths []string, gws []Gateway) {
	present := make(map[string]bool, len(paths)+len(gws))
	for _, p := range paths {
		present["serial:"+p] = true
	}
	for _, g := range gws {
		present["tcp:"+g.Host+":"+itoa(g.Port)] = true
	}
	o.mu.Lock()
	for k := range o.backoff {
		desc := k
		if i := strings.IndexByte(k, '#'); i >= 0 {
			desc = k[:i]
		}
		if !present[desc] {
			delete(o.backoff, k)
		}
	}
	o.mu.Unlock()
}

// bumpBackoff двигает запись по единой лестнице; после последней ступени —
// «никогда» (exhausted) до сброса.
func (o *orchestrator) bumpBackoff(key, reason string) {
	o.mu.Lock()
	st := o.backoff[key]
	if st == nil {
		st = &boState{}
		o.backoff[key] = st
	}
	if st.exhausted {
		o.mu.Unlock()
		return
	}
	st.step++
	if st.step > len(backoffLadder) {
		st.exhausted = true
		st.until = time.Time{}
		o.mu.Unlock()
		lx.Infof("отстойник %s: %s — больше не пробую до сброса", key, reason)
		return
	}
	d := backoffLadder[st.step-1]
	st.until = time.Now().Add(d)
	step := st.step
	o.mu.Unlock()
	lx.Infof("отстойник %s: %s — пауза %s (шаг %d/%d)", key, reason, d, step, len(backoffLadder))
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
