package web

import (
	"math"
	"testing"
)

func TestFieldConvNum(t *testing.T) {
	cases := []struct {
		typ      string
		id       uint16
		raw      float64
		row      map[uint16]float64
		want     float64
		unit     string
		hasValue bool
	}{
		{"map", 1, 0, nil, 0, "В", true},     // UNET: 0 — нет сети (0 В)
		{"map", 1, 120, nil, 220, "В", true}, // UNET: V+100
		{"map", 7, 50, nil, 125, "Гц", true}, // TFNET: 6250/V
		{"map", 7, 0, nil, 0, "Гц", false},   // TFNET: 0 — нет данных
		{"map", 8, 127, nil, 12.7, "В", true},
		{"map", 5, 100, nil, 10000, "Вт", true},
		{"map", 14, 75, nil, 25, "°C", true},
		{"map", 3, 16, map[uint16]float64{2: 16}, 4, "А", true},     // INET≥16 → /4
		{"map", 3, 16, map[uint16]float64{2: 1}, 1, "А", true},      // INET<16 → /16
		{"map", 3, 16, nil, 0, "А", false},                          // R1: без INET — нет данных
		{"map", 10, 160, map[uint16]float64{19: 5}, 10, "А", true},  // заряд → +
		{"map", 10, 160, map[uint16]float64{19: 0}, -10, "А", true}, // не заряд → −
		{"map", 10, 160, nil, 0, "А", false},                        // R1: без MODE — нет данных
		{"map", 9, 7, nil, 7, "", true},                             // IAcc_med_A_2 — без масштаба (raw)
		{"kes", 1, 1234, nil, 12.34, "В", true},
		{"kes", 8, 75, nil, 25, "°C", true},
		{"kes", 4, 65535, nil, 0, "", false}, // Windspeed: 0xFFFF — н/д
		{"kes", 4, 120, nil, 120, "", true},
		{"mbms", 1, 32768 + 340, nil, 3.4, "В", true},                // UakbCell & 0x7FFF
		{"mbms", 2, 255, nil, 0, "°C", false},                        // R2: TakbCell нет датчика → «—»
		{"mbms", 3, 100, map[uint16]float64{1: 320}, 3.2, "А", true}, // Q_Cell: q/100 · UakbCell
		{"mbms", 3, 100, nil, 0, "А", false},                         // R1: без UakbCell — нет данных
	}
	for _, c := range cases {
		fc, known := fieldConvFor(c.typ, 1, c.id)
		if !known {
			t.Fatalf("%s/%d: поле неизвестно", c.typ, c.id)
		}
		if fc.Kind != kindNum {
			t.Fatalf("%s/%d: kind=%v, ожидался kindNum", c.typ, c.id, fc.Kind)
		}
		if fc.Unit != c.unit {
			t.Fatalf("%s/%d: unit=%q, ожидалось %q", c.typ, c.id, fc.Unit, c.unit)
		}
		got, ok := fc.Num(c.raw, c.row)
		if ok != c.hasValue {
			t.Fatalf("%s/%d: ok=%v, ожидалось %v", c.typ, c.id, ok, c.hasValue)
		}
		if ok && math.Abs(got-c.want) > 1e-6 {
			t.Fatalf("%s/%d: got %v, ожидалось %v", c.typ, c.id, got, c.want)
		}
	}
}

func TestFieldConvKinds(t *testing.T) {
	if fc, _ := fieldConvFor("kes", 1, 13); fc.Kind != kindSymbol {
		t.Fatalf("kes/13 (Mode): kind=%v, ожидался kindSymbol", fc.Kind)
	}
	if fc, _ := fieldConvFor("kes", 1, 14); fc.Kind != kindRelay3 {
		t.Fatalf("kes/14 (RelayC): kind=%v, ожидался kindRelay3", fc.Kind)
	}
	if fc, _ := fieldConvFor("map", 2, 27); fc.Kind != kindRelay3 {
		t.Fatalf("map/27 (Relay): kind=%v, ожидался kindRelay3", fc.Kind)
	}
	if _, known := fieldConvFor("unknown", 1, 1); known {
		t.Fatal("неизвестный тип не должен давать конверсию")
	}
	// R6: недокументированные поля помечены как «сырые».
	if fc, _ := fieldConvFor("map", 1, 9); !fc.Raw {
		t.Fatalf("map/9 (IAcc_med_A_2): Raw=%v, ожидалось true", fc.Raw)
	}
	if fc, _ := fieldConvFor("map", 1, 1); fc.Raw {
		t.Fatal("map/1 (UNET): Raw не должен быть true")
	}
}
