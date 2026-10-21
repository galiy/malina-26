package main

import (
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"

	"github.com/galiy/malina-26/daemons/lib/shm"
)

// scriptedPace — транспорт, отвечающий только на заданные ADR.
type scriptedPace struct {
	rb       []byte
	rp       int
	answered map[int]bool
	rtn      byte
	sn       string
	to       time.Duration
	sent     []int
}

func (s *scriptedPace) Write(p []byte) error {
	if len(p) < 5 || p[0] != '~' {
		return nil
	}
	adr, err := strconv.ParseInt(string(p[3:5]), 16, 32)
	if err != nil {
		return nil
	}
	s.sent = append(s.sent, int(adr))
	if s.answered[int(adr)] {
		s.rb = append(s.rb, buildPaceResp(int(adr), s.rtn, []byte(s.sn))...)
	}
	return nil
}

func (s *scriptedPace) Read(p []byte) (int, error) {
	if s.rp >= len(s.rb) {
		return 0, errTimeout
	}
	n := copy(p, s.rb[s.rp:])
	s.rp += n
	return n, nil
}

func (s *scriptedPace) SetReadTimeout(d time.Duration) { s.to = d }
func (s *scriptedPace) Close() error                   { return nil }

// runRescan — синхронный прогон плана рескана для юнит-тестов (в бою — воркер).
func runRescan(o *orchestrator) {
	for _, j := range o.planRescan() {
		o.applyProbe(o.probeSync(j))
	}
}

func newTestOrch(t *testing.T) (*orchestrator, *shm.Segment) {
	t.Helper()
	seg, err := shm.Open(filepath.Join(t.TempDir(), "bms-state"), bmsLayout())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seg.Close() })
	return newOrchestrator(&bmsShm{seg: seg}, nil, make(chan struct{}), nil), seg
}

// TestProbeCandidates — кандидаты: дырки в занятом диапазоне и max+1.
func TestProbeCandidates(t *testing.T) {
	o, _ := newTestOrch(t)
	st := &srcState{bt: enBMS{}}
	add := func(addr int) { st.devs = append(st.devs, &device{addr: addr, st: st}) }

	add(0)
	if got := o.probeCandidates(st); len(got) != 1 || got[0] != 1 {
		t.Fatalf("одна BMS на 0: %v", got)
	}
	add(2)
	if got := o.probeCandidates(st); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("дырка 1 + хвост 3: %v", got)
	}

	st.devs = nil
	for a := 0; a <= enMaxADR; a++ {
		add(a)
	}
	if got := o.probeCandidates(st); got != nil {
		t.Fatalf("занят весь диапазон: %v", got)
	}
}

// TestBackoffLadder — лестница 15с→30с→1м→5м→10м→никогда, затем сброс.
func TestBackoffLadder(t *testing.T) {
	o, _ := newTestOrch(t)
	key := backoffKey("serial:/dev/ttyUSB9", 1)
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute}
	for i, d := range want {
		o.bumpBackoff(key, "нет ответа")
		st := o.backoff[key]
		if st == nil || st.step != i+1 || st.exhausted {
			t.Fatalf("шаг %d: %+v", i+1, st)
		}
		left := time.Until(st.until)
		if left < d-2*time.Second || left > d+time.Second {
			t.Fatalf("шаг %d: пауза %s, ждали ~%s", i+1, left, d)
		}
	}
	o.bumpBackoff(key, "чужое устройство")
	if !o.inBackoff(key) || !o.backoff[key].exhausted {
		t.Fatalf("после лестницы ожидалось «никогда»: %+v", o.backoff[key])
	}

	o.resetBackoff()
	if o.inBackoff(key) {
		t.Fatal("rescan-сброс не снял «никогда»")
	}
	o.bumpBackoff(key, "нет ответа")
	if st := o.backoff[key]; st.step != 1 {
		t.Fatalf("после сброса шаг=%d", st.step)
	}
}

// TestBackoffPrefixReset — топология шины сбрасывает адресные записи.
func TestBackoffPrefixReset(t *testing.T) {
	o, _ := newTestOrch(t)
	desc := "serial:/dev/ttyUSB9"
	o.bumpBackoff(backoffKey(desc, 1), "нет ответа")
	o.bumpBackoff(backoffKey(desc, 2), "чужое устройство")
	o.bumpBackoff("serial:/dev/ttyUSB8", "нет ответа")

	o.clearBackoffPrefix(desc)
	if o.inBackoff(backoffKey(desc, 1)) || o.inBackoff(backoffKey(desc, 2)) {
		t.Fatal("адресные записи не сброшены")
	}
	if !o.inBackoff("serial:/dev/ttyUSB8") {
		t.Fatal("чужая запись задета")
	}
}

