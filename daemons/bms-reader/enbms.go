package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// enBMS — тип EnBMS RS485 (PACE ASCII, host-RS485). docs/bms/enbms-rs485.md.
type enBMS struct{}

func (enBMS) TypeID() uint8  { return typeEnBMS }
func (enBMS) Name() string   { return "EnBMS-RS485" }
func (enBMS) Framing() uint8 { return framingNative }
func (enBMS) Speeds() []int  { return []int{19200} }

// MissThreshold: активная шина — снимаем слот только после 3 подряд неудач
// (устойчивость к помехам; arch §7/§10).
func (enBMS) MissThreshold() int { return 3 }

func (enBMS) MaxAddr() int { return enMaxADR }

const enMaxADR = 15

// Таймауты: опрос — по скорости порта (defTimeout/ioTimeout, arch §10),
// проба отсутствующего адреса — 200 мс.
const (
	enPollTimeout  = 500 * time.Millisecond
	enProbeTimeout = 200 * time.Millisecond
)

func u16be(b []byte, o int) uint16 { return binary.BigEndian.Uint16(b[o : o+2]) }

// defTimeout — штатный таймаут опроса: зависит от скорости serial-порта
// (arch §10), для TCP — 500 мс.
func defTimeout(t Transport) time.Duration {
	if x, ok := t.(interface{ DefaultTimeout() time.Duration }); ok {
		return x.DefaultTimeout()
	}
	return enPollTimeout
}

// enReq — отправка запроса и чтение ответа с таймаутом опроса; ADR ответа
// должен совпасть.
func enReq(t Transport, adr int, cid2 byte, info []byte) (paceFrame, error) {
	return enReqTimeout(t, adr, cid2, info, defTimeout(t))
}

func enReqTimeout(t Transport, adr int, cid2 byte, info []byte, to time.Duration) (paceFrame, error) {
	if err := t.Write(buildPace(adr, cid2, info)); err != nil {
		return paceFrame{}, err
	}
	f, err := readPaceTimeout(t, to)
	if err != nil {
		return f, err
	}
	if f.ADR != adr {
		return f, fmt.Errorf("enbms: ADR ответа %d != запрошенного %d", f.ADR, adr)
	}
	return f, nil
}

// Probe — проба кандидатов по SN (0xA4). docs/bms/enbms-rs485.md §3.
//
// candidates задаёт порядок обхода: весь диапазон 0..15 при поиске на новой
// шине либо один адрес-дырку при рескане. До первого найденного устройства
// промахи пропускаются (ищем первый ответивший адрес). После первого Found
// обход прекращается на первом промахе — дырка/чужое устройство означает конец
// непрерывного диапазона (arch §6.4). Таймаут пробы — 200 мс (arch §10).
func (enBMS) Probe(t Transport, candidates []int) (probeResult, error) {
	if len(candidates) == 0 {
		return probeResult{Kind: probeNoAnswer}, nil
	}
	var slots []probeSlot
	answeredNoSN := false
	for _, adr := range candidates {
		if adr < 0 || adr > enMaxADR {
			continue
		}
		f, err := enReqTimeout(t, adr, 0xA4, nil, enProbeTimeout)
		if err != nil {
			if len(slots) > 0 {
				break // после первого Found — конец диапазона
			}
			continue // до первого Found — ищем дальше
		}
		sn := strings.TrimSpace(string(f.Info))
		ok := f.RTN == 0 && len(sn) >= 8 && strings.HasPrefix(sn, "EMU")
		if !ok {
			answeredNoSN = true
			if len(slots) > 0 {
				break // чужое/без SN на хвосте — конец диапазона
			}
			continue // на новой шине чужое устройство не мешает искать наше
		}
		slots = append(slots, probeSlot{Addr: adr, Base: sn})
	}
	if len(slots) > 0 {
		return probeResult{Kind: probeFound, Slots: slots}, nil
	}
	if answeredNoSN {
		return probeResult{Kind: probeNotMine}, nil
	}
	return probeResult{Kind: probeNoAnswer}, nil
}

// Poll — телеметрия слота: 0x42 (+ 0x44 режим/ключи/баланс/alarm).
func (enBMS) Poll(t Transport, adr int) (bmscontract.Telemetry, error) {
	start := time.Now()
	retried := false
	f, err := enReq(t, adr, 0x42, nil)
	if err != nil {
		// ретрай один раз (ещё один таймаут по скорости, arch §10)
		retried = true
		lx.Infof("enbms: adr=%d 0x42 не ответил, повтор", adr)
		f, err = enReq(t, adr, 0x42, nil)
		if err != nil {
			lx.Debugf("enbms: adr=%d 0x42 не ответил после ретрая: %v", adr, err)
			return bmscontract.Telemetry{}, err
		}
	}
	lx.Debugf("enbms: adr=%d rtt=%s retry=%v rtn=%d", adr, time.Since(start).Round(time.Millisecond), retried, f.RTN)
	if f.RTN != 0 {
		return bmscontract.Telemetry{}, fmt.Errorf("enbms: 0x42 rtn=%d", f.RTN)
	}
	r, err := parseEn42(f.Info)
	if err != nil {
		return bmscontract.Telemetry{}, err
	}
	// 0x44 — по возможности (ошибка не отбрасывает телеметрию).
	if s, err := enReq(t, adr, 0x44, nil); err == nil && s.RTN == 0 {
		applyEn44(&r, s.Info)
	} else {
		r.Mode = modeFromCurrent(r.CurrentDA)
	}
	return r, nil
}

