package main

import "testing"

func TestNowMillis(t *testing.T) {
	if got := nowMillis(); got < 1_000_000_000_000 {
		t.Fatalf("время raw должно быть в миллисекундах, получено %d", got)
	}
}
