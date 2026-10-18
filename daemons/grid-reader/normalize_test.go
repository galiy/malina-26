package main

import "testing"

func TestMapDeye(t *testing.T) {
	r := map[uint16]uint16{
		0x3B: 2, 0x3C: 123, 0x3F: 0x1234, 0x40: 0x0001,
		0x49: 2300, 0x4A: 0, 0x4B: 0,
		0x4C: 50, 0x4D: 0, 0x4E: 0,
		0x4F: 5000,
		0x56: 1000, 0x57: 0, 0x58: uint16(0xFFE7),
		0x5A: 350, 0x5B: 400,
		0x65: 1, 0x66: 2, 0x67: 3, 0x68: 4, 0x69: 5, 0x6A: 6,
		0x6D: 2120, 0x6E: 50, 0x6F: 2000, 0x70: 40,
	}
	got := mapDeye(r)
	want := map[uint16]int64{
		1: 2120, 2: 500, 3: 106,
		4: 2000, 5: 400, 6: 80,
		7: 10, 8: 0, 9: 5000,
		10: 2300, 11: 500, 12: 0, 13: 0, 14: 0, 15: 0,
		16: -600, 17: -650, 18: 2,
		19: 1, 20: 2, 21: 3, 22: 4, 23: 5, 24: 6,
	}
	for id, w := range want {
		if got.vals[id-1] != w {
			t.Errorf("id %d: got %d, want %d", id, got.vals[id-1], w)
		}
	}
	if got.energyToday != 123 || got.energyTotal != 0x00011234 {
		t.Errorf("energy: today=%d total=%d", got.energyToday, got.energyTotal)
	}
}

func TestMapSofar(t *testing.T) {
	r := map[uint16]uint16{
		0x0000: 2,
		0x0001: 1, 0x0002: 2, 0x0003: 3, 0x0004: 4, 0x0005: 5,
		0x0006: 2100, 0x0007: 500, 0x0008: 2000, 0x0009: 400,
		0x000A: 1000, 0x000B: 800, 0x000C: 700,
		0x000D: uint16(0xFFEC),
		0x000E: 5000,
		0x000F: 2300, 0x0010: 500, 0x0011: 0, 0x0012: 0, 0x0013: 0, 0x0014: 0,
		0x0015: 0x0001, 0x0016: 0x1234, 0x0019: 1234,
		0x001B: 25, 0x001C: 30, 0x0021: 7,
	}
	got := mapSofar(r)
	want := map[uint16]int64{
		1: 2100, 2: 500, 3: 1000, 4: 2000, 5: 400, 6: 800,
		7: 700, 8: -20, 9: 5000, 10: 2300, 11: 500,
		16: 250, 17: 300, 18: 2,
		19: 1, 20: 2, 21: 3, 22: 4, 23: 5, 24: 7,
	}
	for id, w := range want {
		if got.vals[id-1] != w {
			t.Errorf("id %d: got %d, want %d", id, got.vals[id-1], w)
		}
	}
	if got.energyToday != 123 || got.energyTotal != (0x00011234*10) {
		t.Errorf("energy: today=%d total=%d", got.energyToday, got.energyTotal)
	}
}

func TestNormalizeSofarSN(t *testing.T) {
	for in, want := range map[string]string{
		"SA3ES127LC1055V123":    "SA3ES127LC1055",
		"SA3ES127LC1055V123V45": "SA3ES127LC1055",
		"SA3ES127LC1055":        "SA3ES127LC1055",
		"V123":                  "V123",
	} {
		if got := normalizeSofarSN(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
