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
	deyeRead       = time.Second
	sofarRead      = 3 * time.Second
)

type pollErrKind int

const (
	errBad pollErrKind = iota + 1
	errNoRoute
	errTimeout
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
	clients      []*solarman.Client
	gen          uint64
	states       []devState
	types        []string
	lastSeen     []string // известный inverter_sn по слоту
	lastRound    time.Duration
	energyWarned []bool
	sink         *tsdbSink
	lx           *logx.Logger
}

func newPoller(shm *Shm, sink *tsdbSink, lx *logx.Logger) *poller {
	return &poller{shm: shm, sink: sink, lx: lx}
}

func (p *poller) logf() func(string, ...any) {
	if p.lx == nil {
		return nil
	}
	return p.lx.Debugf
}

// newClient создаёт клиента под тип инвертора. Sofar держит одно TCP-соединение
// между опросами (часть логгеров плохо переносит частые переподключения); Deye
// по-прежнему открывает свежее соединение на каждый запрос.
func (p *poller) newClient(dev inverter) *solarman.Client {
	readTimeout := deyeRead
	cl := &solarman.Client{
		Address:     net.JoinHostPort(dev.IP, itoa(dev.Port)),
		DeviceSN:    dev.LoggerSN,
		DialTimeout: connectTimeout,
		Logf:        p.logf(),
	}
	if dev.Type == "sofar" {
		readTimeout = sofarRead
		cl.Persistent = true
		cl.IdleWindow = 400 * time.Millisecond
	}
	cl.Timeout = readTimeout
	cl.ReadTimeout = readTimeout
	if cl.IdleWindow == 0 {
		cl.IdleWindow = readTimeout
	}
	cl.MaxTotal = readTimeout
	return cl
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
	for _, c := range p.clients {
		c.Close()
	}
	p.clients = make([]*solarman.Client, len(cfg.Inverters))
	for i, d := range cfg.Inverters {
		p.clients[i] = p.newClient(d)
	}
	p.states = make([]devState, len(cfg.Inverters))
	p.types = make([]string, len(cfg.Inverters))
	for i, d := range cfg.Inverters {
		p.types[i] = d.Type
	}
	p.lastSeen = make([]string, len(cfg.Inverters))
	p.energyWarned = make([]bool, len(cfg.Inverters))
	p.lastRound = 0
	for i := 0; i < gridSlots; i++ {
		p.shm.Item(i).Release()
		p.shm.Slot(i).Release()
	}
	for i := range cfg.Inverters {
		p.shm.Slot(i).PrepareGen(p.gen)
	}
}

func (p *poller) clear() {
	for _, c := range p.clients {
		c.Close()
	}
	p.clients = nil
	for i := 0; i < gridSlots; i++ {
		p.shm.Item(i).Release()
		p.shm.Slot(i).Release()
	}
}

func (p *poller) staleThreshold(period time.Duration) time.Duration {
	base := period
	if p.lastRound > base {
		base = p.lastRound
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

// pollOnce — один последовательный круг. Возвращает длительность круга.
func (p *poller) pollOnce(ctx context.Context, cfg settings) time.Duration {
	start := time.Now()
	for i := range cfg.Inverters {
		select {
		case <-ctx.Done():
			return time.Since(start)
		default:
		}
		if !cfg.Inverters[i].Valid {
			continue
		}
		st := &p.states[i]
		now := time.Now()
		if now.Before(st.banUntil) || now.Before(st.nextTry) {
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, now, p.staleThreshold(time.Duration(cfg.PollPeriod)*time.Second)))
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
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, time.Now(), p.staleThreshold(time.Duration(cfg.PollPeriod)*time.Second)))
			continue
		}
		p.success(i, t, sn)
		p.publishList(i, cfg.Inverters[i], statusOK)
	}
	d := time.Since(start)
	p.lx.Debugf("круг: %d устройств, %s", len(cfg.Inverters), d.Round(time.Millisecond))
	p.lastRound = d
	// обновить stale у уже опубликованных записей
	now := time.Now()
	stale := p.staleThreshold(time.Duration(cfg.PollPeriod) * time.Second)
	for i := range cfg.Inverters {
		if p.states[i].listed {
			p.publishList(i, cfg.Inverters[i], p.currentStatus(i, now, stale))
		}
	}
	return d
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
	case errTimeout:
		st.status = statusNoCon
		st.banUntil = now.Add(5 * time.Minute)
		st.banStatus = statusBan5
		st.nextTry = st.banUntil
		st.failCount = 0
		p.lx.Warnf("инвертор #%d: таймаут connect — бан 5 мин", i+1)
		return
	case errBad:
		st.status = statusBad
		// Sofar периодически не отвечает на отдельные запросы (TCP при этом
		// доступен). Не отправляем его в бан за «плохой ответ»: после двух
		// промахов подряд делаем паузу 1 минуту и продолжаем попытки.
		if i < len(p.types) && p.types[i] == "sofar" {
			st.failCount++
			if st.failCount >= 2 {
				st.nextTry = now.Add(time.Minute)
			}
			p.lx.Warnf("инвертор #%d: плохой ответ Sofar (%v), подряд=%d — без бана, пауза", i+1, err, st.failCount)
			return
		}
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
	readTimeout := deyeRead
	if dev.Type == "sofar" {
		readTimeout = sofarRead
	}
	logf := p.logf()
	var cl *solarman.Client
	if i >= 0 && i < len(p.clients) && p.clients[i] != nil {
		cl = p.clients[i]
		logf = p.logf()
		cl.Logf = logf
	} else {
		cl = &solarman.Client{
			Address:     net.JoinHostPort(dev.IP, itoa(dev.Port)),
			DeviceSN:    dev.LoggerSN,
			Timeout:     readTimeout,
			DialTimeout: connectTimeout,
			ReadTimeout: readTimeout,
			IdleWindow:  readTimeout,
			MaxTotal:    readTimeout,
			Logf:        logf,
		}
	}
	var t telemetry
	var sn string
	switch dev.Type {
	case "deye":
		cl.Done = doneFull(0x3A)
		pdus, frames, err := cl.ReadRegistersDeye(ctx, 0x3B, 0x3A, 1)
		p.logExchange(i, dev, "deye main 0x3B+0x3A", pdus, frames, err)
		if err != nil {
			return t, "", classifyPoll(err)
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
			return t, "", classifyPoll(err)
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
		return "нет маршрута"
	case errTimeout:
		return "таймаут"
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

func classifyPoll(err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &pollError{kind: errTimeout, err: err}
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return &pollError{kind: errNoRoute, err: err}
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return &pollError{kind: errOther, err: err}
	}
	// Ошибка обмена/пустой ответ — плохой ответ, а не проблема подключения.
	return badErr(err)
}
