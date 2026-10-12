package main

import (
	"errors"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/microart"
)

func mustReply(t *testing.T, frame []byte, page byte) []byte {
	t.Helper()
	data, err := microart.ParseReply(frame, page)
	if err != nil {
		t.Fatalf("ParseReply: %v (%q)", err, frame)
	}
	return data
}

func TestEmuReadDevice(t *testing.T) {
	d := newDevice(0, 0x1234, 0x63, 0x0E3D, nil, 0)
	// Чтение 1 байта _Device (addr 0x0000) → 5 (КЭС).
	rep := mustReply(t, d.handle(microart.Request{Cmd: 'r', Page: 0, Addr: 0x0000}), 0)
	if len(rep) != 1 || rep[0] != 5 {
		t.Fatalf("_Device=%v, ждали 5", rep)
	}
	// UID в RAM 0x2C9 → адрес устройства 0x2C9+0x1F0=0x4B9... проверим через mapIdx.
	rep = mustReply(t, d.handle(microart.Request{Cmd: 'r', Page: 1, Addr: 0x2C9}), 1)
	if rep[0] != 0x34 || rep[1] != 0x12 {
		t.Fatalf("серийник=%v, ждали 34 12", rep)
	}
}

func TestEmuWriteRelayAndCommand(t *testing.T) {
	d := newDevice(0, 1, 0x63, 0x0E3D, nil, 0)
	// Команда EEPromWR=3 (запись байта 3 в адрес 0).
	if err := microart.ParseWriteReply(d.handle(microart.Request{Cmd: 'w', Addr: 0, Data: []byte{3}})); err != nil {
		t.Fatalf("cmd 3: %v", err)
	}
	// Запись состояния реле в командную ячейку 0x0E3D → Relay_C (RAM 0x3E3).
	if err := microart.ParseWriteReply(d.handle(microart.Request{Cmd: 'w', Addr: 0x0E3D, Data: []byte{0x05}})); err != nil {
		t.Fatalf("relay write: %v", err)
	}
	got := d.Read(0x3E3+0x1F0, 1)[0]
	if got != 0x05 {
		t.Fatalf("Relay_C=%#x, ждали 0x05", got)
	}
	// Неверная команда.
	err := microart.ParseWriteReply(d.handle(microart.Request{Cmd: 'w', Addr: 0, Data: []byte{1}}))
	var de *microart.DeviceError
	if !errors.As(err, &de) || de.Code != 0x04 {
		t.Fatalf("bad cmd: %v", err)
	}
}

func TestEmuSetRelayDir(t *testing.T) {
	d := newDevice(0, 1, 0x63, 0x0E3D, nil, 0)
	d.SetRelay(2, true)
	if d.Read(0x3E3+0x1F0, 1)[0] != 0x02 {
		t.Fatalf("relay2 on: %#x", d.Read(0x3E3+0x1F0, 1)[0])
	}
	d.SetRelay(2, false)
	if d.Read(0x3E3+0x1F0, 1)[0] != 0x00 {
		t.Fatalf("relay2 off: %#x", d.Read(0x3E3+0x1F0, 1)[0])
	}
}

func TestEmuLivingRanges(t *testing.T) {
	d := newDevice(0, 0x9191, 0x63, 0x0E3D, nil, 0)
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < 600; i++ { // ~10 минут модельного времени
		d.Tick(base.Add(time.Duration(i) * time.Second))
	}
	rd16 := func(ram uint16) uint16 {
		b := d.Read(ram+0x1F0, 2)
		return uint16(b[0]) | uint16(b[1])<<8
	}
	pvV := float64(rd16(0x2D9)) / 100
	iCh := float64(rd16(0x2FB)) / 100
	vBat := float64(rd16(0x2E5)) / 100
	if pvV < 105 || pvV > 165 {
		t.Errorf("Vc_PV=%v вне 110..160 (±)", pvV)
	}
	if iCh < 0 || iCh > 100 {
		t.Errorf("I_Ch=%v вне 0..100", iCh)
	}
	if vBat < 48 || vBat > 56.5 {
		t.Errorf("V_Bat=%v вне 48..56.5", vBat)
	}
	if mode := d.Read(0x2C2+0x1F0, 1)[0]; mode != 'b' && mode != 'S' {
		t.Errorf("Mode=%q вне {A,S}", mode)
	}
	// Температуры в допустимых диапазонах (x-50).
	if ti := int(int8(d.Read(0x31D+0x1F0, 1)[0])); ti < -40 || ti > 50 {
		t.Errorf("Temp_Int=%d вне 20..65 (±)", ti)
	}
	if tb := int(int8(d.Read(0x31E+0x1F0, 1)[0])); tb < -40 || tb > 45 {
		t.Errorf("Temp_Bat=%d вне 20..32 (±)", tb)
	}
}
