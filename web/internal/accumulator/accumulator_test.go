package accumulator

import (
	"testing"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

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
