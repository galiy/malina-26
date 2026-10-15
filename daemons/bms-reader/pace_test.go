package main

import (
	"fmt"
	"testing"
)

// TestPaceParse — разбор синтетического ответа PACE (без INFO).
func TestPaceParse(t *testing.T) {
	body := "20004600" + "0000" // VER=20 ADR=00 46 RTN=00 LEN=0000
	frame := fmt.Sprintf("~%s%04X", body, chksum([]byte(body)))
	f, err := parsePace([]byte(frame))
	if err != nil {
		t.Fatal(err)
	}
	if f.ADR != 0 || f.RTN != 0 || len(f.Info) != 0 {
		t.Fatalf("pace: %+v", f)
	}
}

// TestPaceBadChk — неверная CHK отклоняется.
func TestPaceBadChk(t *testing.T) {
	if _, err := parsePace([]byte("~2000460000000000")); err == nil {
		t.Fatal("ожидалась ошибка CHK")
	}
}

// TestBuildPace — запрос SN (0xA4) формируется и содержит CID2.
func TestBuildPace(t *testing.T) {
	req := buildPace(3, 0xA4, nil)
	s := string(req)
	if s[0] != '~' || s[len(s)-1] != '\r' {
		t.Fatalf("обрамление: %q", s)
	}
	if s[5:7] != "46" || len(s) < 12 {
		t.Fatalf("формат: %q", s)
	}
}
