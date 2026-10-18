package tscontract

import "testing"

func TestSchemasRegistered(t *testing.T) {
	reg := NewDefaultRegistry()

	if c, ok := reg.Get("map", 1); !ok || len(c.Fields()) != 26 {
		t.Fatalf("map v1: ok=%v", ok)
	}
	if c, ok := reg.Get("kes", 1); !ok || len(c.Fields()) != 15 {
		t.Fatalf("kes v1: ok=%v", ok)
	}
	if _, ok := reg.Get("mbms", 32); !ok {
		t.Fatal("mbms v32 не зарегистрирован")
	}
	if _, ok := reg.Get("mbms", 33); ok {
		t.Fatal("mbms v33 не должен существовать")
	}
	if n := len(mbmsFields(8)); n != 24 {
		t.Fatalf("mbms v8 полей: %d, ожидалось 24", n)
	}
	// map v2 = v1 + реле (id 27)
	if c, ok := reg.Get("map", 2); !ok || len(c.Fields()) != 27 {
		t.Fatalf("map v2: ok=%v", ok)
	}
	// map v3 = v2 + ThFMAP (28) + I_Akb_MPPT0..3 (29..32) + PNET_Sign_P (33)
	if c, ok := reg.Get("map", 3); !ok || len(c.Fields()) != 33 {
		t.Fatalf("map v3: ok=%v", ok)
	}
}

// TestMapV2Relay — map v2 кодирует/декодирует поле реле (id 27).
func TestMapV2Relay(t *testing.T) {
	reg := NewDefaultRegistry()
	c, ok := reg.Get("map", 2)
	if !ok {
		t.Fatal("map v2 не зарегистрирован")
	}
	p, err := c.Encode([]Value{
		{ID: 1, Type: FTU8, Int: 120},   // UNET
		{ID: 27, Type: FTU8, Int: 0x03}, // Relay: реле1+реле2
	})
	if err != nil {
		t.Fatal(err)
	}
	vals, err := c.Decode(p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range vals {
		if v.ID == 27 {
			found = true
			if v.Int != 0x03 {
				t.Fatalf("Relay=%d, ожидалось 3", v.Int)
			}
		}
	}
	if !found {
		t.Fatal("поле Relay (27) не найдено в payload v2")
	}
}

func TestU8U16RoundTrip(t *testing.T) {
	c := newSchemaContract(1, mapV1Fields)
	p, err := c.Encode([]Value{
		{ID: 1, Type: FTU8, Int: 250},
		{ID: 4, Type: FTU16, Int: 12345},
	})
	if err != nil {
		t.Fatal(err)
	}
	vals, err := c.Decode(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 2 || vals[0].ID != 1 || vals[0].Int != 250 || vals[1].ID != 4 || vals[1].Int != 12345 {
		t.Fatalf("decode: %+v", vals)
	}
}

func TestProject(t *testing.T) {
	reg := NewDefaultRegistry()
	c, _ := reg.Get("map", 1)
	p, _ := c.Encode([]Value{{ID: 1, Type: FTU8, Int: 7}, {ID: 4, Type: FTU16, Int: 1000}})

	proj, ok := reg.Project("map", 1, p, []uint16{4})
	if !ok {
		t.Fatal("project: ожидался успех")
	}
	pv, _ := c.Decode(proj)
	if len(pv) != 1 || pv[0].ID != 4 || pv[0].Int != 1000 {
		t.Fatalf("project: %+v", pv)
	}
	if _, ok := reg.Project("unknown", 1, p, []uint16{1}); ok {
		t.Fatal("неизвестный тип должен отвергаться")
	}
}

// TestGridV1 — контракт grid v1: 24 поля, все присутствуют, payload 50 Б,
// знаковые поля i16 кодируются/декодируются.
func TestGridV1(t *testing.T) {
	reg := NewDefaultRegistry()
	c, ok := reg.Get("grid", 1)
	if !ok || len(c.Fields()) != 24 {
		t.Fatalf("grid v1: ok=%v fields=%d", ok, len(c.Fields()))
	}
	vals := make([]Value, 0, 24)
	for id := uint16(1); id <= 24; id++ {
		v := Value{ID: id, Type: FTU16, Int: int64(id)}
		if id == 8 || id == 16 || id == 17 {
			v.Type = FTI16
			v.Int = -int64(id)
		}
		if id == 18 {
			v.Type = FTU8
		}
		vals = append(vals, v)
	}
	p, err := c.Encode(vals)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 50 {
		t.Fatalf("payload: %d Б, ожидалось 50", len(p))
	}
	got, err := c.Decode(p)
	if err != nil || len(got) != 24 {
		t.Fatalf("decode: n=%d err=%v", len(got), err)
	}
	if got[7].Int != -8 || got[15].Int != -16 || got[17].Int != 18 {
		t.Fatalf("signed/status: %+v %+v %+v", got[7], got[15], got[17])
	}
}
