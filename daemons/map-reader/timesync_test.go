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
		sec       int
		inWindow  bool
		boundaryS int // секунды внутри 10-минутного блока у ожидаемой границы
	}{
		{0, true, 0},    // ровно точка
		{5, true, 0},    // конец «после»
		{6, false, 0},   // вне окна
		{30, false, 0},  // середина
		{59, true, 600}, // «до» следующей точки
		{55, true, 600}, // начало «до»
		{54, false, 0},  // вне окна «до»
	}
	for _, c := range cases {
		now := time.Date(2026, 10, 10, 12, 30, c.sec, 0, loc)
		b, in := tenMinWindow(now, w)
		if in != c.inWindow {
			t.Errorf("sec=%d: inWindow=%v, want %v", c.sec, in, c.inWindow)
			continue
		}
		if !in {
			continue
		}
		// Ожидаемая граница: 12:30:00 (+600 если sec>=55).
		want := time.Date(2026, 10, 10, 12, 30, 0, 0, loc).Unix() + int64(c.boundaryS)
		if b != want {
			t.Errorf("sec=%d: boundary=%d, want %d", c.sec, b, want)
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
