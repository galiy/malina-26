package events

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	good := Config{Events: []Event{{
		Enabled: true, Type: "param", Source: Device{Kind: "map", Serial: "37005"},
		Field: "unet", Op: "le", Value: "180", Action: "email",
	}}}
	if err := Validate(good); err != nil {
		t.Fatalf("good: %v", err)
	}
	bad := []Config{
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: ""}, Field: "unet", Op: "le", Value: "1", Action: "email"}}},    // нет серийника
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: "x"}, Field: "unet", Op: "le", Value: "1", Action: "email"}}},   // серийник не числовой
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "xx", Value: "1", Action: "email"}}},   // bad op
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: "1"}, Field: "bogus", Op: "le", Value: "1", Action: "email"}}},  // bad param
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "le", Value: "abc", Action: "email"}}}, // bad number
		{Events: []Event{{Type: "time", Source: Device{Kind: "map", Serial: "1"}, Value: "99:99", Action: "email"}}},                         // bad time
		{Events: []Event{{Type: "param", Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "le", Value: "1", Action: "xx"}}},      // bad action
	}
	for i := range bad {
		if err := Validate(bad[i]); err == nil {
			t.Fatalf("кейс %d: ожидали ошибку", i)
		}
	}
}

type fakeProv struct{ s Snapshot }

func (f fakeProv) Snapshot(serial, kind string) (Snapshot, bool) { return f.s, f.s.OK }
func (f fakeProv) Available(serial, kind string) bool            { return true }

type fakeAct struct {
	emails   int
	macs     int
	lastSubj string
}

func (a *fakeAct) Email(subject, body, recipient string) error {
	a.emails++
	a.lastSubj = subject
	return nil
}
func (a *fakeAct) Relay(serial, kind string, relayNo int, on bool) error { return nil }
func (a *fakeAct) MacCmd(serial, kind, cmd string) error                 { a.macs++; return nil }
func (a *fakeAct) SNMPTrap(host, field string, value float64, mpptNo int, ok bool) error {
	return nil
}

func saveCfg(t *testing.T, path string, ev Event) {
	t.Helper()
	if err := Save(path, Config{Events: []Event{ev}}); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestEngineParamFrontOkCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	saveCfg(t, path, Event{ID: "aaaaaaaa", Enabled: true, Type: "param",
		Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "le", Value: "100",
		Cooldown: 30, Action: "email"})
	prov := fakeProv{s: Snapshot{Serial: "1", Kind: "map", OK: true, Fields: map[string]uint32{"UNET": 0}}}
	act := &fakeAct{}
	e := New(path, prov, act, nil)
	e.SetStatePath(filepath.Join(t.TempDir(), "state.json"))
	t0 := time.Now()
	e.tick(t0) // нет сети → unet=0 ≤100 → сработало
	if act.emails != 1 {
		t.Fatalf("emails=%d", act.emails)
	}
	e.tick(t0.Add(10 * time.Second)) // кулдаун
	if act.emails != 1 {
		t.Fatalf("в кулдауне повторно: %d", act.emails)
	}
	prov.s.Fields["UNET"] = 50 // 150 > 100 → норма → Ok
	e.tick(t0.Add(40 * time.Second))
	if act.emails != 2 {
		t.Fatalf("ожидали Ok-письмо: %d", act.emails)
	}
}

func TestEngineModeNoOk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	saveCfg(t, path, Event{ID: "cccccccc", Enabled: true, Type: "mode",
		Source: Device{Kind: "map", Serial: "1"}, Value: "0", Cooldown: 30, Action: "email"})
	prov := fakeProv{s: Snapshot{Serial: "1", Kind: "map", OK: true, Fields: map[string]uint32{"MODE_RAM": 0}}}
	act := &fakeAct{}
	e := New(path, prov, act, nil)
	e.SetStatePath(filepath.Join(t.TempDir(), "state.json"))
	t0 := time.Now()
	e.tick(t0)
	if act.emails != 1 {
		t.Fatalf("MODE=0: %d", act.emails)
	}
	prov.s.Fields["MODE_RAM"] = 3
	e.tick(t0.Add(40 * time.Second))
	if act.emails != 2 {
		t.Fatalf("MODE: обратное письмо должно уйти: %d", act.emails)
	}
}

func TestEngineTimeInitialTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	saveCfg(t, path, Event{ID: "ffffffff", Enabled: true, Type: "time",
		Source: Device{Kind: "map", Serial: "1"}, Value: "12:00", Action: "email"})
	act := &fakeAct{}
	e := New(path, fakeProv{s: Snapshot{Serial: "1", Kind: "map", OK: true}}, act, nil)
	e.SetStatePath(filepath.Join(t.TempDir(), "state.json"))
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)
	e.tick(at) // на старте trigger=1 → не срабатывает
	if act.emails != 0 {
		t.Fatalf("time на старте не должен срабатывать: %d", act.emails)
	}
}

func TestEngineStopGenInitialTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	saveCfg(t, path, Event{ID: "dddddddd", Enabled: true, Type: "param",
		Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "le", Value: "100",
		Cooldown: 30, Action: "mac_stop_gen"})
	prov := fakeProv{s: Snapshot{Serial: "1", Kind: "map", OK: true, Fields: map[string]uint32{"UNET": 0}}}
	act := &fakeAct{}
	e := New(path, prov, act, nil)
	e.SetStatePath(filepath.Join(t.TempDir(), "state.json"))
	t0 := time.Now()
	e.tick(t0)
	if act.macs != 0 {
		t.Fatalf("на старте не должно: %d", act.macs)
	}
	prov.s.Fields["UNET"] = 50 // 150 > 100 → норма → сброс
	e.tick(t0.Add(40 * time.Second))
	if act.macs != 0 {
		t.Fatalf("сброс не командует: %d", act.macs)
	}
	prov.s.Fields["UNET"] = 0
	e.tick(t0.Add(80 * time.Second))
	if act.macs != 1 {
		t.Fatalf("после сброса должно: %d", act.macs)
	}
}

func TestEngineResetOnChangeAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	rule := Event{ID: "eeeeeeee", Enabled: true, Type: "param",
		Source: Device{Kind: "map", Serial: "1"}, Field: "unet", Op: "le", Value: "100",
		Cooldown: 30, Action: "email"}
	saveCfg(t, path, rule)
	prov := fakeProv{s: Snapshot{Serial: "1", Kind: "map", OK: true, Fields: map[string]uint32{"UNET": 0}}}
	act := &fakeAct{}
	e := New(path, prov, act, nil)
	e.SetStatePath(filepath.Join(t.TempDir(), "state.json"))
	t0 := time.Now()
	e.tick(t0)
	if act.emails != 1 {
		t.Fatalf("emails=%d", act.emails)
	}
	rule.Value = "10" // смена содержания → сброс состояния
	saveCfg(t, path, rule)
	e.tick(t0.Add(time.Second))
	if act.emails != 2 {
		t.Fatalf("после смены содержания должно сработать снова: %d", act.emails)
	}
	// удаляем правило → состояние чистится
	if err := Save(path, Config{Events: []Event{}}); err != nil {
		t.Fatal(err)
	}
	e.tick(t0.Add(2 * time.Second))
	if len(e.state) != 0 {
		t.Fatalf("состояния не почищены: %d", len(e.state))
	}
}

func TestResolveParam(t *testing.T) {
	f := map[string]uint32{"UNET": 120, "UAcc_med_VH": 1, "UAcc_med_VL": 200}
	if v, _ := ResolveParam("unet", f); v != 220 {
		t.Fatalf("unet=%v", v)
	}
	if v, _ := ResolveParam("uacc", f); v != (1*256+200)/10.0 {
		t.Fatalf("uacc=%v", v)
	}
	// Единый слой measure: inet с учётом INET, pnet/PLoad по L/H, iacc со знаком.
	g := map[string]uint32{
		"INET": 20, "INET_16_4": 16, // INET≥16 → /4
		"PNET_L": 36, "PNET_H": 0, // 3600 Вт
		"PLoad_L": 10, "PLoad_H": 1, // (10+256)*100
		"IAcc_med_A_u16_L": 160, "IAcc_med_A_u16_H": 0, "MODE_RAM": 5, // заряд → +10
	}
	if v, _ := ResolveParam("inet", g); v != 4 {
		t.Fatalf("inet=%v", v)
	}
	if v, _ := ResolveParam("pnet", g); v != 3600 {
		t.Fatalf("pnet=%v", v)
	}
	if v, _ := ResolveParam("pacc", g); v != (10+256)*100 {
		t.Fatalf("pacc=%v", v)
	}
	if v, _ := ResolveParam("iacc", g); v != 10 {
		t.Fatalf("iacc(charge)=%v", v)
	}
	g["MODE_RAM"] = 0
	if v, _ := ResolveParam("iacc", g); v != -10 {
		t.Fatalf("iacc(discharge)=%v", v)
	}
}

func TestResolveParamKes(t *testing.T) {
	g := map[string]uint32{
		"VcPV": 1234, "IcPV": 567, "VBat": 4800, "PPV": 900,
		"TempBat": 10, "TempInt": 25, "Windspeed": 120, "Mode": 105,
	}
	if v, _ := ResolveParam("k_vcpv", g); v != 12.34 {
		t.Fatalf("k_vcpv=%v", v)
	}
	if v, _ := ResolveParam("k_icpv", g); v != 5.67 {
		t.Fatalf("k_icpv=%v", v)
	}
	if v, _ := ResolveParam("k_ppv", g); v != 900 {
		t.Fatalf("k_ppv=%v", v)
	}
	if v, _ := ResolveParam("k_tbat", g); v != -40 {
		t.Fatalf("k_tbat=%v", v)
	}
	if v, _ := ResolveParam("k_tint", g); v != -25 {
		t.Fatalf("k_tint=%v", v)
	}
	if v, _ := ResolveParam("k_mode", g); v != 105 {
		t.Fatalf("k_mode=%v", v)
	}
	// imppt — СУММА по каналам (не среднее): два канала по 16 А → 32 А.
	m := map[string]uint32{
		"I_Akb_MPPT_L0": 0, "I_Akb_MPPT_H0": 1,
		"I_Akb_MPPT_L1": 0, "I_Akb_MPPT_H1": 1,
	}
	if v, _ := ResolveParam("imppt", m); v != 32 {
		t.Fatalf("imppt(сумма)=%v, ожидалось 32", v)
	}
	// R4: без каналов MPPT — нет данных (а не 0 А).
	if _, ok := ResolveParam("imppt", map[string]uint32{}); ok {
		t.Fatal("imppt без каналов: ожидалось ok=false")
	}
	if _, ok := ResolveParam("k_unknown", g); ok {
		t.Fatal("k_unknown не должен резолвиться")
	}
	if !IsParam("kes", "k_vcpv") || IsParam("map", "k_vcpv") || !IsParam("map", "unet") || IsParam("kes", "unet") {
		t.Fatal("IsParam: неверная привязка параметров к типу")
	}
}
