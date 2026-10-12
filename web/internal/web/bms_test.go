package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// testFields собирает legacyFields из name->значений (только присутствующие).
func testFields(t *testing.T, vals map[string]uint32) *legacyFields {
	t.Helper()
	f := &legacyFields{
		val:  make([]uint32, len(mapFieldNames)),
		pres: make([]bool, len(mapFieldNames)),
	}
	for nm, v := range vals {
		i, ok := legacyFieldIdx[nm]
		if !ok {
			t.Fatalf("нет поля %q", nm)
		}
		f.val[i] = v
		f.pres[i] = true
	}
	return f
}

// bmsDevice — распакованное устройство из read_bms.php.
type bmsDevice struct {
	DeviceName   string    `json:"deviceName"`
	Port         string    `json:"port"`
	CellCount    int       `json:"cell_count"`
	CellsV       []float64 `json:"cells_v"`
	CurrentA     float64   `json:"current_a"`
	Soc          float64   `json:"soc"`
	CapacityAh   float64   `json:"capacity_ah"`
	RemainingAh  float64   `json:"remaining_ah"`
	Temperatures []float64 `json:"temperatures_c"`
	PowerW       float64   `json:"power_w"`
	MaxCellV     float64   `json:"max_cell_v"`
	MinCellV     float64   `json:"min_cell_v"`
	AvgCellV     float64   `json:"avg_cell_v"`
}

func TestRenderBMSNil(t *testing.T) {
	if got := string(renderBMS(nil, 0, "MAP", "")); got != `{"updated":0,"devices":[]}` {
		t.Fatalf("nil: %q", got)
	}
}

func TestRenderBMSBanks(t *testing.T) {
	const ts = 1700000000
	f := testFields(t, map[string]uint32{
		"LCD_BMSFunk": 3,
		"BMS_Num":     2,
		// банка 0 — 3.20 В, банка 1 — 3.40 В (L + (H&0x7f)<<8)/100
		"UakbCell_L0": 64, "UakbCell_H0": 1,
		"UakbCell_L1": 84, "UakbCell_H1": 1,
		// temp: 75−50=25 °C; 255 — нет датчика → 127
		"TakbCell0": 75, "TakbCell1": 255,
		"SoC":      80,
		"UACC":     2,
		"LCD_CAcc": 80, // (80*25)>>2 = 500 Ач
		// 16/16 = 1 А; режим не 4..9 → разряд (−1 А)
		"IAcc_med_A_u16_L": 16, "IAcc_med_A_u16_H": 0,
	})

	body := renderBMS(f, ts, "Титанатор", "/dev/ttyAMA0")
	var top map[string]interface{}
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("JSON: %v\n%s", err, body)
	}
	if top["updated"] != float64(ts) {
		t.Fatalf("updated=%v, ожидалось %d", top["updated"], ts)
	}
	devs, ok := top["devices"].([]interface{})
	if !ok || len(devs) != 1 {
		t.Fatalf("devices: %#v", top["devices"])
	}
	raw, _ := json.Marshal(devs[0])
	var d bmsDevice
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("device JSON: %v", err)
	}
	if !strings.HasPrefix(d.DeviceName, "AntBms 500 A/h (Титанатор)") {
		t.Errorf("deviceName=%q", d.DeviceName)
	}
	if d.Port != "/dev/ttyAMA0" {
		t.Errorf("port=%q", d.Port)
	}
	if d.CellCount != 2 {
		t.Errorf("cell_count=%d", d.CellCount)
	}
	if len(d.CellsV) != 2 || d.CellsV[0] != 3.2 || d.CellsV[1] != 3.4 {
		t.Errorf("cells_v=%v", d.CellsV)
	}
	if d.CurrentA != -1 {
		t.Errorf("current_a=%v", d.CurrentA)
	}
	if d.Soc != 80 {
		t.Errorf("soc=%v", d.Soc)
	}
	if d.CapacityAh != 500 {
		t.Errorf("capacity_ah=%v", d.CapacityAh)
	}
	if d.RemainingAh != 400 {
		t.Errorf("remaining_ah=%v", d.RemainingAh)
	}
	if len(d.Temperatures) != 2 || d.Temperatures[0] != 25 || d.Temperatures[1] != 127 {
		t.Errorf("temperatures_c=%v", d.Temperatures)
	}
	if d.MaxCellV != 3.4 || d.MinCellV != 3.2 || d.AvgCellV != 3.3 {
		t.Errorf("max/min/avg=%v/%v/%v", d.MaxCellV, d.MinCellV, d.AvgCellV)
	}
}

func TestRenderBMSNoBMS(t *testing.T) {
	f := testFields(t, map[string]uint32{
		"LCD_BMSFunk": 0, // без BMS-модуля
		"BMS_Num":     0,
		"SoC":         55,
		"UACC":        2,
		"LCD_CAcc":    40,
	})
	body := renderBMS(f, 0, "MAP", "")
	var top map[string]interface{}
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	devs := top["devices"].([]interface{})
	raw, _ := json.Marshal(devs[0])
	var d bmsDevice
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("device JSON: %v", err)
	}
	if d.CellCount != 0 || len(d.CellsV) != 0 || len(d.Temperatures) != 0 {
		t.Errorf("без BMS: cell_count=%d cells_v=%v temps=%v", d.CellCount, d.CellsV, d.Temperatures)
	}
	if d.CapacityAh != 250 { // (40*25)>>2
		t.Errorf("capacity_ah=%v", d.CapacityAh)
	}
}