// TestEnBMSProbeStopsAfterFound — после первого Found обход прекращается на
// первом промахе; одиночный кандидат-дырка не тянет за собой остальные.
func TestEnBMSProbeStopsAfterFound(t *testing.T) {
	const sn = "EMU110110S-XXXX-YYMMDD-00001"
	tr := &scriptedPace{sn: sn, answered: map[int]bool{0: true, 2: true}}

	res, err := enBMS{}.Probe(tr, []int{0, 1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != probeFound || len(res.Slots) != 1 || res.Slots[0].Addr != 0 {
		t.Fatalf("probe: %+v", res)
	}
	if len(tr.sent) != 2 { // 0 найден, 1 — промах → стоп
		t.Fatalf("опрошены адреса %v, ждали [0 1]", tr.sent)
	}
	if tr.to != enProbeTimeout {
		t.Fatalf("таймаут пробы %s, ждали %s", tr.to, enProbeTimeout)
	}

	tr2 := &scriptedPace{sn: sn, answered: map[int]bool{2: true}}
	res2, _ := enBMS{}.Probe(tr2, []int{1})
	if res2.Kind != probeNoAnswer || len(tr2.sent) != 1 || tr2.sent[0] != 1 {
		t.Fatalf("дырка 1: res=%+v sent=%v", res2, tr2.sent)
	}
}

// TestRescanOneCandidateAndLadder — рескан пробует ровно одну дырку, промах
// уходит в отстойник; следующий рескан при активной паузе шину не трогает.
func TestRescanOneCandidateAndLadder(t *testing.T) {
	o, _ := newTestOrch(t)
	tr := &scriptedPace{answered: map[int]bool{}} // никто не отвечает
	st := &srcState{src: &source{desc: "serial:/dev/ttyUSB9", tr: tr}, bt: enBMS{}}
	st.devs = append(st.devs, &device{addr: 0, st: st})
	o.states[st.src.desc] = st

	runRescan(o)
	key := backoffKey(st.src.desc, 1)
	if !o.inBackoff(key) {
		t.Fatal("промах дырки не попал в отстойник")
	}
	if len(tr.sent) != 1 || tr.sent[0] != 1 {
		t.Fatalf("рескан опросил %v, ждали ровно [1]", tr.sent)
	}
	tr.sent = nil
	runRescan(o) // адрес в паузе — запросов быть не должно
	if len(tr.sent) != 0 {
		t.Fatalf("во время паузы опрошены %v", tr.sent)
	}
}

// TestRescanFoundClearsBackoff — успешная проба дырки добавляет слот и
// сбрасывает отстойник адреса.
func TestRescanFoundClearsBackoff(t *testing.T) {
	const sn = "EMU110110S-XXXX-YYMMDD-00002"
	o, _ := newTestOrch(t)
	tr := &scriptedPace{sn: sn, answered: map[int]bool{1: true}}
	st := &srcState{src: &source{desc: "serial:/dev/ttyUSB9", tr: tr}, bt: enBMS{}}
	o.states[st.src.desc] = st
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "AntBMS-DSP-160"}) {
		t.Fatal("стартовый слот 0 не добавлен")
	}
	key := backoffKey(st.src.desc, 1)
	o.bumpBackoff(key, "нет ответа")
	o.mu.Lock() // пауза истекла — кандидат снова пробуется
	o.backoff[key].until = time.Now().Add(-time.Second)
	o.mu.Unlock()

	runRescan(o)
	if len(st.devs) != 2 || st.devs[1].addr != 1 {
		t.Fatalf("слот не добавлен: %+v", st.devs)
	}
	if o.inBackoff(key) {
		t.Fatal("после Found отстойник адреса не сброшен")
	}
	items := o.seg.list()
	if len(items) != 2 {
		t.Fatalf("shm-список: %+v", items)
	}
	found := false
	for _, it := range items {
		if it.ID == sn {
			found = true
		}
	}
	if !found {
		t.Fatalf("найденная BMS не опубликована: %+v", items)
	}
}

