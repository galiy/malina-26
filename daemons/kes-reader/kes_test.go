package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestParseKesAndData(t *testing.T) {
	img := map[uint16]byte{}
	put16 := func(a, v uint16) { img[a] = byte(v); img[a+1] = byte(v >> 8) }

	// ID-окно.
	img[0x0000] = 5 // _Device = КЭС
	img[0x0002] = 0x63
	img[0x2C9] = 0x34
	img[0x2CA] = 0x12 // UID = 0x1234
	img[0x2C2] = 'A'  // Mode
	// Измерения.
	put16(0x2D9, 1234) // Vc_PV
	put16(0x2E5, 567)  // V_Bat
	img[0x31D] = 0x5A  // Temp_Int (байт)
	img[0x31E] = 0x64  // Temp_Bat (байт)
	img[0x3E3] = 0x05  // Relay_C
	img[0x3B6] = 0x02  // RSErrSis
	put16(0x3F8, 300)  // windspeed

	info := parseKes(img)
	if info.Device != 5 {
		t.Fatalf("Device=%d, ждали 5", info.Device)
	}
	if info.UID != 0x1234 {
		t.Fatalf("UID=%#x, ждали 0x1234", info.UID)
	}
	if info.Mode != 'A' {
		t.Fatalf("Mode=%q, ждали A", info.Mode)
	}

	d := parseData(img)
	if d.VcPV != 1234 || d.VBat != 567 || d.RelayC != 0x05 || d.RSErrSis != 0x02 || d.Wind != 300 {
		t.Fatalf("parseData: %+v", d)
	}
	if d.TempInt != 0x5A || d.TempBat != 0x64 {
		t.Fatalf("температуры (байты): %+v", d)
	}
	if d.Mode != 'A' {
		t.Fatalf("Mode mapper: %q", d.Mode)
	}

	p := buildPayload(contractMeta{UID: info.UID, Port: "/dev/ttyACM0", LastSeen: time.Unix(1, 0)}, d)
	if len(p) < 64 {
		t.Fatalf("payload короткий: %d", len(p))
	}
	if string(p[0:4]) != "KES1" {
		t.Fatalf("magic payload: %q", p[0:4])
	}
	if got := binary.LittleEndian.Uint16(p[6:8]); got != 0x1234 {
		t.Fatalf("UID payload: %#x", got)
	}
	if got := binary.LittleEndian.Uint16(p[14:16]); got != 1234 {
		t.Fatalf("VcPV payload: %d", got)
	}
}

func TestMapChar(t *testing.T) {
	if mapChar('A') != 'A' {
		t.Fatal("печатный символ должен сохраняться")
	}
	if mapChar(0x20) != 'U' || mapChar(0x7D) != 'U' || mapChar(0x00) != 'U' {
		t.Fatal("непечатные должны мапиться в 'U'")
	}
}

func TestModelCode(t *testing.T) {
	cases := map[string]byte{"200/100": '1', "200/60": '2', "250/60": '3', "": 'K'}
	for m, want := range cases {
		if got := modelCode(m); got != want {
			t.Errorf("modelCode(%q)=%q, ждали %q", m, got, want)
		}
	}
}

func TestValidateWrite(t *testing.T) {
	okReg := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "E3D", Data: "05"}}}
	if k, err := validateWrite(okReg); err != nil || k != kindReg {
		t.Fatalf("reg: kind=%v err=%v", k, err)
	}
	okCmd := writeRequest{Ops: []writeOp{{Op: "cmd", Code: "08"}}}
	if k, err := validateWrite(okCmd); err != nil || k != kindCmd {
		t.Fatalf("cmd: kind=%v err=%v", k, err)
	}
	okRelay := writeRequest{Ops: []writeOp{{Op: "relay", Relay: 2, Mode: "on"}}}
	if k, err := validateWrite(okRelay); err != nil || k != kindRelay {
		t.Fatalf("relay: kind=%v err=%v", k, err)
	}
	if _, err := validateWrite(writeRequest{}); err == nil {
		t.Fatal("пустой запрос должен быть ошибкой")
	}
	if _, err := validateWrite(writeRequest{Ops: []writeOp{{Op: "relay", Relay: 4, Mode: "on"}}}); err == nil {
		t.Fatal("relay=4 должен быть ошибкой")
	}
	mix := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "0", Data: "01"}, {Op: "cmd", Code: "03"}}}
	if _, err := validateWrite(mix); err == nil {
		t.Fatal("смешение reg/cmd должно быть ошибкой")
	}
}

func TestBuildSequenceReg(t *testing.T) {
	wr := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "E3D", Data: "05"}}}
	seq := buildSequence(wr, kindReg)
	// Ожидаем: pre-команда EEPromWR (3), затем блок по адресу.
	if len(seq) != 2 || !seq[0].isCmd || seq[0].code != 3 || seq[1].addr != 0xE3D {
		t.Fatalf("seq=%+v", seq)
	}
	// NoFrame — без pre-команды.
	wr.NoFrame = true
	if seq := buildSequence(wr, kindReg); len(seq) != 1 || seq[0].addr != 0xE3D {
		t.Fatalf("noframe seq=%+v", seq)
	}
}
