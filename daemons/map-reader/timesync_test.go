package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeDecodeTime(t *testing.T) {
	for _, hm := range [][2]int{{0, 0}, {9, 54}, {20, 43}, {23, 59}, {12, 7}} {
		now := time.Date(2026, 10, 10, hm[0], hm[1], 0, 0, time.Local)
		tc, tm := encodeClock(now)
		mem := memMap{cellTimeCyr: tc, cellTimeMin: tm}
		got, ok := decodeMapTime(mem)
		if !ok || got != hm[0]*60+hm[1] {
			t.Fatalf("encode/decode %02d:%02d: got %d ok=%v (tc=%#x tm=%d)", hm[0], hm[1], got, ok, tc, tm)
		}
	}
	// Битые значения.
	if _, ok := decodeMapTime(memMap{cellTimeCyr: 0xFF, cellTimeMin: 0}); ok {
		t.Error("часы 0x1F должны отвергаться")
	}
	if _, ok := decodeMapTime(memMap{cellTimeCyr: 0}); ok {
		t.Error("отсутствующая ячейка минут должна отвергаться")
	}
}

func TestTenMinWindow(t *testing.T) {
	loc := time.Local
	w := 5 * time.Second
	cases := []struct {
		min      int
		sec      int
		inWindow bool
	}{
		{30, 0, true},  // ровно точка
		{30, 5, true},  // конец окна
		{30, 6, false}, // вне окна
		{30, 30, false},
		{30, 55, false}, // «до» точки не открываем
		{33, 0, false},  // НЕ «круглая» минута — окна нет
		{39, 3, false},  // НЕ «круглая» минута
		{40, 2, true},   // следующая круглая точка (12:40)
	}
	for _, c := range cases {
		now := time.Date(2026, 10, 10, 12, c.min, c.sec, 0, loc)
		b, in := tenMinWindow(now, w)
		if in != c.inWindow {
			t.Errorf("%02d:%02d: inWindow=%v, want %v", c.min, c.sec, in, c.inWindow)
			continue
		}
		if !in {
			continue
		}
		want := time.Date(2026, 10, 10, 12, c.min, 0, 0, loc).Unix()
		if b != want {
			t.Errorf("%02d:%02d: boundary=%d, want %d", c.min, c.sec, b, want)
		}
	}
}

func TestMaybeSyncFlagDisables(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "off")
	if err := os.WriteFile(flag, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config{timeSyncOff: flag, timeSyncWindow: 5 * time.Second, timeSyncThreshMin: 1}
	e := &MapEntry{}
	// В окне, время МАП заведомо «неверное» — но флаг должен остановить до записи.
	now := time.Date(2026, 10, 10, 12, 30, 0, 0, time.Local)
	mem := memMap{cellTimeCyr: 0x00, cellTimeMin: 0}
	maybeSyncTime(cfg, nil, nil, "microart", nil, e, mem, now) // не должно паниковать/писать
	if e.syncWin != 0 {
		t.Fatalf("при выключенном флаге окно не должно отмечаться: %d", e.syncWin)
	}
}

func TestMaybeSyncWindowAndOnce(t *testing.T) {
	cfg := &config{timeSyncOff: "", timeSyncWindow: 5 * time.Second, timeSyncThreshMin: 1}
	e := &MapEntry{}
	mem := memMap{cellTimeCyr: 0x00, cellTimeMin: 0} // МАП 00:00

	// Вне окна — ничего не делаем.
	maybeSyncTime(cfg, nil, nil, "microart", nil, e, mem, time.Date(2026, 10, 10, 12, 30, 30, 0, time.Local))
	if e.syncWin != 0 {
		t.Fatalf("вне окна окно не должно отмечаться: %d", e.syncWin)
	}

	// В окне, расхождение 750 мин → попытка; окно отмечается (executeWrite с
	// пустым реестром вернёт no_device, порт не нужен).
	now := time.Date(2026, 10, 10, 12, 30, 0, 0, time.Local)
	maybeSyncTime(cfg, nil, nil, "microart", nil, e, mem, now)
	b, _ := tenMinWindow(now, cfg.timeSyncWindow)
	if e.syncWin != b {
		t.Fatalf("в окне окно должно быть отмечено %d, got %d", b, e.syncWin)
	}
}

func TestQuietPeriodAndThreshold(t *testing.T) {
	loc := time.Local
	at := func(h, m int) time.Time { return time.Date(2026, 10, 10, h, m, 0, 0, loc) }

	// Границы ночного окна [22:30, 07:30).
	for _, c := range []struct {
		h, m int
		want bool
	}{
		{22, 29, false}, {22, 30, true}, {23, 59, true}, {0, 0, true},
		{7, 29, true}, {7, 30, false}, {12, 0, false},
	} {
		if got := inQuietPeriod(at(c.h, c.m)); got != c.want {
			t.Errorf("inQuietPeriod(%02d:%02d)=%v, want %v", c.h, c.m, got, c.want)
		}
	}

	// День: корректируем при diff > порога (1), не при diff <= 1.
	if shouldCorrectTime(at(12, 0), 1, 1) {
		t.Error("днём diff=1 не должен корректироваться")
	}
	if !shouldCorrectTime(at(12, 0), 2, 1) {
		t.Error("днём diff=2 должен корректироваться")
	}

	// Ночь: корректируем только при diff >= 5 мин.
	for _, d := range []int{0, 1, 4} {
		if shouldCorrectTime(at(23, 0), d, 1) {
			t.Errorf("ночью diff=%d не должен корректироваться", d)
		}
	}
	for _, d := range []int{5, 6, 60} {
		if !shouldCorrectTime(at(23, 0), d, 1) {
			t.Errorf("ночью diff=%d должен корректироваться", d)
		}
	}
}