// TestRescanNotMineUsesLadder — NotMine идёт по той же лестнице, что NoAnswer.
func TestRescanNotMineUsesLadder(t *testing.T) {
	o, _ := newTestOrch(t)
	tr := &scriptedPace{answered: map[int]bool{1: true}, rtn: 4}
	st := &srcState{src: &source{desc: "serial:/dev/ttyUSB9", tr: tr}, bt: enBMS{}}
	st.devs = append(st.devs, &device{addr: 0, st: st})
	o.states[st.src.desc] = st

	runRescan(o)
	stt := o.backoff[backoffKey(st.src.desc, 1)]
	if stt == nil || stt.step != 1 || stt.exhausted {
		t.Fatalf("NotMine: %+v", stt)
	}
}

// TestRescanSkipsExhausted — исчерпанная нижняя дырка не блокирует шину:
// проба переходит к следующему кандидату.
func TestRescanSkipsExhausted(t *testing.T) {
	const sn = "EMU110110S-XXXX-YYMMDD-00003"
	o, _ := newTestOrch(t)
	tr := &scriptedPace{sn: sn, answered: map[int]bool{3: true}}
	st := &srcState{src: &source{desc: "serial:/dev/ttyUSB9", tr: tr}, bt: enBMS{}}
	st.devs = append(st.devs, &device{addr: 0, st: st}, &device{addr: 2, st: st})
	o.states[st.src.desc] = st

	key1 := backoffKey(st.src.desc, 1)
	for i := 0; i < len(backoffLadder)+1; i++ {
		o.bumpBackoff(key1, "нет ответа")
	}
	if !o.inBackoff(key1) {
		t.Fatal("адрес 1 не исчерпан")
	}

	runRescan(o)
	if len(tr.sent) != 1 || tr.sent[0] != 3 {
		t.Fatalf("после «никогда» опрошены %v, ждали [3]", tr.sent)
	}
	if len(st.devs) != 3 || st.devs[2].addr != 3 {
		t.Fatalf("слот 3 не добавлен: %+v", st.devs)
	}
}

// TestEnqueueNonBlocking — постановка пробы в очередь не блокирует вызывающего,
// даже когда очередь заполнена: цикл оркестратора продолжает работать.
func TestEnqueueNonBlocking(t *testing.T) {
	o, _ := newTestOrch(t)
	for i := 0; i < 100; i++ {
		src := &source{desc: "serial:/dev/ttyX" + strconv.Itoa(i)}
		done := make(chan struct{})
		go func() {
			o.enqueue(probeJob{src: src, adr: -1})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("enqueue заблокировался на %d-й постановке", i)
		}
	}
	if got := len(o.jobs); got != cap(o.jobs) {
		t.Fatalf("очередь: %d, ждали заполнение %d", got, cap(o.jobs))
	}
}

// TestApplyProbeNewSourceNotMine — NotMine нового источника идёт по общей
// лестнице отстойника, транспорт не удерживается.
func TestApplyProbeNewSourceNotMine(t *testing.T) {
	o, _ := newTestOrch(t)
	src := &source{desc: "serial:/dev/ttyX"}
	o.applyProbe(probeOutcome{job: probeJob{src: src, adr: -1}, kind: probeNotMine})
	st := o.backoff[src.desc]
	if st == nil || st.step != 1 || st.exhausted {
		t.Fatalf("отстойник источника: %+v", st)
	}
}

// --- R9/R10: изоляция пробы от пулеров и цикла ---

type blockingProbeType struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingProbeType) TypeID() uint8      { return 99 }
func (b *blockingProbeType) Name() string       { return "blocking" }
func (b *blockingProbeType) Framing() uint8     { return framingNative }
func (b *blockingProbeType) Speeds() []int      { return []int{19200} }
func (b *blockingProbeType) MissThreshold() int { return 1 }
func (b *blockingProbeType) MaxAddr() int       { return 15 }
func (b *blockingProbeType) Poll(Transport, int) (bmscontract.Telemetry, error) {
	return bmscontract.Telemetry{}, errTimeout
}
func (b *blockingProbeType) Probe(Transport, []int) (probeResult, error) {
	close(b.started)
	<-b.release
	return probeResult{Kind: probeNoAnswer}, nil
}

type countingPollType struct{ n int32 }

