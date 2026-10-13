package main

import (
	"encoding/binary"
	"testing"
)

// TestWireMagics — магии кадров должны соответствовать 4-символьным литералам.
func TestWireMagics(t *testing.T) {
	cases := []struct {
		v uint32
		s string
	}{
		{magicWriteReq, "MTSW"},
		{magicWriteAck, "MTSK"},
		{magicReadReq, "MTSR"},
		{magicReadReply, "MTSA"},
		{magicReadEnd, "MTSE"},
		{segMagic, "MTS1"},
		{idxMagic, "MTSX"},
	}
	for _, c := range cases {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], c.v)
		if string(b[:]) != c.s {
			t.Fatalf("0x%08X → %q, ожидалось %q", c.v, string(b[:]), c.s)
		}
	}
}
