package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
	"github.com/galiy/malina-26/daemons/lib/solarman"
)

const (
	statusInit  = 0
	statusOK    = 1
	statusStale = 2
	statusBad   = 3
	statusNoCon = 4
	statusBan1  = 5
	statusBan5  = 6
	statusBan10 = 7
)

const (
	connectTimeout = time.Second
	deyeRead       = 2 * time.Second
	sofarRead      = 5 * time.Second
)

func readTimeoutFor(typ string) time.Duration {
	if typ == "sofar" {
		return sofarRead
	}
	return deyeRead
}

func idleWindowFor(typ string) time.Duration {
	if typ == "sofar" {
		// Sofar может прислать несколько кадров подряд; ответ считаем
		// завершённым после короткой тишины, не дожидаясь полного таймаута.
		return 400 * time.Millisecond
	}
	return deyeRead
}

type pollErrKind int

const (
	errBad pollErrKind = iota + 1
	errNoRoute
	errOther
)

type pollError struct {
	kind        pollErrKind
	err         error
	readTimeout bool
}

func (e *pollError) Error() string { return e.err.Error() }
func (e *pollError) Unwrap() error { return e.err }

type devState struct {
	lastSeen    int64
	status      uint8 // последний результат (1/3/4), не бан
	failCount   int
	banUntil    time.Time
	banStatus   uint8
	nextTry     time.Time
	listed      bool
	attempts    int
	timeouts    int
	windowStart time.Time
}

type poller struct {
	shm          *Shm
	gen          uint64
	states       []devState
	types        []string
	lastSeen     []string // известный inverter_sn по слоту
	energyWarned []bool
	sink         *tsdbSink
	lx           *logx.Logger
	// neighbor — источник признака «жив на L2» после connect timeout
	// (по умолчанию netlink-таблица соседей; в тестах подменяется).
	neighbor neighborChecker
	// Длительность последнего круга по группам: у Deye и Sofar независимые
	// циклы опроса, каждый пишет своё поле.
	lastRoundDeye  time.Duration
	lastRoundSofar time.Duration
}

func newPoller(shm *Shm, sink *tsdbSink, lx *logx.Logger) *poller {
	return &poller{shm: shm, sink: sink, lx: lx, neighbor: neighborReachable}
}

func (p *poller) logf() func(string, ...any) {
	if p.lx == nil {
		return nil
	}
	return p.lx.Debugf
}

// fullRegsLast берёт последний корректный PDU из всех кадров ответа.
// Sofar не повторяет serial запроса и может прислать несколько кадров
// (включая нулевой/устаревший); актуальные данные — в последнем полном PDU.
func fullRegsLast(frames []solarman.Frame, start, count uint16) (map[uint16]uint16, bool) {
	var out map[uint16]uint16
	for _, f := range frames {
		for _, p := range solarman.ParseModbusPDU(f.Payload) {
			if p.CRC != p.CRCCalc || len(p.Values) < int(count) {
				continue
			}
			m := make(map[uint16]uint16, count)
			for i := 0; i < int(count); i++ {
				m[start+uint16(i)] = p.Values[i]
			}
			out = m
		}
	}
	return out, out != nil
}

// reset применяет новый конфиг: чистит shm, сбрасывает баны/таймеры и
// готовит слоты. Устройства снова появятся в списке после первого полного опроса.
func (p *poller) reset(cfg settings) {
	p.gen++
	p.states = make([]devState, len(cfg.Inverters))
	p.types = make([]string, len(cfg.Inverters))
	for i, d := range cfg.Inverters {
		p.types[i] = d.Type
	}
	p.lastSeen = make([]string, len(cfg.Inverters))
	p.energyWarned = make([]bool, len(cfg.Inverters))
	p.lastRoundDeye = 0
	p.lastRoundSofar = 0
	for i := 0; i < gridSlots; i++ {
		p.shm.Item(i).Release()
		p.shm.Slot(i).Release()
	}
	for i := range cfg.Inverters {
		p.shm.Slot(i).PrepareGen(p.gen)
	}
}

func (p *poller) clear() {
	for i := 0; i < gridSlots; i++ {
		p.shm.Item(i).Release()
		p.shm.Slot(i).Release()
	}
}