func (c *countingPollType) TypeID() uint8      { return 98 }
func (c *countingPollType) Name() string       { return "counting" }
func (c *countingPollType) Framing() uint8     { return framingNative }
func (c *countingPollType) Speeds() []int      { return []int{19200} }
func (c *countingPollType) MissThreshold() int { return 1 }
func (c *countingPollType) MaxAddr() int       { return 15 }
func (c *countingPollType) Probe(Transport, []int) (probeResult, error) {
	return probeResult{Kind: probeNoAnswer}, nil
}
func (c *countingPollType) Poll(Transport, int) (bmscontract.Telemetry, error) {
	atomic.AddInt32(&c.n, 1)
	var tel bmscontract.Telemetry
	tel.CellCount = 1
	tel.CellsMV[0] = 3300
	return tel, nil
}

type closeFlagTransport struct{ closed int32 }

func (t *closeFlagTransport) Read([]byte) (int, error)     { return 0, errTimeout }
func (t *closeFlagTransport) Write([]byte) error           { return nil }
func (t *closeFlagTransport) SetReadTimeout(time.Duration) {}
func (t *closeFlagTransport) Close() error                 { atomic.StoreInt32(&t.closed, 1); return nil }

// TestProbeDoesNotBlockHeartbeat (R9): воркер висит в пробе нового порта, а
// цикл оркестратора продолжает обрабатывать heartbeat и публиковать last_seen.
func TestProbeDoesNotBlockHeartbeat(t *testing.T) {
	o, _ := newTestOrch(t)
	st := &srcState{src: &source{desc: "serial:/dev/ttyA", tr: &fakeTransport{}}, bt: enBMS{}}
	o.states[st.src.desc] = st
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "BMS-A"}) {
		t.Fatal("слот не добавлен")
	}
	dev := st.devs[0]

	started := make(chan struct{})
	release := make(chan struct{})
	tr := &closeFlagTransport{}
	probe := func(probeJob) probeOutcome {
		close(started)
		<-release
		return probeOutcome{kind: probeNoAnswer, tr: tr}
	}
	go o.runLoop(probe, func() {}, func() {})
	var once sync.Once
	stopAll := func() {
		once.Do(func() { close(o.stop) })
		o.waitPollers()
	}
	defer stopAll()

	o.enqueue(probeJob{src: &source{desc: "serial:/dev/ttyNew"}, adr: -1})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("проба не началась")
	}

	o.events <- devEvent{d: dev}
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		items := o.seg.list()
		if len(items) == 1 && items[0].LastSeen > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("heartbeat не опубликован, пока идёт проба")
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(release)
	stopAll()
	if atomic.LoadInt32(&tr.closed) == 0 {
		t.Fatal("транспорт пробы не закрыт при остановке")
	}
}

// TestHoleProbeDoesNotBlockOtherBus (R10): проба дырки шины A держит только
// мьютекс A; пулер шины B продолжает опрос.
func TestHoleProbeDoesNotBlockOtherBus(t *testing.T) {
	o, _ := newTestOrch(t)
	started := make(chan struct{})
	release := make(chan struct{})
	stA := &srcState{src: &source{desc: "serial:/dev/ttyA", tr: &fakeTransport{}},
		bt: &blockingProbeType{started: started, release: release}}
	cp := &countingPollType{}
	stB := &srcState{src: &source{desc: "serial:/dev/ttyB", tr: &fakeTransport{}}, bt: cp}
	o.states[stA.src.desc] = stA
	o.states[stB.src.desc] = stB
	if !o.addSlot(stA, probeSlot{Addr: 0, Base: "BMS-A"}) {
		t.Fatal("слот A не добавлен")
	}
	if !o.addSlot(stB, probeSlot{Addr: 0, Base: "BMS-B"}) {
		t.Fatal("слот B не добавлен")
	}
	o.publishList(stB.devs[0], 1) // как в addSource: item готов до старта пулера
	o.pollWg.Add(1)
	go func() {
		defer o.pollWg.Done()
		o.sourcePoller(stB)
	}()
	defer func() {
		close(o.stop)
		o.waitPollers()
	}()

	// Дождаться первого опроса B, затем обнулить счётчик: следующий опрос
	// должен случиться уже во время заблокированной пробы A.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&cp.n) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("пулер B не стартовал")
		}
		time.Sleep(10 * time.Millisecond)
	}
	atomic.StoreInt32(&cp.n, 0)

	done := make(chan probeOutcome, 1)
	go func() { done <- o.probeSync(probeJob{src: stA.src, st: stA, adr: 1}) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("проба A не началась")
	}

	deadline = time.Now().Add(2500 * time.Millisecond)
	for atomic.LoadInt32(&cp.n) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("пулер шины B не опрашивал, пока шла проба шины A")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	<-done
}