// parseEn42 разбирает payload 0x42 (Battery) в контракт.
func parseEn42(p []byte) (bmscontract.Telemetry, error) {
	var r bmscontract.Telemetry
	if len(p) < 4 {
		return r, fmt.Errorf("enbms 0x42: коротко %d", len(p))
	}
	n := int(p[2])
	if n < 1 || n > bmscontract.MaxCells {
		return r, fmt.Errorf("enbms 0x42: ячеек %d", n)
	}
	r.CellCount = uint8(n)
	o := 3
	for i := 0; i < n; i++ {
		r.CellsMV[i] = u16be(p, o)
		o += 2
	}
	if o >= len(p) {
		return r, fmt.Errorf("enbms 0x42: нет tempnum")
	}
	tn := int(p[o])
	o++
	if tn > bmscontract.MaxTemps {
		tn = bmscontract.MaxTemps
	}
	r.TempCount = uint8(tn)
	for i := 0; i < tn; i++ {
		if o+2 > len(p) {
			return r, fmt.Errorf("enbms 0x42: нет температур")
		}
		r.TempsDC[i] = int16(u16be(p, o)) - 2731 // 0.1 °C
		o += 2
	}
	if o+19 > len(p) {
		return r, fmt.Errorf("enbms 0x42: не хватает телеметрии")
	}
	r.CurrentDA = -int32(int16(u16be(p, o))) // 0.01А, знак инвертирован (разряд +)
	o += 2
	r.TotalCV = u16be(p, o)
	o += 2
	r.RemainingMAh = uint32(u16be(p, o)) * 10 // 0.01А·ч→мА·ч
	o += 2
	o++ // customerp
	r.CapMAh = uint32(u16be(p, o)) * 10
	o += 2
	r.SocDP = u16be(p, o)
	o += 2
	r.RatedMAh = uint32(u16be(p, o)) * 10
	o += 2
	r.Cycles = u16be(p, o)
	o += 2
	r.SohDP = u16be(p, o)
	o += 2
	r.PortCV = u16be(p, o)
	// мощность U·I
	u := float64(r.TotalCV) * 0.01
	i := float64(r.CurrentDA) * 0.01
	r.PowerW = int32(math.Round(u * i))
	return r, nil
}

// enModeCanon — Mode_Byte → канон (docs/bms/enbms-rs485.md §5).
func enModeCanon(mb byte) uint16 {
	switch mb {
	case 0x01:
		return bmscontract.ModeDischarge
	case 0x02:
		return bmscontract.ModeCharge
	case 0x04:
		return bmscontract.ModeMaintain
	case 0x08:
		return bmscontract.ModeFull
	case 0x20:
		return bmscontract.ModeShutdown
	default: // 0x00, 0x10
		return bmscontract.ModeIdle
	}
}

// applyEn44 разбирает 0x44 TeleState: режим, ключи, баланс, alarm (best-effort).
func applyEn44(r *bmscontract.Telemetry, p []byte) {
	if len(p) < 6 {
		return
	}
	cc := int(p[2])
	o := 3
	if o+cc > len(p) {
		return
	}
	cellGB := p[o : o+cc]
	o += cc
	if o >= len(p) {
		return
	}
	tc := int(p[o])
	o++
	if o+tc > len(p) {
		return
	}
	tempGB := p[o : o+tc]
	o += tc
	o += 2 // current/total-voltage state
	if o >= len(p) {
		return
	}
	ec := int(p[o])
	o++
	extLen := ec - 1
	if extLen < 0 || o+extLen > len(p) {
		extLen = len(p) - o - 1
	}
	if extLen < 0 {
		extLen = 0
	}
	ext := p[o : o+extLen]
	o += extLen
	if o < len(p) {
		r.Mode = enModeCanon(p[o]) // Mode_Byte
	}
	if r.Mode == 0 {
		r.Mode = modeFromCurrent(r.CurrentDA)
	}
	// alarm: ячейки 4 бита/ячейку (0..63)
	for i := 0; i < len(cellGB) && i < bmscontract.MaxCells; i++ {
		for b := 0; b < 4; b++ {
			if cellGB[i]&(1<<b) != 0 {
				setAlarmBit(&r.Alarm, i*4+b)
			}
		}
	}
	// температуры 4 бита/датчик (64..95)
	for i := 0; i < len(tempGB) && i < bmscontract.MaxTemps; i++ {
		for b := 0; b < 4; b++ {
			if tempGB[i]&(1<<b) != 0 {
				setAlarmBit(&r.Alarm, 64+i*4+b)
			}
		}
	}
	// Ext_Bit Byte0..5 → биты 100..146
	for by := 0; by < 6 && by < len(ext); by++ {
		for bit := 0; bit < 8; bit++ {
			if by == 5 && bit > 6 {
				continue // Byte5: биты 0..6
			}
			if ext[by]&(1<<uint(bit)) != 0 {
				setAlarmBit(&r.Alarm, 100+by*8+bit)
			}
		}
	}
	// ключи (Byte6) и балансировка (Byte7-8) — по возможности
	if len(ext) > 6 {
		k := ext[6]
		if k&0x02 != 0 {
			r.Flags |= bmscontract.FlagChargeMOS
		}
		if k&0x01 != 0 {
			r.Flags |= bmscontract.FlagDischargeMOS
		}
	}
	if len(ext) > 8 {
		r.BalanceMask = uint32(ext[7]) | uint32(ext[8])<<8
		if r.BalanceMask != 0 {
			r.Flags |= bmscontract.FlagBalancer
		}
	}
}

// setAlarmBit устанавливает бит alarm (номер от 0).
func setAlarmBit(a *[bmscontract.AlarmLen]byte, bit int) {
	if bit < 0 || bit >= bmscontract.AlarmLen*8 {
		return
	}
	a[bit/8] |= 1 << uint(bit%8)
}
