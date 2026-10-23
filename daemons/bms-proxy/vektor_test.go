package main

import (
	"testing"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

func setAlarmBit(t *bmscontract.Telemetry, idx int) {
	t.Alarm[idx/8] |= 1 << uint(idx%8)
}

func setAlarmField(t *bmscontract.Telemetry, base, w, v int) {
	for k := 0; k < w; k++ {
		if v&(1<<uint(k)) != 0 {
			setAlarmBit(t, base+k)
		}
	}
}

func resetVektorType() {
	vektorTypeState.mu.Lock()
	vektorTypeState.typ = 0
	vektorTypeState.mu.Unlock()
}

func TestIsVektorStatusRequest(t *testing.T) {
	req := withCRC([]byte{0x01, 0x03, 0x00, 0x15, 0x00, 0x15})
	start, _, n, ok := findModbusRequest(req)
	if !ok {
		t.Fatal("запрос состояния не распознан")
	}
	if !isVektorStatusRequest(req, start, n) {
		t.Fatal("запрос состояния не принят")
	}
	if !isVektorKnownRequest(req, start, n) {
		t.Fatal("запрос состояния не считается известным")
	}
	// Старая прошивка MAP: 16 регистров с 0x0015.
	req16 := withCRC([]byte{0x01, 0x03, 0x00, 0x15, 0x00, 0x10})
	start, _, n, ok = findModbusRequest(req16)
	if !ok {
		t.Fatal("запрос состояния x16 не распознан")
	}
	if !isVektorStatusRequest(req16, start, n) || !isVektorKnownRequest(req16, start, n) {
		t.Fatal("запрос состояния x16 не принят")
	}
	if got := vektorRequestQty(req16, start); got != 16 {
		t.Fatalf("qty x16 = %d", got)
	}
	// Ячеечный запрос — тоже известный.
	cell := []byte{0x01, 0x03, 0x00, 0x71, 0x00, 0x10, 0x14, 0x1D}
	start, _, n, ok = findModbusRequest(cell)
	if !ok || !isVektorKnownRequest(cell, start, n) {
		t.Fatal("запрос ячеек не считается известным")
	}
	// Другой регистр — не наш.
	other := withCRC([]byte{0x01, 0x03, 0x00, 0x81, 0x00, 0x10})
	start, _, n, ok = findModbusRequest(other)
	if !ok {
		t.Fatal("0x0081 не распознан")
	}
	if isVektorKnownRequest(other, start, n) {
		t.Fatal("0x0081 не должен считаться известным")
	}
}

func TestBuildVektorStatusResponse(t *testing.T) {
	regs := make([]uint16, vektorStatusCount)
	for i := range regs {
		regs[i] = uint16(0x100 + i)
	}
	b := buildVektorRegisterResponse(1, regs)
	if len(b) != 3+2*vektorStatusCount+2 {
		t.Fatalf("длина ответа = %d", len(b))
	}
	if b[0] != 1 || b[1] != modbusFnReadHolding || b[2] != 2*vektorStatusCount {
		t.Fatalf("заголовок: % X", b[:3])
	}
	if modbusCRC(b) != 0 {
		t.Fatalf("CRC ответа неверен")
	}
	for i, want := range regs {
		got := uint16(b[3+2*i])<<8 | uint16(b[4+2*i])
		if got != want {
			t.Fatalf("reg %d = 0x%04X, ожидалось 0x%04X", i, got, want)
		}
	}
}

// TestBuildVektorStatusResponse16 — старая прошивка MAP: 16 регистров.
func TestBuildVektorStatusResponse16(t *testing.T) {
	regs := make([]uint16, vektorStatusQtyAlt)
	for i := range regs {
		regs[i] = uint16(0x200 + i)
	}
	b := buildVektorRegisterResponse(1, regs)
	if len(b) != 3+2*vektorStatusQtyAlt+2 {
		t.Fatalf("длина ответа x16 = %d", len(b))
	}
	if b[0] != 1 || b[1] != modbusFnReadHolding || b[2] != 2*vektorStatusQtyAlt {
		t.Fatalf("заголовок x16: % X", b[:3])
	}
	if modbusCRC(b) != 0 {
		t.Fatalf("CRC ответа x16 неверен")
	}
	for i, want := range regs {
		got := uint16(b[3+2*i])<<8 | uint16(b[4+2*i])
		if got != want {
			t.Fatalf("reg %d x16 = 0x%04X, ожидалось 0x%04X", i, got, want)
		}
	}
}

func TestVektorStatusRegisters(t *testing.T) {
	resetVektorType()
	mk := func(vals ...uint16) bmscontract.Telemetry {
		var tel bmscontract.Telemetry
		tel.CellCount = uint8(len(vals))
		copy(tel.CellsMV[:], vals)
		return tel
	}
	t1 := mk(3200, 3250, 3300, 3350)
	t1.SocDP = 500
	t1.CurrentDA = 1000
	t1.TempCount = 1
	t1.TempsDC[0] = 255
	t1.RemainingMAh = 100000
	t1.CapMAh = 200000
	t1.RatedMAh = 180000
	t1.Cycles = 10
	t1.SohDP = 900
	t2 := mk(3150, 3200, 3250, 3300)
	t2.SocDP = 600
	t2.CurrentDA = 2000
	t2.TempCount = 1
	t2.TempsDC[0] = 305
	t2.RemainingMAh = 100000
	t2.CapMAh = 200000
	t2.RatedMAh = 0
	t2.Cycles = 20
	t2.SohDP = 950

	ats := []activeTelemetry{
		{TypeID: 2, Tel: t1},
		{TypeID: 2, Tel: t2},
	}
	snap, ok := buildVektorSnapshot(ats)
	if !ok {
		t.Fatal("snapshot не собран")
	}
	r := vektorStatusRegisters(snap)
	want := [vektorStatusCount]uint16{
		55,    // SOC (50.0+60.0)/2 = 55%
		1300,  // PAvg 13000 мВ / 10
		3000,  // ток 1000+2000 (0.01 А)
		31,    // max 30.5 °C → 31
		32700, // лимит 327 А
		20000, // RM 200000 мА·ч / 10
		38000, // FCC: RatedMAh 180000 + fallback CapMAh 200000, /10
		0,
		200, // delta 3350-3150 мВ
		20,  // cycles max
		0,
		93,   // SOH (90.0+95.0)/2 = 92.5 → 93
		5760, // CV 57.6 В
		0,    // warning + type LFP
		32700,
		0,
		3350,
		3150,
		2,
		1,
		16,
	}
	for i := range want {
		if r[i] != want[i] {
			t.Fatalf("reg %d = %d, ожидалось %d", i, r[i], want[i])
		}
	}
}

func TestVektorCVForType(t *testing.T) {
	cases := map[uint16]uint16{0: 5760, 1: 6720, 2: 4480, 3: 5760}
	for typ, want := range cases {
		if got := vektorCVForType(typ); got != want {
			t.Fatalf("type=%d CV=%d, ожидалось %d", typ, got, want)
		}
	}
	var s vektorSnapshot
	s.Type = 1
	if got := vektorStatusRegisters(s)[12]; got != 6720 {
		t.Fatalf("NMC status CV=%d", got)
	}
	s.Type = 2
	if got := vektorStatusRegisters(s)[12]; got != 4480 {
		t.Fatalf("LTO status CV=%d", got)
	}
}

func TestVektorClamps(t *testing.T) {
	resetVektorType()
	var tel bmscontract.Telemetry
	tel.CellCount = 4
	for i := 0; i < 4; i++ {
		tel.CellsMV[i] = 3300
	}
	tel.SocDP = 500
	tel.CurrentDA = 40000 // 400 А > 327.67 А
	tel.TempCount = 1
	tel.TempsDC[0] = 2000 // 200 °C
	tel.RemainingMAh = 800000
	tel.CapMAh = 800000
	tel.Cycles = 65535
	tel.SohDP = 1000
	snap, ok := buildVektorSnapshot([]activeTelemetry{{TypeID: 2, Tel: tel}})
	if !ok {
		t.Fatal("snapshot не собран")
	}
	if snap.Current != 32767 {
		t.Fatalf("ток = %d, ожидался клэмп 32767", snap.Current)
	}
	if snap.TempC != 127 {
		t.Fatalf("температура = %d, ожидался клэмп 127", snap.TempC)
	}
	if snap.Remaining10mAh != 65535 || snap.Cap10mAh != 65535 {
		t.Fatalf("ёмкости = %d/%d, ожидался клэмп 65535", snap.Remaining10mAh, snap.Cap10mAh)
	}
	if snap.Cycles != 65535 {
		t.Fatalf("циклы = %d, ожидался клэмп 65535", snap.Cycles)
	}
}

func TestBatteryTypeHeuristic(t *testing.T) {
	cases := []struct {
		cur  uint16
		vc   int
		soc  int
		want uint16
	}{
		{0, 3250, 50, 0},  // LFP
		{0, 3600, 50, 1},  // NMC
		{0, 4100, 50, 1},  // NMC высокое
		{0, 2500, 50, 2},  // LTO
		{1, 3300, 5, 1},   // неоднозначно, держим NMC
		{0, 3600, 100, 0}, // LFP полный
		{1, 3600, 100, 0}, // LFP полный сбрасывает NMC
		{0, 3450, 5, 1},   // 3.45 В при SOC<10 — NMC
	}
	for i, c := range cases {
		if got := decideBatteryType(c.cur, c.vc, c.soc); got != c.want {
			t.Fatalf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

func TestWarningsEnBMS(t *testing.T) {
	var tel bmscontract.Telemetry
	// Все 14 битов Growatt через прямые источники EnBMS.
	setAlarmBit(&tel, 0)   // cell 1 HVwarn → bit0
	setAlarmBit(&tel, 6)   // cell 2 LVwarn → bit1
	setAlarmBit(&tel, 112) // pack OV → bit2
	setAlarmBit(&tel, 114) // pack UV → bit3
	setAlarmBit(&tel, 134) // discharge OC → bit4
	setAlarmBit(&tel, 132) // charge OC → bit5
	setAlarmBit(&tel, 120) // discharge overtemp → bit6
	setAlarmBit(&tel, 122) // discharge undertemp → bit7
	setAlarmBit(&tel, 116) // charge overtemp → bit8
	setAlarmBit(&tel, 118) // charge undertemp → bit9
	setAlarmBit(&tel, 128) // power part → bit10
	setAlarmBit(&tel, 124) // env overtemp → bit11
	setAlarmBit(&tel, 126) // env undertemp → bit12
	setAlarmBit(&tel, 142) // remaining capacity → bit13
	w := warningsFor(activeTelemetry{TypeID: 2, Tel: tel})
	if w != 0x3FFF {
		t.Fatalf("warning = 0x%04X, ожидалось 0x3FFF", w)
	}
}

func TestWarningsAntBMS(t *testing.T) {
	var tel bmscontract.Telemetry
	setAlarmField(&tel, 0, 4, 6) // charge MOS 6 → bit8
	setAlarmField(&tel, 4, 4, 2) // discharge MOS 2 → bit3
	setAlarmField(&tel, 8, 4, 3) // balancer 3 → bit10
	w := warningsFor(activeTelemetry{TypeID: 1, Tel: tel})
	want := uint16(1<<3 | 1<<8 | 1<<10)
	if w != want {
		t.Fatalf("warning = 0x%04X, ожидалось 0x%04X", w, want)
	}
}

func TestVektorSnapshotEmpty(t *testing.T) {
	if _, ok := buildVektorSnapshot(nil); ok {
		t.Fatal("без BMS snapshot не должен собираться")
	}
	var bad bmscontract.Telemetry
	bad.CellCount = 1 // ячейка 0 мВ
	if _, ok := buildVektorSnapshot([]activeTelemetry{{TypeID: 2, Tel: bad}}); ok {
		t.Fatal("BMS с нулевой ячейкой не должна учитываться")
	}
}