func (p *poller) staleThreshold(typ string, period time.Duration) time.Duration {
	base := period
	lastRound := p.lastRoundDeye
	if typ == "sofar" {
		lastRound = p.lastRoundSofar
	}
	if lastRound > base {
		base = lastRound
	}
	return base * 7 / 2 // 3,5 периода
}

func (p *poller) currentStatus(i int, now time.Time, stale time.Duration) uint8 {
	st := &p.states[i]
	if now.Before(st.banUntil) {
		return st.banStatus
	}
	if st.status != 0 {
		if st.status == statusOK {
			if st.lastSeen > 0 && time.Since(time.Unix(st.lastSeen, 0)) > stale {
				return statusStale
			}
		}
		return st.status
	}
	if st.lastSeen > 0 {
		if time.Since(time.Unix(st.lastSeen, 0)) > stale {
			return statusStale
		}
		return statusOK
	}
	return statusInit
}

// pollOnce — один последовательный круг устройств одной группы (typ: deye или
// sofar). Возвращает длительность круга.
func (p *poller) pollOnce(ctx context.Context, cfg settings, typ string) time.Duration {
	start := time.Now()
	period := time.Duration(cfg.PollPeriod) * time.Second
	count := 0
	for i := range cfg.Inverters {
		select {
		case <-ctx.Done():
			return time.Since(start)
		default:
		}
		if !cfg.Inverters[i].Valid || cfg.Inverters[i].Type != typ {
			continue
		}
		count++
		st := &p.states[i]
		now := time.Now()
		if now.Before(st.banUntil) || now.Before(st.nextTry) {
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, now, p.staleThreshold(typ, period)))
			continue
		}
		t, sn, err := p.pollDevice(ctx, i, cfg.Inverters[i], p.lastSeen[i])
		if err != nil {
			if ctx.Err() != nil {
				return time.Since(start)
			}
			if pe, ok := err.(*pollError); ok && pe.kind == errBad {
				// однократный повтор для плохого ответа
				t, sn, err = p.pollDevice(ctx, i, cfg.Inverters[i], p.lastSeen[i])
			}
		}
		if err != nil {
			p.fail(i, err)
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, time.Now(), p.staleThreshold(typ, period)))
			continue
		}
		p.success(i, t, sn)
		p.publishList(i, cfg.Inverters[i], statusOK)
	}
	d := time.Since(start)
	if typ == "sofar" {
		p.lastRoundSofar = d
	} else {
		p.lastRoundDeye = d
	}
	p.lx.Debugf("круг %s: %d устройств, %s", typ, count, d.Round(time.Millisecond))
	// обновить stale у уже опубликованных записей этой группы
	now := time.Now()
	stale := p.staleThreshold(typ, period)
	for i := range cfg.Inverters {
		if cfg.Inverters[i].Type == typ && p.states[i].listed {
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, now, stale))
		}
	}
	return d
}

