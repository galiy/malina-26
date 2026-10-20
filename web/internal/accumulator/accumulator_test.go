package accumulator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// TestLoadDefaults — без файла аккумулятор включён с периодом 5 мин;
// явные значения из файла сохраняются, невалидный период сбрасывается на 5.
func TestLoadDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accumulator.json")
	cfg := Load(path)
	if !cfg.Enabled || cfg.PeriodMin != 5 {
		t.Fatalf("дефолт: %+v, ожидалось enabled=true period=5", cfg)
	}
	if err := os.WriteFile(path, []byte(`{"enabled":false,"period_min":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg = Load(path)
	if cfg.Enabled || cfg.PeriodMin != 1 {
		t.Fatalf("из файла: %+v, ожидалось enabled=false period=1", cfg)
	}
	if err := os.WriteFile(path, []byte(`{"enabled":true,"period_min":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg = Load(path)
	if !cfg.Enabled || cfg.PeriodMin != 5 {
		t.Fatalf("невалидный период: %+v, ожидалось enabled=true period=5", cfg)
	}
}

func mkVals(kv map[uint16]tscontract.Value) map[uint16]tscontract.Value { return kv }

func TestAverageMapRules(t *testing.T) {
	s1 := Sample{Time: 1000, Vals: mkVals(map[uint16]tscontract.Value{
		1:  {ID: 1, Type: tscontract.FTU8, Int: 10}, // UNET (среднее)
		11: {ID: 11, Type: tscontract.FTU8, Int: 1}, // StateUAcc (последнее)
		12: {ID: 12, Type: tscontract.FTU8, Int: 0}, // F_AccOver (ИЛИ)
		26: {ID: 26, Type: tscontract.FTU8, Int: 0}, // flagUnet2
	})}
	s2 := Sample{Time: 2000, Vals: mkVals(map[uint16]tscontract.Value{
		1:  {ID: 1, Type: tscontract.FTU8, Int: 20},
		11: {ID: 11, Type: tscontract.FTU8, Int: 2},
		12: {ID: 12, Type: tscontract.FTU8, Int: 4},
		26: {ID: 26, Type: tscontract.FTU8, Int: 0x04},
	})}
	res, ok := Average("map", 1, []Sample{s1, s2})
	if !ok {
		t.Fatal("Average: ok=false")
	}
	if res.Time != 2000 {
		t.Fatalf("время = %d, ожидалось 2000 (последний снимок)", res.Time)
	}
	c, _ := tscontract.NewDefaultRegistry().Get("map", 1)
	vals, err := c.Decode(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]int64{}
	for _, v := range vals {
		got[v.ID] = v.Int
	}
	if got[1] != 15 {
		t.Fatalf("UNET = %d, ожидалось 15 (среднее)", got[1])
	}
	if got[11] != 2 {
		t.Fatalf("StateUAcc = %d, ожидалось 2 (последнее)", got[11])
	}
	if got[12] != 4 {
		t.Fatalf("F_AccOver = %d, ожидалось 4 (ИЛИ)", got[12])
	}
	if got[26] != 0x04 {
		t.Fatalf("flagUnet2 = %d, ожидалось 4 (2-й вход)", got[26])
	}
}

// TestAverageMapV2Relay — map v2: реле (id 27) усредняется по правилу ИЛИ.
func TestAverageMapV2Relay(t *testing.T) {
	s1 := Sample{Time: 1000, Vals: mkVals(map[uint16]tscontract.Value{
		27: {ID: 27, Type: tscontract.FTU8, Int: 0x01}, // Relay: реле1
	})}
	s2 := Sample{Time: 2000, Vals: mkVals(map[uint16]tscontract.Value{
		27: {ID: 27, Type: tscontract.FTU8, Int: 0x02}, // Relay: реле2
	})}
	res, ok := Average("map", 2, []Sample{s1, s2})
	if !ok {
		t.Fatal("Average map v2: ok=false")
	}
	c, _ := tscontract.NewDefaultRegistry().Get("map", 2)
	vals, err := c.Decode(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vals {
		if v.ID == 27 && v.Int != 0x03 {
			t.Fatalf("Relay = %d, ожидалось 3 (ИЛИ)", v.Int)
		}
	}
}

func TestConfigPeriod(t *testing.T) {
	for _, p := range []int{1, 2, 3, 4, 5, 6, 10} {
		if !ValidatePeriod(p) {
			t.Fatalf("период %d должен быть разрешён", p)
		}
	}
	for _, p := range []int{0, 7, 8, 9, 11, 60} {
		if ValidatePeriod(p) {
			t.Fatalf("период %d должен быть запрещён", p)
		}
	}
}

// TestAverageMapZeroMajority — для напряжений и частот МАП нулевые снимки не
// участвуют в среднем, а решение принимается по большинству.
func TestAverageMapZeroMajority(t *testing.T) {
	mk := func(u, f int64) Sample {
		return Sample{Vals: mkVals(map[uint16]tscontract.Value{
			1: {ID: 1, Type: tscontract.FTU8, Int: u},
			7: {ID: 7, Type: tscontract.FTU8, Int: f},
		})}
	}
	check := func(name string, samples []Sample, wantU, wantF int64) {
		t.Helper()
		res, ok := Average("map", 3, samples)
		if !ok {
			t.Fatalf("%s: Average ok=false", name)
		}
		c, _ := tscontract.NewDefaultRegistry().Get("map", 3)
		vals, err := c.Decode(res.Payload)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := map[uint16]int64{}
		for _, v := range vals {
			got[v.ID] = v.Int
		}
		if got[1] != wantU || got[7] != wantF {
			t.Fatalf("%s: UNET=%d TFNET=%d, ожидалось %d/%d", name, got[1], got[7], wantU, wantF)
		}
	}
	check("валидных больше", []Sample{mk(120, 125), mk(122, 125), mk(0, 0)}, 121, 125)
	check("нулей больше", []Sample{mk(120, 125), mk(0, 0), mk(0, 0)}, 0, 0)
	check("равенство", []Sample{mk(120, 125), mk(0, 0)}, 0, 0)
	check("все нули", []Sample{mk(0, 0), mk(0, 0)}, 0, 0)
	check("все валидные", []Sample{mk(120, 125), mk(122, 125)}, 121, 125)
	check("частота в Гц", []Sample{mk(120, 125), mk(120, 100), mk(0, 0)}, 120, 111)
}

// TestAverageMapZeroMajorityOut — те же правила для UOUTmed (6) и ThFMAP (28).
func TestAverageMapZeroMajorityOut(t *testing.T) {
	mk := func(uo, tf int64) Sample {
		return Sample{Vals: mkVals(map[uint16]tscontract.Value{
			6:  {ID: 6, Type: tscontract.FTU8, Int: uo},
			28: {ID: 28, Type: tscontract.FTU8, Int: tf},
		})}
	}
	res, ok := Average("map", 3, []Sample{mk(120, 125), mk(122, 100), mk(0, 0)})
	if !ok {
		t.Fatal("Average ok=false")
	}
	c, _ := tscontract.NewDefaultRegistry().Get("map", 3)
	vals, err := c.Decode(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]int64{}
	for _, v := range vals {
		got[v.ID] = v.Int
	}
	if got[6] != 121 || got[28] != 111 {
		t.Fatalf("UOUTmed=%d ThFMAP=%d, ожидалось 121/111", got[6], got[28])
	}
}
