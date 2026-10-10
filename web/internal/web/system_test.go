package web

import (
	"path/filepath"
	"testing"
)

func TestMapTimeSyncFlag(t *testing.T) {
	off := filepath.Join(t.TempDir(), "map-time-sync.off")
	if !timeSyncEnabled(off) {
		t.Fatal("без файла синхронизация должна быть включена")
	}
	if err := setTimeSyncEnabled(off, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if timeSyncEnabled(off) {
		t.Fatal("при наличии флаг-файла синхронизация должна быть выключена")
	}
	if err := setTimeSyncEnabled(off, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !timeSyncEnabled(off) {
		t.Fatal("после включения флага нет — синхронизация включена")
	}
}
