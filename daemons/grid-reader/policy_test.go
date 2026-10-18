package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

func TestBanLadder(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	now := time.Now()
	bad := func() { p.fail(0, badErr(errors.New("bad"))) }

	bad()
	if p.states[0].status != statusBad || !p.states[0].banUntil.IsZero() {
		t.Fatalf("после 1 неудачи: %+v", p.states[0])
	}
	bad()
	if p.states[0].banStatus != statusBan1 || p.states[0].banUntil.Before(now.Add(50*time.Second)) {
		t.Fatalf("после 2 неудач: %+v", p.states[0])
	}
	p.states[0].banUntil = time.Now().Add(-time.Second)
	bad()
	if p.states[0].nextTry.Before(time.Now().Add(50 * time.Second)) {
		t.Fatalf("после 3-й: нет повтора через минуту: %+v", p.states[0])
	}
	p.states[0].nextTry = time.Time{}
	bad()
	if p.states[0].banUntil.After(time.Now()) || p.states[0].nextTry.Before(time.Now().Add(50*time.Second)) {
		t.Fatalf("после 4-й: ожидался повтор через минуту: %+v", p.states[0])
	}
	p.states[0].nextTry = time.Time{}
	bad()
	if p.states[0].banStatus != statusBan5 || p.states[0].banUntil.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("после 5-й: ожидался бан 5 мин: %+v", p.states[0])
	}
}

func TestBanNoRouteAndTimeout(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 2)
	p.fail(0, &pollError{kind: errNoRoute, err: errors.New("no route")})
	if p.states[0].banStatus != statusBan10 || p.states[0].banUntil.Before(time.Now().Add(9*time.Minute)) {
		t.Fatalf("no route: %+v", p.states[0])
	}
	p.fail(1, &pollError{kind: errTimeout, err: errors.New("timeout")})
	if p.states[1].banStatus != statusBan5 || p.states[1].banUntil.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("timeout: %+v", p.states[1])
	}
}

func TestStatusPriorityAndStale(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	st := &p.states[0]
	st.lastSeen = time.Now().Add(-time.Hour).Unix()
	st.status = statusOK
	if got := p.currentStatus(0, time.Now(), 10*time.Second); got != statusStale {
		t.Fatalf("stale: %d", got)
	}
	st.banUntil = time.Now().Add(time.Minute)
	st.banStatus = statusBan5
	if got := p.currentStatus(0, time.Now(), 10*time.Second); got != statusBan5 {
		t.Fatalf("бан должен быть выше stale: %d", got)
	}
}

func TestLoadSettings16AndInvalidSlots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid.json")
	var b []byte
	b = append(b, '{')
	b = append(b, []byte(`"enabled":true,"poll_period":10,"inverters":[`)...)
	for i := 0; i < 17; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		name := "d"
		ip := "192.0.2." + itoa(i+1)
		if i == 1 {
			ip = "" // невалидная запись не должна сдвигать слоты
		}
		b = append(b, []byte(`{"name":"`+name+`","ip":"`+ip+`","port":8899,"type":"deye","logger_sn":1}`)...)
	}
	b = append(b, []byte(`]}`)...)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warns := loadSettingsFrom(path)
	if len(cfg.Inverters) != 16 {
		t.Fatalf("len=%d, ожидалось 16 (лишний отброшен)", len(cfg.Inverters))
	}
	if cfg.Inverters[0].Valid != true || cfg.Inverters[1].Valid != false || cfg.Inverters[2].Valid != true {
		t.Fatalf("valid: %+v", cfg.Inverters[:3])
	}
	if len(warns) == 0 {
		t.Fatal("ожидались предупреждения")
	}
}
