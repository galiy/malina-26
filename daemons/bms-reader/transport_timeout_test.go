package main

import (
	"testing"
	"time"
)

// TestIOTimeout — таймаут опроса по скорости порта (arch §10).
func TestIOTimeout(t *testing.T) {
	cases := []struct {
		baud int
		want time.Duration
	}{
		{0, 500 * time.Millisecond},     // неизвестно (TCP)
		{2400, 2800 * time.Millisecond}, // 700 * 9600/2400
		{4800, 1400 * time.Millisecond}, // 700 * 9600/4800
		{9600, 700 * time.Millisecond},
		{14400, 700 * time.Millisecond},
		{19200, 500 * time.Millisecond},
		{38400, 500 * time.Millisecond}, // между 19200 и 57600
		{57600, 200 * time.Millisecond},
		{115200, 200 * time.Millisecond},
		{230400, 200 * time.Millisecond},
	}
	for _, c := range cases {
		if got := ioTimeout(c.baud); got != c.want {
			t.Errorf("ioTimeout(%d) = %s, ожидалось %s", c.baud, got, c.want)
		}
	}
}
