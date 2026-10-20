package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/logx"
	"github.com/galiy/malina-26/daemons/lib/solarman"
)

func TestBanLadder(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	now := time.Now()
	bad := func() { p.fail(0, &pollError{kind: errOther, err: errors.New("refused")}) }

	bad()
	if p.states[0].status != statusNoCon || !p.states[0].banUntil.IsZero() {
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

func TestBadSoftBackoffBothTypes(t *testing.T) {
	for _, typ := range []string{"deye", "sofar"} {
		p := newPoller(nil, nil, logx.New("test: "))
		p.states = make([]devState, 1)
		for i := 0; i < 3; i++ {
			p.fail(0, badErr(solarman.ErrNoData))
		}
		st := p.states[0]
		if st.status != statusBad || !st.banUntil.IsZero() || st.banStatus != 0 {
			t.Fatalf("%s: плохой ответ не должен уходить в бан: %+v", typ, st)
		}
		if st.nextTry.Before(time.Now().Add(50 * time.Second)) {
			t.Fatalf("%s: ожидалась пауза 1 мин: %+v", typ, st)
		}
	}
}

func TestBanNoRoute(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	p.fail(0, &pollError{kind: errNoRoute, err: errors.New("no route")})
	if p.states[0].banStatus != statusBan10 || p.states[0].banUntil.Before(time.Now().Add(9*time.Minute)) {
		t.Fatalf("no route: %+v", p.states[0])
	}
}

func TestClassifyTimeoutByARP(t *testing.T) {
	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	ip := net.ParseIP("192.0.2.10")
	cases := []struct {
		name      string
		reachable bool
		arpErr    error
		want      pollErrKind
	}{
		{"жив на L2", true, nil, errBad},
		{"нет на L2", false, nil, errNoRoute},
		{"ARP ошибка → нет сети", false, errors.New("netlink"), errNoRoute},
	}
	for _, tc := range cases {
		check := func(context.Context, net.IP) (bool, error) { return tc.reachable, tc.arpErr }
		err := classifyPoll(context.Background(), timeout, ip, check)
		pe, ok := err.(*pollError)
		if !ok || pe.kind != tc.want {
			t.Fatalf("%s: получено %+v, ожидалось %v", tc.name, pe, tc.want)
		}
		// dial timeout при живом ARP должен повторяться, иначе — нет.
		if got := retryableBad(err); got != (tc.want == errBad) {
			t.Fatalf("%s: retryableBad=%v, want %v", tc.name, got, tc.want == errBad)
		}
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

func TestClassifyTimeoutIPv6(t *testing.T) {
	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	check := func(context.Context, net.IP) (bool, error) { return false, errors.New("ARP: не IPv4") }
	pe, ok := classifyPoll(context.Background(), timeout, net.ParseIP("2001:db8::1"), check).(*pollError)
	if !ok || pe.kind != errNoRoute {
		t.Fatalf("IPv6/неоднозначность → нет сети, получено %+v", pe)
	}
}

func TestTimeoutChainToBan(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	check := func(context.Context, net.IP) (bool, error) { return false, nil }
	err := classifyPoll(context.Background(), timeout, net.ParseIP("192.0.2.10"), check)
	p.fail(0, err)
	st := p.states[0]
	if st.banStatus != statusBan10 || st.banUntil.Before(time.Now().Add(9*time.Minute)) {
		t.Fatalf("timeout без ARP должен давать бан 10 мин: %+v", st)
	}
}

func TestSoftBackoffBoundary(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	p.fail(0, badErr(solarman.ErrNoData))
	if !p.states[0].nextTry.IsZero() || !p.states[0].banUntil.IsZero() {
		t.Fatalf("после 1 промаха паузы быть не должно: %+v", p.states[0])
	}
	p.fail(0, badErr(solarman.ErrNoData))
	if p.states[0].nextTry.Before(time.Now().Add(50*time.Second)) || !p.states[0].banUntil.IsZero() {
		t.Fatalf("после 2 промахов ожидалась пауза 1 мин без бана: %+v", p.states[0])
	}
}

func TestStatusAfterBanExpires(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 1)
	st := &p.states[0]
	st.status = statusBad
	st.banUntil = time.Now().Add(-time.Second)
	st.banStatus = statusBan5
	if got := p.currentStatus(0, time.Now(), 10*time.Second); got != statusBad {
		t.Fatalf("после истечения бана ожидался statusBad: %d", got)
	}
	st.status = 0
	if got := p.currentStatus(0, time.Now(), 10*time.Second); got != statusInit {
		t.Fatalf("без данных ожидался statusInit: %d", got)
	}
}

func TestRetryableBad(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no data", badErr(solarman.ErrNoData), false},
		{"неполный блок", badErr(errors.New("неполный блок Sofar")), true},
		{"dial timeout при живом ARP", &pollError{kind: errBad, err: errors.New("dial timeout")}, true},
		{"ошибка connect", &pollError{kind: errOther, err: errors.New("refused")}, false},
	}
	for _, c := range cases {
		if got := retryableBad(c.err); got != c.want {
			t.Errorf("%s: retryableBad=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestTimeoutsForType(t *testing.T) {
	if connectTimeout != time.Second {
		t.Fatalf("connectTimeout=%v", connectTimeout)
	}
	if got := readTimeoutFor("deye"); got != 2*time.Second {
		t.Fatalf("deye read=%v", got)
	}
	if got := readTimeoutFor("sofar"); got != 14*time.Second {
		t.Fatalf("sofar first byte=%v", got)
	}
	if got := totalTimeoutFor("sofar"); got != 20*time.Second {
		t.Fatalf("sofar total=%v", got)
	}
	if got := totalTimeoutFor("deye"); got != 2*time.Second {
		t.Fatalf("deye total=%v", got)
	}
	if got := idleWindowFor("deye"); got != 2*time.Second {
		t.Fatalf("deye idle=%v", got)
	}
	if got := idleWindowFor("sofar"); got != 400*time.Millisecond {
		t.Fatalf("sofar idle=%v", got)
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

func TestLongBanRequiresActiveBan(t *testing.T) {
	st := &devState{banStatus: statusBan5}
	if longBan(st, time.Now()) {
		t.Fatal("banStatus без banUntil не должен считаться активным баном")
	}
	st.banUntil = time.Now().Add(-time.Second)
	if longBan(st, time.Now()) {
		t.Fatal("истёкший бан не должен считаться активным")
	}
	st.banUntil = time.Now().Add(time.Minute)
	if !longBan(st, time.Now()) {
		t.Fatal("активный бан 5 мин должен считаться длинным")
	}
}

func TestSuccessAndFailClearBanStatus(t *testing.T) {
	shm, err := shmOpen(filepath.Join(t.TempDir(), "grid"))
	if err != nil {
		t.Fatal(err)
	}
	defer shm.Close()
	p := newPoller(shm, nil, logx.New("test: "))
	p.reset(settings{Inverters: []inverter{{Name: "x", Type: "sofar", Valid: true}}})
	st := &p.states[0]
	st.banStatus = statusBan5
	p.success(0, telemetry{}, "SN")
	if st.banStatus != 0 || !st.banUntil.IsZero() {
		t.Fatalf("после успеха остался статус бана: %+v", st)
	}
	st.banStatus = statusBan10
	st.banUntil = time.Time{}
	p.fail(0, badErr(solarman.ErrNoData))
	if st.banStatus != 0 {
		t.Fatalf("после errBad остался статус бана: %+v", st)
	}
}

func TestStaleThresholdPerInverter(t *testing.T) {
	p := newPoller(nil, nil, logx.New("test: "))
	p.states = make([]devState, 2)
	p.states[0].lastDuration = 30 * time.Second
	p.states[1].lastDuration = 2 * time.Second
	if got, want := p.staleThreshold(0, 10*time.Second), 105*time.Second; got != want {
		t.Fatalf("stale slot 0=%v, want %v", got, want)
	}
	if got, want := p.staleThreshold(1, 10*time.Second), 35*time.Second; got != want {
		t.Fatalf("stale slot 1=%v, want %v", got, want)
	}
}
