package web

import (
	"math"
	"testing"
)

func TestSocFromOCV(t *testing.T) {
	tests := []struct {
		name string
		tbl  []ocvPoint
		u    float64
		want float64
	}{
		{"ниже минимума -> 0", liFePO4OCV, 2.0, 0},
		{"выше максимума -> 100", liFePO4OCV, 4.0, 100},
		{"точно в точке", liFePO4OCV, 3.20, 10.3},
		{"интерполяция между", liFePO4OCV, 3.265, 20}, // между 3.25(15) и 3.28(25)
		{"пустая таблица -> 0", nil, 3.3, 0},
	}
	for _, tc := range tests {
		if got := socFromOCV(tc.tbl, tc.u); math.Abs(got-tc.want) > 1e-6 {
			t.Fatalf("%s: socFromOCV(u=%v)=%v, ожидалось %v", tc.name, tc.u, got, tc.want)
		}
	}
}

// testMapProfile — поля снимка МАП, воспроизводящие реальное железо (.60):
// LiFePO4 48В (16S), заряд (MODE=4), Uacc=51.5В, ток 0.625А, ёмкость 500 Ач,
// без аппаратного SoC (255).
func testMapProfile() map[string]uint32 {
	return map[string]uint32{
		"LCD_AccType":      5, // Li-ion LYP 3.7В
		"UACC":             2, // 48 В
		"UAcc_med_VH":      2,
		"UAcc_med_VL":      3, // (2*256+3)/10 = 51.5 В
		"MODE":             4, // заряд
		"IAcc_med_A_u16_L": 10,
		"IAcc_med_A_u16_H": 0,  // (10)/16 = 0.625 А
		"LCD_CAcc":         80, // → (80*25)>>2 = 500 Ач
		"LCD_CAcc_dop":     0,
	}
}

func TestBatSocPriorityHardwareSoC(t *testing.T) {
	f := testMapProfile()
	f["SoC"] = 72 // аппаратный SoC в диапазоне 0..100 → приоритет
	soc, ok := batSocFromFields(f)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	if soc != 72 {
		t.Fatalf("soc=%v, ожидалось 72 (приоритет аппаратному SoC)", soc)
	}
}

func TestBatSocSoC255FallsToOCV(t *testing.T) {
	f := testMapProfile()
	f["SoC"] = 255 // нет данных → OCV
	soc, ok := batSocFromFields(f)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	// ucell (Optim 51.6В) ≈ 3.225 В → факт OCV LFP ≈ 11% (по статистике БД недели).
	if soc < 8 || soc > 15 {
		t.Fatalf("soc=%v, ожидалось ~11%% по OCV LFP (факт 3.225В)", soc)
	}
}

func TestBatSocLeadPer12VBlocks(t *testing.T) {
	f := testMapProfile()
	// 24В, кислотный: blocks = 2, Uacc = 25.6В → per-block 12.8В → 100%.
	f["LCD_AccType"] = 0 // кислотный
	f["UACC"] = 1        // 24 В
	f["UAcc_med_VH"] = 1
	f["UAcc_med_VL"] = 60 // (256+60)/10 = 31.6? нет: 1*256+60=316/10=31.6
	// 31.6/2 = 15.8 В — выше любой точки (>12.8) → 100%
	soc, ok := batSocFromFields(f)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	if soc != 100 {
		t.Fatalf("soc=%v, ожидалось 100 (напряжение выше максимума свинца)", soc)
	}
}

func TestBatSocGelTable(t *testing.T) {
	f := testMapProfile()
	// 12В гель, Uacc так, чтобы попасть в середину gel-кривой: 12.35В → 50%.
	f["LCD_AccType"] = 1 // гель/AGM
	f["UACC"] = 0        // 12 В
	f["UAcc_med_VH"] = 0
	f["UAcc_med_VL"] = 80 // (0*256+80)/10 = 8.0? нет: VL=80 → 80/10=8.0 В
	// 8.0 В — ниже минимума (11.4) → 0... это низкое напряжение.
	f["UAcc_med_VL"] = 0
	f["UAcc_med_VH"] = 0
	// зададим 12.35 В: VH=1, VL=... (256*x)/10. 12.35В → VH*256+VL = 123.5 → VH=0, VL=123.5
	// VL целое: возьмём VH=0, VL=124 → 12.4 В.
	f["UAcc_med_VH"] = 0
	f["UAcc_med_VL"] = 124 // 12.4 В
	soc, ok := batSocFromFields(f)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	// 12.4В на gel: между 12.35(50) и 12.50(60) → ~56%. Проверим диапазон.
	if soc < 50 || soc > 65 {
		t.Fatalf("soc=%v, ожидалось ~50..65 по gel-кривой при 12.4В", soc)
	}
}

func TestBatSocDirectionByMode(t *testing.T) {
	// Держим значимый ток (30 А), чтобы сдвиг OCV от поправки был не «плоским».
	chargeF := testMapProfile()
	chargeF["IAcc_med_A_u16_L"] = 480 // (480+0)/16 = 30 А
	disF := testMapProfile()
	disF["IAcc_med_A_u16_L"] = 480
	charge := batSocFromFieldsWrapped(t, chargeF, 4)
	discharge := batSocFromFieldsWrapped(t, disF, 10)
	// При разряде напряжение проседает под нагрузкой → OCV-оценка выше, SoC выше.
	if discharge < charge {
		t.Fatalf("разряд (%v) должен давать SoC >= заряда (%v)", discharge, charge)
	}
}

func batSocFromFieldsWrapped(t *testing.T, f map[string]uint32, mode uint32) float64 {
	f["MODE"] = mode
	soc, ok := batSocFromFields(f)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	return soc
}

func TestBatSocNoData(t *testing.T) {
	if _, ok := batSocFromFields(map[string]uint32{}); ok {
		t.Fatal("пустой снимок: ожидалось ok=false")
	}
}
