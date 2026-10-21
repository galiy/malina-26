package main

import (
	"testing"
	"time"
)

func TestPrioritize(t *testing.T) {
	in := []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "/dev/ttyUSB3"}
	got := prioritize(in, "/dev/ttyUSB3")
	want := []string{"/dev/ttyUSB3", "/dev/ttyUSB0", "/dev/ttyUSB1"}
	if len(got) != len(want) {
		t.Fatalf("prioritize: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prioritize: %v, ждали %v", got, want)
		}
	}
	if got := prioritize([]string{"a", "b"}, ""); got[0] != "a" {
		t.Fatalf("пустой prefer: %v", got)
	}
	if got := prioritize([]string{"a", "b"}, "c"); got[0] != "a" {
		t.Fatalf("prefer отсутствует: %v", got)
	}
}

// TestBeginScanClearsBackoff — смена скорости снимает баны портов.
func TestBeginScanClearsBackoff(t *testing.T) {
	sc := newPortScanner(make(chan struct{}))
	sc.backoff[42] = time.Now().Add(time.Hour)
	sc.beginScan(19200)
	if !sc.inBackoff(42) {
		t.Fatal("бан снят без смены скорости")
	}
	sc.beginScan(57600)
	if sc.inBackoff(42) {
		t.Fatal("бан не снят при смене скорости")
	}
}
