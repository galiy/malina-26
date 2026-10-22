package main

// Vektor/Growatt: синтетический блок состояния 0x0015..0x0029.
// Требования — review/bms-proxy-vektor-0x0015-analysis-20261022.md, docs/bms-proxy.md §5.1.

import (
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// Ограничения протокола.
const (
	vektorMaxCurrentA    = 327 // лимит 0x0019/0x0023 (A), влезает в signed 16-bit
	vektorMaxCurrent10mA = vektorMaxCurrentA * 100
	vektorCV10mV         = 5760 // 57.6 В для LFP 16S
)

// vektorSnapshot — всё, что нужно для ответа на блок состояния.
type vektorSnapshot struct {
	Cells          [vektorCellCount]uint16
	PackAvgMv      int
	MinMv          int
	MaxMv          int
	AvgCellMv      int // среднее по фактическим ячейкам (для типа АКБ)
	SocPct         int
	Current        int // 10 мА, signed, клэмпнут
	TempC          int
	MaxCurrent     int
	Remaining10mAh int
	Cap10mAh       int
	DeltaMv        int
	Cycles         int
	SohPct         int
	Warning        uint16 // биты 0..13
	Type           uint16 // 0=LFP, 1=NMC, 2=LTO (биты 14..15)
}

// buildVektorSnapshot считает синтетические ячейки и агрегаты по активным BMS.
// BMS с неполными ячейками отбрасываются (см. validCells).
func buildVektorSnapshot(ats []activeTelemetry) (vektorSnapshot, bool) {
	var snap vektorSnapshot
	valid := make([]activeTelemetry, 0, len(ats))
	tels := make([]bmscontract.Telemetry, 0, len(ats))
	for _, at := range ats {
		if _, ok := validCells(&at.Tel); !ok {
			continue
		}
		valid = append(valid, at)
		tels = append(tels, at.Tel)
	}
	st, ok := computePackStats(tels)
	if !ok {
		return snap, false
	}
	snap.Cells = st.cells
	snap.PackAvgMv = (st.sumPacks + st.n/2) / st.n
	snap.MinMv = st.minMV
	snap.MaxMv = st.maxMV
	snap.DeltaMv = st.maxMV - st.minMV
	if st.totalCells > 0 {
		snap.AvgCellMv = st.totalCellMV / st.totalCells
	}

	socSum, socN := 0, 0
	var currentSum int64
	var remainingSum, capSum int64
	tempMax := 0
	tempSet := false
	cyclesMax := 0
	sohSum, sohN := 0, 0
	var warning uint16
	for _, at := range valid {
		t := &at.Tel
		socSum += int(t.SocDP)
		socN++
		currentSum += int64(t.CurrentDA)
		tc := int(t.TempCount)
		if tc > bmscontract.MaxTemps {
			tc = bmscontract.MaxTemps
		}
		for i := 0; i < tc; i++ {
			v := int(t.TempsDC[i])
			if !tempSet || v > tempMax {
				tempMax = v
				tempSet = true
			}
		}
		remainingSum += int64(t.RemainingMAh)
		capSum += int64(t.CapMAh)
		if int(t.Cycles) > cyclesMax {
			cyclesMax = int(t.Cycles)
		}
		if t.SohDP > 0 {
			sohSum += int(t.SohDP)
			sohN++
		}
		warning |= warningsFor(at)
	}

	if socN > 0 {
		socDP := (socSum + socN/2) / socN
		snap.SocPct = clampInt((socDP+5)/10, 0, 100)
	}
	snap.Current = int(clampInt64(currentSum, -32767, 32767))
	if tempSet {
		snap.TempC = clampInt(divRound(tempMax, 10), -127, 127)
	}
	snap.MaxCurrent = vektorMaxCurrent10mA
	snap.Remaining10mAh = int(clampInt64(remainingSum/10, 0, 65535))
	snap.Cap10mAh = int(clampInt64(capSum/10, 0, 65535))
	snap.Cycles = clampInt(cyclesMax, 0, 65535)
	if sohN == 0 {
		snap.SohPct = 100 // SOH не отдают AntBMS; не показываем «мёртвую» батарею
	} else {
		sohDP := (sohSum + sohN/2) / sohN
		snap.SohPct = clampInt((sohDP+5)/10, 0, 100)
	}
	snap.Warning = warning & 0x3FFF
	snap.Type = updateBatteryType(snap.AvgCellMv, snap.SocPct)
	return snap, true
}

// vektorStatusRegisters раскладывает snapshot в 21 регистр 0x0015..0x0029.
func vektorStatusRegisters(s vektorSnapshot) [vektorStatusCount]uint16 {
	var r [vektorStatusCount]uint16
	r[0] = uint16(s.SocPct)
	r[1] = uint16((s.PackAvgMv + 5) / 10) // 10 мВ
	r[2] = uint16(int16(s.Current))       // signed 10 мА
	r[3] = uint16(int16(s.TempC))         // signed °C
	r[4] = uint16(s.MaxCurrent)
	r[5] = uint16(s.Remaining10mAh)
	r[6] = uint16(s.Cap10mAh)
	r[7] = 0                 // YW/FW
	r[8] = uint16(s.DeltaMv) // 1 мВ
	r[9] = uint16(s.Cycles)
	r[10] = 0 // Box ID
	r[11] = uint16(s.SohPct)
	r[12] = vektorCV10mV
	r[13] = s.Warning | (s.Type << 14)
	r[14] = uint16(s.MaxCurrent)
	r[15] = 0 // Extended Error
	r[16] = uint16(s.MaxMv)
	r[17] = uint16(s.MinMv)
	r[18] = 2 // max cell number, 1-based (ячейка 2)
	r[19] = 1 // min cell number, 1-based (ячейка 1)
	r[20] = uint16(vektorCellCount)
	return r
}

// vektorStatusReply читает активные BMS и собирает ответ на блок состояния.
// qty — сколько регистров запросил MAP (16 для старой прошивки, 21 для новой).
// nil — активных BMS нет или qty вне 1..21 (молчим).
func vektorStatusReply(shmPath string, slave byte, qty int) []byte {
	if qty <= 0 || qty > vektorStatusCount {
		return nil
	}
	ats := readActiveTelemetries(readShm(shmPath), time.Now())
	snap, ok := buildVektorSnapshot(ats)
	if !ok {
		return nil
	}
	regs := vektorStatusRegisters(snap)
	return buildVektorRegisterResponse(slave, regs[:qty])
}

// --- тип батареи ---------------------------------------------------------

var vektorTypeState struct {
	mu  sync.Mutex
	typ uint16 // 0=LFP, 1=NMC, 2=LTO; холодный старт — LFP
}

// decideBatteryType — эвристика по среднему напряжению ячейки и SOC.
// В неоднозначной зоне (низкий SOC, Vc 3.0..3.4) возвращает текущий тип.
func decideBatteryType(cur uint16, avgCellMv, socPct int) uint16 {
	switch {
	case avgCellMv < 2900:
		return 2
	case avgCellMv > 3900:
		return 1
	case socPct < 85 && avgCellMv > 3550:
		return 1
	case socPct < 10 && avgCellMv > 3400:
		return 1
	case socPct >= 85 && avgCellMv <= 3650:
		return 0
	case socPct >= 10 && avgCellMv <= 3500:
		return 0
	}
	return cur
}

// updateBatteryType хранит тип только в памяти (на диск ничего не пишем).
func updateBatteryType(avgCellMv, socPct int) uint16 {
	vektorTypeState.mu.Lock()
	defer vektorTypeState.mu.Unlock()
	vektorTypeState.typ = decideBatteryType(vektorTypeState.typ, avgCellMv, socPct)
	return vektorTypeState.typ
}

// --- warning-маппинг -----------------------------------------------------

func alarmBit(a [bmscontract.AlarmLen]byte, idx int) bool {
	if idx < 0 || idx >= bmscontract.AlarmLen*8 {
		return false
	}
	return a[idx/8]&(1<<uint(idx%8)) != 0
}

// alarmField — значение поля ширины w (LSB-first) с позиции base.
func alarmField(a [bmscontract.AlarmLen]byte, base, w int) int {
	v := 0
	for k := 0; k < w; k++ {
		if alarmBit(a, base+k) {
			v |= 1 << uint(k)
		}
	}
	return v
}

// warningsFor — биты Growatt 0..13 для одной BMS по её типу.
func warningsFor(at activeTelemetry) uint16 {
	switch at.TypeID {
	case 1: // AntBMS
		return antWarnings(&at.Tel)
	case 2: // EnBMS
		return enbmsWarnings(&at.Tel)
	}
	return 0
}

func antWarnings(t *bmscontract.Telemetry) uint16 {
	charge := alarmField(t.Alarm, 0, 4)
	discharge := alarmField(t.Alarm, 4, 4)
	bal := alarmField(t.Alarm, 8, 4)
	var w uint16
	if charge == 2 || charge == 5 {
		w |= 1 << 2 // pack OV
	}
	if discharge == 2 || discharge == 5 {
		w |= 1 << 3 // pack UV
	}
	if discharge == 3 || discharge == 8 || discharge == 12 {
		w |= 1 << 4 // discharge OC / short
	}
	if charge == 3 || charge == 8 {
		w |= 1 << 5 // charge OC
	}
	if discharge == 6 {
		w |= 1 << 6 // discharge overtemp
	}
	if charge == 6 {
		w |= 1 << 8 // charge overtemp
	}
	if charge == 7 || charge == 10 || discharge == 7 || discharge == 10 || bal == 3 || bal == 10 {
		w |= 1 << 10 // MOS/power overtemp
	}
	return w
}

func enbmsWarnings(t *bmscontract.Telemetry) uint16 {
	var w uint16
	// Ячейки: 16 × 4 бита (HVwarn, OVprot, LVwarn, UVprot).
	for i := 0; i < 16; i++ {
		base := i * 4
		if alarmBit(t.Alarm, base) || alarmBit(t.Alarm, base+1) {
			w |= 1 << 0
		}
		if alarmBit(t.Alarm, base+2) || alarmBit(t.Alarm, base+3) {
			w |= 1 << 1
		}
	}
	// Ext_Bit Byte1: cell/pack OV/UV.
	if alarmBit(t.Alarm, 108) || alarmBit(t.Alarm, 109) {
		w |= 1 << 0
	}
	if alarmBit(t.Alarm, 110) || alarmBit(t.Alarm, 111) {
		w |= 1 << 1
	}
	if alarmBit(t.Alarm, 112) || alarmBit(t.Alarm, 113) || alarmBit(t.Alarm, 140) {
		w |= 1 << 2
	}
	if alarmBit(t.Alarm, 114) || alarmBit(t.Alarm, 115) {
		w |= 1 << 3
	}
	// Ext_Bit Byte4: токи/КЗ.
	if alarmBit(t.Alarm, 134) || alarmBit(t.Alarm, 135) || alarmBit(t.Alarm, 137) || alarmBit(t.Alarm, 139) {
		w |= 1 << 4
	}
	if alarmBit(t.Alarm, 132) || alarmBit(t.Alarm, 133) || alarmBit(t.Alarm, 98) {
		w |= 1 << 5
	}
	// Ext_Bit Byte2: температуры заряда/разряда.
	if alarmBit(t.Alarm, 120) || alarmBit(t.Alarm, 121) {
		w |= 1 << 6
	}
	if alarmBit(t.Alarm, 122) || alarmBit(t.Alarm, 123) {
		w |= 1 << 7
	}
	if alarmBit(t.Alarm, 116) || alarmBit(t.Alarm, 117) {
		w |= 1 << 8
	}
	if alarmBit(t.Alarm, 118) || alarmBit(t.Alarm, 119) {
		w |= 1 << 9
	}
	// Ext_Bit Byte3: силовая часть/среда.
	if alarmBit(t.Alarm, 128) || alarmBit(t.Alarm, 129) || alarmBit(t.Alarm, 97) {
		w |= 1 << 10
	}
	if alarmBit(t.Alarm, 124) || alarmBit(t.Alarm, 125) {
		w |= 1 << 11
	}
	if alarmBit(t.Alarm, 126) || alarmBit(t.Alarm, 127) {
		w |= 1 << 12
	}
	// Ext_Bit Byte5: остаток/запрет заряда при низком U.
	if alarmBit(t.Alarm, 142) || alarmBit(t.Alarm, 143) || alarmBit(t.Alarm, 144) {
		w |= 1 << 13
	}
	return w
}

// --- арифметика ----------------------------------------------------------

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// divRound — деление с округлением half away from zero.
func divRound(v, d int) int {
	if v >= 0 {
		return (v + d/2) / d
	}
	return (v - d/2) / d
}
