package main

import (
	"encoding/binary"
	"testing"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// buildAntFrame собирает валидный 140-байтный кадр с заданными полями.
func buildAntFrame(cells []uint16, currentDA10 int32, soc uint8, capUAh uint32) []byte {
	f := make([]byte, antFrameLen)
	copy(f[0:4], antMarker)
	for i, mv := range cells {
		binary.BigEndian.PutUint16(f[6+2*i:], mv)
	}
	binary.BigEndian.PutUint32(f[70:], uint32(currentDA10))
	f[74] = soc
	binary.BigEndian.PutUint32(f[75:], capUAh)
	binary.BigEndian.PutUint32(f[79:], capUAh/2)
	f[103] = 1 // charge_mos
	f[105] = 0
	binary.BigEndian.PutUint32(f[111:], 1234)
	for i := 0; i < 6; i++ {
		binary.BigEndian.PutUint16(f[91+2*i:], uint16(25))
	}
	f[123] = byte(len(cells))
	var sum int
	for _, x := range f[4:138] {
		sum += int(x)
	}
	binary.BigEndian.PutUint16(f[138:], uint16(sum))
	return f
}

func TestParseAntFrame(t *testing.T) {
	f := buildAntFrame([]uint16{3300, 3301, 3302}, 100, 50, 320_000_000)
	tel, err := parseAntFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	if tel.CellCount != 3 || tel.CellsMV[2] != 3302 {
		t.Fatalf("ячейки: %+v", tel.CellsMV)
	}
	if tel.CurrentDA != 1000 { // 100×0.1А → 0.01А
		t.Fatalf("current_da = %d", tel.CurrentDA)
	}
	if tel.SocDP != 500 {
		t.Fatalf("soc_dp = %d", tel.SocDP)
	}
	if tel.CapMAh != 320000 {
		t.Fatalf("cap_mah = %d", tel.CapMAh)
	}
	if tel.Mode != modeDischargeCode() {
		t.Fatalf("mode = %#x (ожидался разряд)", tel.Mode)
	}
	if tel.Flags&1 == 0 { // charge_mos
		t.Fatal("flags: charge_mos не выставлен")
	}
}

func TestAntProbe(t *testing.T) {
	f := buildAntFrame([]uint16{3300, 3301, 3302, 3303}, -50, 80, 160_000_000)
	tr := &fakeTransport{rb: f}
	res, err := antBMS{}.Probe(tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != probeFound || len(res.Slots) != 1 {
		t.Fatalf("probe: %+v", res)
	}
	if res.Slots[0].Base != "AntBMS-DSP-160" {
		t.Fatalf("base = %q", res.Slots[0].Base)
	}
}

func modeDischargeCode() uint16 { return 0x01 }

// setAntCRC пересчитывает CRC кадра ANT (сумма байт 4..137).
func setAntCRC(f []byte) {
	var sum int
	for _, x := range f[4:138] {
		sum += int(x)
	}
	binary.BigEndian.PutUint16(f[138:], uint16(sum))
}

// TestAntStreamChunked — эмуляция пассивного потока AntBMS: кадр приходит
// раздробленными порциями (по 7 байт), с мусорным префиксом. readAntFrame
// должен собрать ровно один валидный кадр за выборку (кадры в потоке избыточны,
// лишнее за кадром не значимо).
func TestAntStreamChunked(t *testing.T) {
	f := buildAntFrame([]uint16{3300, 3301}, 100, 60, 160_000_000)
	// мусорный префикс (не маркер) + кадр + «хвост» следующего кадра
	stream := append([]byte{0x00, 0x11, 0xAA, 0x55, 0x01, 0x02}, f...)
	stream = append(stream, f[:20]...) // обрывок следующего кадра

	tr := &chunkTransport{rb: stream, chunk: 7}
	g, err := readAntFrame(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(g, f) {
		t.Fatal("кадр извлечён неверно")
	}
	tel, err := parseAntFrame(g)
	if err != nil || tel.CellCount != 2 || tel.SocDP != 600 {
		t.Fatalf("разбор: %+v (%v)", tel, err)
	}
}

// TestAntAlarm — упаковка alarm: коды MOS (4 бита) + system log (16 бит).
func TestAntAlarm(t *testing.T) {
	f := buildAntFrame([]uint16{3300}, 0, 50, 100_000_000)
	f[103] = 2                                  // charge_mos: Overvoltage protection
	f[104] = 0                                  // discharge_mos: Off
	f[105] = 1                                  // balancer: Exceeds limit equilibrium
	binary.BigEndian.PutUint16(f[136:], 0x8001) // system log: bit0 (Status) + bit15 (chg/dis)
	setAntCRC(f)

	tel, err := parseAntFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	if tel.Alarm[0] != 0x02 { // bits0..3 charge=2, bits4..7 discharge=0
		t.Fatalf("Alarm[0]=%#x, ожидалось 0x02", tel.Alarm[0])
	}
	if tel.Alarm[1] != 0x11 { // bits8..11 balancer=1, bits12..15 syslog low nibble=1
		t.Fatalf("Alarm[1]=%#x, ожидалось 0x11", tel.Alarm[1])
	}
	if tel.Alarm[3]&0x08 == 0 { // syslog bit15 → alarm bit 27 (byte3 bit3)
		t.Fatalf("Alarm[3]=%#x, ожидался бит3 (syslog bit15)", tel.Alarm[3])
	}
	for i := 4; i < bmscontract.AlarmLen; i++ {
		if tel.Alarm[i] != 0 {
			t.Fatalf("Alarm[%d]=%#x, ожидался 0", i, tel.Alarm[i])
		}
	}
	if tel.Flags&1 == 0 { // Flags по «≠0» сохраняется
		t.Fatal("flags: charge_mos должен быть выставлен (≠0)")
	}
}

// TestAntStreamTakesLastFrame — при нескольких кадрах в буфере readAntFrame
// обязан вернуть последний (свежий), а не первый (голову FIFO).
func TestAntStreamTakesLastFrame(t *testing.T) {
	f1 := buildAntFrame([]uint16{3300, 3301}, 500, 60, 160_000_000)
	f2 := buildAntFrame([]uint16{3300, 3301}, 1200, 59, 160_000_000)
	tr := &fakeTransport{rb: append(append([]byte{}, f1...), f2...)}
	g, err := readAntFrame(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEq(g, f2) {
		t.Fatalf("взят не последний кадр: current=%d", int32(binary.BigEndian.Uint32(g[70:74])))
	}
}

// TestValidateAntCellsRejectsImplausibleCell — мВ вне 1.0..5.0 В отбрасываются
// при Probe; в Poll такой кадр (авария) должен публиковаться.
func TestValidateAntCellsRejectsImplausibleCell(t *testing.T) {
	allBad := buildAntFrame([]uint16{900, 900}, 100, 50, 160_000_000)
	if err := validateAntCells(allBad, 2); err == nil {
		t.Fatal("ожидалась ошибка: все ячейки вне 1.0..5.0 В")
	}
	// Аварийный кадр с одной мёртвой ячейкой должен публиковаться.
	oneBad := buildAntFrame([]uint16{3300, 900}, 100, 50, 160_000_000)
	if err := validateAntCells(oneBad, 2); err != nil {
		t.Fatalf("одна мёртвая ячейка не должна отбрасывать кадр: %v", err)
	}
	if _, err := parseAntFrame(oneBad); err != nil {
		t.Fatalf("Poll не должен отбрасывать аварийный кадр: %v", err)
	}
}

// flushTransport считает вызовы Flush (проверка Poll → Flush перед чтением).
type flushTransport struct {
	fakeTransport
	flushes int
}

func (f *flushTransport) Flush() { f.flushes++ }

// TestAntPollFlushesAndTakesLatest — Poll сбрасывает FIFO и публикует последний
// кадр, а не первый.
func TestAntPollFlushesAndTakesLatest(t *testing.T) {
	f1 := buildAntFrame([]uint16{3300, 3301}, 500, 60, 160_000_000)
	f2 := buildAntFrame([]uint16{3300, 3301}, 1200, 59, 160_000_000)
	tr := &flushTransport{fakeTransport: fakeTransport{rb: append(append([]byte{}, f1...), f2...)}}
	tel, err := antBMS{}.Poll(tr, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tr.flushes != 1 {
		t.Fatalf("Flush вызван %d раз, ожидался 1", tr.flushes)
	}
	if tel.CurrentDA != 12000 {
		t.Fatalf("current_da=%d, ожидался последний кадр (12000)", tel.CurrentDA)
	}
}
