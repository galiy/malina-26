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
func (a *fakeAct) SNMPTrap(host, field string, value float64, mpptNo int) error {
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
}
