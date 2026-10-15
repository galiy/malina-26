package main

import (
	"encoding/binary"
	"testing"
)

func be16(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }
func be16s(v int16) []byte { return be16(uint16(v)) }

// buildEn42 собирает синтетический payload 0x42 (Battery).
func buildEn42() []byte {
	p := []byte{0x00, 0x00, 0x02} // header + cellcount=2
	p = append(p, be16(3300)...)  // cell1
	p = append(p, be16(3301)...)  // cell2
	p = append(p, 0x01)           // tempnum=1
	p = append(p, be16(2981)...)  // 25.0 °C → (250+2731)
	p = append(p, be16s(-123)...) // current s16 → инверсия
	p = append(p, be16(5300)...)  // totalV (53.00 В)
	p = append(p, be16(16000)...) // leftcap (160.00 А·ч)
	p = append(p, 0x00)           // customerp
	p = append(p, be16(32000)...) // totalcap
	p = append(p, be16(500)...)   // soc (50.0 %)
	p = append(p, be16(32000)...) // ratedcap
	p = append(p, be16(42)...)    // cycles
	p = append(p, be16(990)...)   // soh (99.0 %)
	p = append(p, be16(5290)...)  // portV
	return p
}

func TestParseEn42(t *testing.T) {
	tel, err := parseEn42(buildEn42())
	if err != nil {
		t.Fatal(err)
	}
	if tel.CellCount != 2 || tel.CellsMV[1] != 3301 {
		t.Fatalf("ячейки: %+v", tel)
	}
	if tel.TempCount != 1 || tel.TempsDC[0] != 250 {
		t.Fatalf("температуры: %+v", tel.TempsDC)
	}
	if tel.CurrentDA != 123 {
		t.Fatalf("current_da = %d (ожидалось 123)", tel.CurrentDA)
	}
	if tel.TotalCV != 5300 || tel.PortCV != 5290 {
		t.Fatalf("напряжения: total=%d port=%d", tel.TotalCV, tel.PortCV)
	}
	if tel.RemainingMAh != 160000 || tel.CapMAh != 320000 || tel.RatedMAh != 320000 {
		t.Fatalf("ёмкости: rem=%d cap=%d rated=%d", tel.RemainingMAh, tel.CapMAh, tel.RatedMAh)
	}
	if tel.SocDP != 500 || tel.SohDP != 990 || tel.Cycles != 42 {
		t.Fatalf("soc/soh/cycles: %d/%d/%d", tel.SocDP, tel.SohDP, tel.Cycles)
	}
	if tel.PowerW != 65 {
		t.Fatalf("power_w = %d (ожидалось 65)", tel.PowerW)
	}
}

func TestEnModeCanon(t *testing.T) {
	cases := map[byte]uint16{0x00: 0x00, 0x10: 0x00, 0x01: 0x01, 0x02: 0x02, 0x04: 0x03, 0x08: 0x04, 0x20: 0x05}
	for in, want := range cases {
		if got := enModeCanon(in); got != want {
			t.Fatalf("enModeCanon(%#x) = %#x, ожидалось %#x", in, got, want)
		}
	}
}
