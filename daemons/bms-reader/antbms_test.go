package main

import (
	"encoding/binary"
	"testing"
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
	res, err := antBMS{}.Probe(tr)
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
