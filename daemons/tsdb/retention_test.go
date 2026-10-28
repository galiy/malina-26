package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Активный сегмент учитывается в объёме: при total (sealed+active) > limit
// удаляется старейший sealed-сегмент, даже если сумма sealed формально ниже
// лимита. Регрессия 2026-10-28: на tmpfs raw сумма sealed не превышала лимит
// (лимит ≈ размер ФС) и очистка не срабатывала → ENOSPC.
func TestRetentionCountsActiveSegment(t *testing.T) {
	restore := freeSpaceFn
	freeSpaceFn = func(string) (int64, error) { return 1 << 40, nil }
	defer func() { freeSpaceFn = restore }()

	dir := t.TempDir()
	mk := func(name string, size int, maxTime int64) *segRef {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return &segRef{path: p, maxTime: maxTime}
	}
	old := mk("old.ts", 400, 1)
	mid := mk("mid.ts", 400, 2)
	ser := &series{
		sealed: []*segRef{old, mid},
		active: &activeSeg{size: 300},
	}
	s := &Store{
		root:       dir,
		limitBytes: 1000,
		series:     map[seriesKey]*series{{typ: "map", id: "1"}: ser},
	}
	// total = 400+400+300 = 1100 > 1000 → удалить старейший (old).
	s.retentionSweep()

	if _, err := os.Stat(old.path); !os.IsNotExist(err) {
		t.Fatalf("старейший sealed не удалён: %v", err)
	}
	if _, err := os.Stat(mid.path); err != nil {
		t.Fatalf("новейший sealed удалён: %v", err)
	}
	if len(ser.sealed) != 1 || ser.sealed[0] != mid {
		t.Fatalf("список sealed не обновлён: %+v", ser.sealed)
	}
}

// Если объём в пределах лимита, ничего не удаляем.
func TestRetentionKeepsUnderLimit(t *testing.T) {
	restore := freeSpaceFn
	freeSpaceFn = func(string) (int64, error) { return 1 << 40, nil }
	defer func() { freeSpaceFn = restore }()

	dir := t.TempDir()
	p := filepath.Join(dir, "a.ts")
	if err := os.WriteFile(p, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := &segRef{path: p, maxTime: 1}
	ser := &series{sealed: []*segRef{ref}, active: &activeSeg{size: 100}}
	s := &Store{
		root:       dir,
		limitBytes: 1000,
		series:     map[seriesKey]*series{{typ: "map", id: "1"}: ser},
	}
	s.retentionSweep()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("сегмент удалён при объёме в пределах лимита: %v", err)
	}
}
