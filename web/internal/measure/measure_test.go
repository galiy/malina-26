package measure

import (
	"math"
	"testing"
)

func TestMeasure(t *testing.T) {
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"Volt220(0)", Volt220(0), 0},
		{"Volt220(120)", Volt220(120), 220},
		{"Inet(20,16)", Inet(20, 16), 4},   // INET≥16 → /4
		{"Inet(1,16)", Inet(1, 16), 1},     // INET<16 → /16
		{"Iacc(160,5)", Iacc(160, 5), 10},  // заряд → +
		{"Iacc(160,0)", Iacc(160, 0), -10}, // не заряд → −
		{"Power100(36,0)", Power100(36, 0), 3600},
		{"BattVolt(0,127)", BattVolt(0, 127), 12.7},
		{"Temp(75)", Temp(75), 25},
		{"MpptCurrent(0,0)", MpptCurrent(0, 0), 0},
	}
	for _, c := range cases {
		if math.Abs(c.got-c.want) > 1e-6 {
			t.Fatalf("%s: got %v, ожидалось %v", c.name, c.got, c.want)
		}
	}
	if f, ok := Freq6250(50); !ok || math.Abs(f-125) > 1e-6 {
		t.Fatalf("Freq6250(50): %v ok=%v", f, ok)
	}
	if _, ok := Freq6250(0); ok {
		t.Fatal("Freq6250(0): должно быть нет данных")
	}
	if !IsCharge(4) || !IsCharge(9) || !IsCharge(10) || IsCharge(3) || IsCharge(0) {
		t.Fatal("IsCharge: неверно (ожидалось modeRAM>3)")
	}
}