// pollLoop — независимый цикл опроса одной группы (deye или sofar): свой
// период, свой круг. Группы не ждут друг друга.
func (p *poller) pollLoop(ctx context.Context, cfg settings, typ string) {
	period := time.Duration(cfg.PollPeriod) * time.Second
	for {
		round := p.pollOnce(ctx, cfg, typ)
		if ctx.Err() != nil {
			return
		}
		wait := period - round
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *poller) success(i int, t telemetry, sn string) {
	st := &p.states[i]
	st.attempts++
	p.checkTimeoutRate(i, st)
	st.status = statusOK
	st.failCount = 0
	st.banUntil = time.Time{}
	st.nextTry = time.Time{}
	st.lastSeen = time.Now().Unix()
	st.listed = true
	p.lastSeen[i] = sn
	p.lx.Debugf("инвертор #%d: успех, SN=%s", i+1, sn)
	p.publishTelemetry(i, t)
}

func (p *poller) fail(i int, err error) {
	st := &p.states[i]
	now := time.Now()
	kind := errOther
	readTimeout := false
	if pe, ok := err.(*pollError); ok {
		kind = pe.kind
		readTimeout = pe.readTimeout
	}
	st.attempts++
	if readTimeout {
		st.timeouts++
	}
	p.checkTimeoutRate(i, st)
	switch kind {
	case errNoRoute:
		st.status = statusNoCon
		st.banUntil = now.Add(10 * time.Minute)
		st.banStatus = statusBan10
		st.nextTry = st.banUntil
		st.failCount = 0
		p.lx.Warnf("инвертор #%d: нет связи (нет солнца/выключен) — бан 10 мин", i+1)
		return
	case errBad:
		// Плохой ответ (включая connect timeout при живом ARP и таймаут
		// полного блока) — мягкий backoff для обоих типов: без бана, после
		// двух промахов подряд пауза 1 минута.
		st.status = statusBad
		st.failCount++
		if st.failCount >= 2 {
			st.nextTry = now.Add(time.Minute)
		}
		p.lx.Warnf("инвертор #%d: плохой ответ (%v), подряд=%d — мягкий backoff", i+1, err, st.failCount)
		return
	default:
		st.status = statusNoCon
	}
	st.failCount++
	switch {
	case st.failCount >= 5:
		st.banUntil = now.Add(5 * time.Minute)
		st.banStatus = statusBan5
		st.nextTry = st.banUntil
	case st.failCount == 2:
		st.banUntil = now.Add(time.Minute)
		st.banStatus = statusBan1
		st.nextTry = st.banUntil
	case st.failCount > 2:
		st.nextTry = now.Add(time.Minute)
	}
	p.lx.Warnf("инвертор #%d: неудача (%s: %v), подряд=%d", i+1, kindName(kind), err, st.failCount)
}

func (p *poller) checkTimeoutRate(i int, st *devState) {
	now := time.Now()
	if st.windowStart.IsZero() {
		st.windowStart = now
	}
	if now.Sub(st.windowStart) < time.Hour || st.attempts < 20 {
		return
	}
	if float64(st.timeouts)/float64(st.attempts) > 0.05 {
		p.lx.Warnf("инвертор #%d: доля таймаутов приёма %.1f%% за час > 5%% — проверьте таймаут/сеть",
			i+1, 100*float64(st.timeouts)/float64(st.attempts))
	}
	st.attempts, st.timeouts, st.windowStart = 0, 0, now
}

func (p *poller) publishList(i int, dev inverter, status uint8) {
	if !p.states[i].listed {
		return
	}
	publishItem(p.shm.Item(i), listMeta{
		Serial:   p.lastSeen[i],
		Addr:     net.JoinHostPort(dev.IP, itoa(dev.Port)),
		Kind:     kindCode(dev.Type),
		Status:   status,
		Gen:      p.gen,
		LastSeen: p.states[i].lastSeen,
	})
}

func (p *poller) publishTelemetry(i int, t telemetry) {
	if t.energySat && i < len(p.energyWarned) && !p.energyWarned[i] {
		p.energyWarned[i] = true
		p.lx.Warnf("инвертор #%d: energy_total переполнил u24 — сатурация 1,68 ГВт·ч", i+1)
	}
	c, ok := tsreg.Get("grid", 1)
	if !ok {
		return
	}
	payload, err := c.Encode(t.contractValues())
	if err != nil {
		p.lx.Warnf("encode grid: %v", err)
		return
	}
	// shm: contract payload + energy_today u16 + energy_total u24 (LE).
	payload = append(payload,
		byte(t.energyToday), byte(t.energyToday>>8),
		byte(t.energyTotal), byte(t.energyTotal>>8), byte(t.energyTotal>>16))
	p.shm.Slot(i).Publish(payload)
	if p.sink != nil {
		p.sink.publish(p.lastSeen[i], payload[:len(payload)-5])
	}
}

func kindCode(typ string) uint8 {
	if typ == "sofar" {
		return 1
	}
	return 0
}

func (p *poller) pollDevice(ctx context.Context, i int, dev inverter, knownSN string) (telemetry, string, error) {
	readTimeout := readTimeoutFor(dev.Type)
	idleWindow := idleWindowFor(dev.Type)
	// Новое TCP-подключение на каждый запрос — для обоих типов.
	cl := &solarman.Client{
		Address:     net.JoinHostPort(dev.IP, itoa(dev.Port)),
		DeviceSN:    dev.LoggerSN,
		Timeout:     readTimeout,
		DialTimeout: connectTimeout,
		ReadTimeout: readTimeout,
		IdleWindow:  idleWindow,
		MaxTotal:    readTimeout,
		Logf:        p.logf(),
	}
	var t telemetry
	var sn string
	switch dev.Type {
	case "deye":
		cl.Done = doneFull(0x3A)
		pdus, frames, err := cl.ReadRegistersDeye(ctx, 0x3B, 0x3A, 1)
		p.logExchange(i, dev, "deye main 0x3B+0x3A", pdus, frames, err)
		if err != nil {
			return t, "", classifyPoll(ctx, err, net.ParseIP(dev.IP), p.checkNeighbor)
		}
		regs, ok := fullRegs(pdus, 0x3B, 0x3A)
		if !ok {
			return t, "", badErr(errors.New("неполный блок Deye"))
		}
		t = mapDeye(regs)
		sn = knownSN
		if sn == "" {
			cl.Done = doneFull(0x0005)
			sp, sframes, err := cl.ReadRegistersDeye(ctx, 0x0003, 0x0005, 1)
			p.logExchange(i, dev, "deye SN 0x0003+0x0005", sp, sframes, err)
			if err != nil {
				return t, "", badErr(fmt.Errorf("серийник Deye: %w", err))
			}
			sregs, ok := fullRegs(sp, 0x0003, 0x0005)
			if !ok {
				return t, "", badErr(errors.New("серийник Deye: неполный ответ"))
			}
			vals := make([]uint16, 0, 5)
			for a := uint16(0x0003); a <= 0x0007; a++ {
				vals = append(vals, sregs[a])
			}
			sn = asciiFromRegs(vals, false)
			if sn == "" {
				return t, "", badErr(errors.New("серийник Deye: пустой"))
			}
		}
	case "sofar":
		cl.Done = nil // дочитываем до тишины: Sofar может прислать несколько кадров
		pdus, frames, err := cl.ReadRegistersDeyeFn(ctx, 0x0000, 0x0028, 1, 0x03)
		p.logExchange(i, dev, "sofar main 0x0000+0x0028 fn03", pdus, frames, err)
		if err != nil {
			return t, "", classifyPoll(ctx, err, net.ParseIP(dev.IP), p.checkNeighbor)
		}
		regs, ok := fullRegsLast(frames, 0x0000, 0x0028)
		if !ok {
			return t, "", badErr(errors.New("неполный блок Sofar"))
		}
		t = mapSofar(regs)
		sn = knownSN
		if sn == "" {
			cl.Done = nil
			sp, sframes, err := cl.ReadRegistersDeyeFn(ctx, 0x2000, 0x000E, 1, 0x04)
			p.logExchange(i, dev, "sofar SN 0x2000+0x000E fn04", sp, sframes, err)
			if err != nil {
				return t, "", badErr(fmt.Errorf("серийник Sofar: %w", err))
			}
			sregs, ok := fullRegsLast(sframes, 0x2000, 0x000E)
			if !ok {
				return t, "", badErr(errors.New("серийник Sofar: неполный ответ"))
			}
			vals := make([]uint16, 0, 14)
			for a := uint16(0x2000); a <= 0x200D; a++ {
				vals = append(vals, sregs[a])
			}
			sn = normalizeSofarSN(asciiFromRegs(vals, true))
			if sn == "" {
				return t, "", badErr(errors.New("серийник Sofar: пустой"))
			}
		}
	default:
		return t, "", badErr(fmt.Errorf("неизвестный тип %q", dev.Type))
	}
	return t, sn, nil
}

// doneFull — предикат раннего завершения: в накопленном ответе уже есть
// корректный PDU с нужным числом регистров.
func doneFull(count uint16) func([]byte) bool {
	return func(raw []byte) bool {
		for _, f := range solarman.SplitFrames(raw) {
			for _, p := range solarman.ParseModbusPDU(f.Payload) {
				if p.CRC == p.CRCCalc && len(p.Values) >= int(count) {
					return true
				}
			}
		}
		return false
	}
}

// fullRegs берёт первый корректный PDU, покрывающий диапазон start..start+count-1.
func fullRegs(pdus []solarman.ModbusPDU, start, count uint16) (map[uint16]uint16, bool) {
	for _, p := range pdus {
		if p.CRC != p.CRCCalc || len(p.Values) < int(count) {
			continue
		}
		m := make(map[uint16]uint16, count)
		for i := 0; i < int(count); i++ {
			m[start+uint16(i)] = p.Values[i]
		}
		return m, true
	}
	return nil, false
}

func badErr(err error) error {
	return &pollError{kind: errBad, err: err, readTimeout: errors.Is(err, solarman.ErrNoData)}
}

func kindName(k pollErrKind) string {
	switch k {
	case errBad:
		return "плохой ответ"
	case errNoRoute:
		return "нет сети"
	default:
		return "ошибка связи"
	}
}

// logExchange печатает в debug подробности ответа: кадры (control/serial/SN,
// checksum, payload) и найденные Modbus-PDU (CRC, длина, регистры). Вызывается
// на каждом обмене; при уровне info строки отбрасываются логгером.
func (p *poller) logExchange(i int, dev inverter, what string, pdus []solarman.ModbusPDU, frames []solarman.Frame, err error) {
	if p.lx == nil {
		return
	}
	name := dev.Name
	if name == "" {
		name = dev.IP
	}
	if err != nil {
		p.lx.Debugf("инвертор #%d %q (%s) %s: ошибка обмена: %v", i+1, name, net.JoinHostPort(dev.IP, itoa(dev.Port)), what, err)
	}
	for k, f := range frames {
		p.lx.Debugf("инвертор #%d %q %s: кадр[%d] control=0x%04X serial=%d sn=%d payload=%dB checksum_ok=%v valid=%v payload_hex=%x",
			i+1, name, what, k, f.ControlCode, f.Serial, f.DeviceSN, len(f.Payload), f.ChecksumOK, f.Valid, f.Payload)
	}
	for k, pd := range pdus {
		p.lx.Debugf("инвертор #%d %q %s: pdu[%d] fn=0x%02X offset=%d values=%d crc=0x%04X/0x%04X crc_ok=%v values_hex=%x",
			i+1, name, what, k, pd.Function, pd.Offset, len(pd.Values), pd.CRC, pd.CRCCalc, pd.CRC == pd.CRCCalc, pd.Values)
	}
	if err == nil && len(pdus) == 0 {
		p.lx.Debugf("инвертор #%d %q %s: корректных Modbus-PDU нет", i+1, name, what)
	}
}

// checkNeighbor возвращает признак «жив на L2» (netlink-таблица соседей).
func (p *poller) checkNeighbor(ctx context.Context, ip net.IP) (bool, error) {
	if p.neighbor != nil {
		return p.neighbor(ctx, ip)
	}
	return neighborReachable(ctx, ip)
}

// isDialTimeout — именно таймаут установления TCP-соединения (dial), а не
// произвольный сетевой таймаут. Только для него решает ARP-классификация.
func isDialTimeout(err error) bool {
	var oe *net.OpError
	return errors.As(err, &oe) && oe.Op == "dial" && oe.Timeout()
}

// classifyPoll переводит ошибку обмена в вид политики. Для connect timeout
// дополнительно смотрит таблицу соседей: NUD_REACHABLE → «в сети, но не
// ответил» (мягкий backoff), иначе → «спит совсем / нет на L2» (бан 10 мин).
// Ошибка/неоднозначность ARP трактуется как «нет в сети».
func classifyPoll(ctx context.Context, err error, ip net.IP, check neighborChecker) error {
	if isDialTimeout(err) {
		reachable := false
		if check != nil && ip != nil {
			if ok, aerr := check(ctx, ip); aerr == nil {
				reachable = ok
			}
		}
		if reachable {
			return &pollError{kind: errBad, err: err}
		}
		return &pollError{kind: errNoRoute, err: err}
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return &pollError{kind: errNoRoute, err: err}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		// Таймаут приёма (не dial) — плохой ответ, а не отдельный бан.
		return badErr(err)
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return &pollError{kind: errOther, err: err}
	}
	// Ошибка обмена/пустой ответ — плохой ответ, а не проблема подключения.
	return badErr(err)
}
