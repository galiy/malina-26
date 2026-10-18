package accumulator

import (
	"testing"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// TestAverageBmsAlarmOr — bms: alarm (id 40) усредняется по ИЛИ, mode (39) — last.
func TestAverageBmsAlarmOr(t *testing.T) {
	c, ok := reg.Get("bms", 1)
	if !ok {
		t.Fatal("контракт bms v1 не зарегистрирован")
	}
	samples := []Sample{
		{Time: 1, Vals: map[uint16]tscontract.Value{
			40: {ID: 40, Type: tscontract.FTU8, Int: 0x01},
			39: {ID: 39, Type: tscontract.FTU16, Int: 2},
			30: {ID: 30, Type: tscontract.FTU16, Int: 500},
		}},
		{Time: 2, Vals: map[uint16]tscontract.Value{
			40: {ID: 40, Type: tscontract.FTU8, Int: 0x02},
			39: {ID: 39, Type: tscontract.FTU16, Int: 1},
			30: {ID: 30, Type: tscontract.FTU16, Int: 700},
		}},
	}
	res, ok := Average("bms", 1, samples)
	if !ok {
		t.Fatal("Average вернул ok=false")
	}
	vals, err := c.Decode(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]int64{}
	for _, v := range vals {
		got[v.ID] = v.Int
	}
	if got[40] != 0x03 {
		t.Fatalf("alarm(id40) = %d, ожидалось 3 (ИЛИ)", got[40])
	}
	if got[39] != 1 {
		t.Fatalf("mode(id39) = %d, ожидалось 1 (last)", got[39])
	}
	if got[30] != 600 {
		t.Fatalf("soc(id30) = %d, ожидалось 600 (avg)", got[30])
	}
}

// TestAverageGridRules — grid: inverter_status (18) — last, alarm_w0..5 (19..24) —
// OR, телеметрия (7) — среднее.
func TestAverageGridRules(t *testing.T) {
	c, ok := reg.Get("grid", 1)
	if !ok {
		t.Fatal("контракт grid v1 не зарегистрирован")
	}
	samples := []Sample{
		{Time: 1, Vals: map[uint16]tscontract.Value{
			7:  {ID: 7, Type: tscontract.FTU16, Int: 100},
			18: {ID: 18, Type: tscontract.FTU8, Int: 2},
			19: {ID: 19, Type: tscontract.FTU16, Int: 0x01},
		}},
		{Time: 2, Vals: map[uint16]tscontract.Value{
			7:  {ID: 7, Type: tscontract.FTU16, Int: 300},
			18: {ID: 18, Type: tscontract.FTU8, Int: 3},
			19: {ID: 19, Type: tscontract.FTU16, Int: 0x02},
		}},
	}
	res, ok := Average("grid", 1, samples)
	if !ok {
		t.Fatal("Average вернул ok=false")
	}
	vals, err := c.Decode(res.Payload)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint16]int64{}
	for _, v := range vals {
		got[v.ID] = v.Int
	}
	if got[7] != 200 {
		t.Fatalf("active power = %d, ожидалось 200 (avg)", got[7])
	}
	if got[18] != 3 {
		t.Fatalf("inverter_status = %d, ожидалось 3 (last)", got[18])
	}
	if got[19] != 0x03 {
		t.Fatalf("alarm_w0 = %d, ожидалось 3 (OR)", got[19])
	}
}
