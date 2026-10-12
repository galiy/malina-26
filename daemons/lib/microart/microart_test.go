package microart

import (
	"errors"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	// Чтение: 'r', page=0, addr=0x0000.
	req, err := ParseRequestFrame(BuildReadRequest(0x0000, 0))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if req.Cmd != 'r' || req.Page != 0 || req.Addr != 0x0000 {
		t.Fatalf("read parsed: %+v", req)
	}
	// Чтение с экранированием (addr с байтом 0x0A): 0x0A0A → должен экранироваться.
	req, err = ParseRequestFrame(BuildReadRequest(0x0A0A, 0x0A))
	if err != nil {
		t.Fatalf("esc read: %v", err)
	}
	if req.Addr != 0x0A0A || req.Page != 0x0A {
		t.Fatalf("esc parsed: %+v", req)
	}
	// Запись блока с байтами 0x0A/0xDB.
	data := []byte{0x00, 0x0A, 0xDB, 0x05}
	req, err = ParseRequestFrame(BuildWriteRequest(0x1B0, data))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if req.Cmd != 'w' || req.Addr != 0x1B0 || len(req.Data) != 4 {
		t.Fatalf("write parsed: %+v", req)
	}
	for i := range data {
		if req.Data[i] != data[i] {
			t.Fatalf("data[%d]=%#x, ждали %#x", i, req.Data[i], data[i])
		}
	}
}

func TestRequestCRCIsEscapeByte(t *testing.T) {
	// 0x02C2/0xEF даёт CRC=0xDB — проверяем, что парсер не путает CRC с экранированием.
	req, err := ParseRequestFrame(BuildReadRequest(0x02C2, 0xEF))
	if err != nil {
		t.Fatalf("addr 0x02C2: %v", err)
	}
	if req.Addr != 0x02C2 || req.Page != 0xEF {
		t.Fatalf("parsed %+v", req)
	}
	// CRC == 0x0A (терминатор отсутствует).
	found := false
	for a := 0; a < 0x10000; a += 0x100 {
		for pg := 0; pg < 0x100; pg++ {
			fr := BuildReadRequest(uint16(a), byte(pg))
			if len(fr) > 0 && fr[len(fr)-1] == 0x0A {
				continue
			}
			if _, err := ParseRequestFrame(fr); err != nil {
				t.Fatalf("addr=%#x page=%#x: %v", a, pg, err)
			}
			found = true
			break
		}
		if found {
			break
		}
	}
}

func TestReplyRoundTrip(t *testing.T) {
	data := []byte{0x00, 0x0A, 0xDB, 0x00}
	got, err := ParseReply(BuildReply(data), byte(len(data)-1))
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("reply[%d]=%#x, ждали %#x", i, got[i], data[i])
		}
	}
}

func TestErrorReplyRoundTrip(t *testing.T) {
	_, err := ParseReply(BuildErrorReply(0x10), 0)
	var de *DeviceError
	if !errors.As(err, &de) || de.Code != 0x10 {
		t.Fatalf("error reply: %v", err)
	}
}

func TestParseRequestBadSum(t *testing.T) {
	fr := BuildReadRequest(0x0000, 0)
	fr[len(fr)-1] ^= 0xFF // ломаем сумму
	if _, err := ParseRequestFrame(fr); !errors.Is(err, ErrBadReply) && !errors.Is(err, ErrNoDevice) {
		t.Fatalf("битая сумма: %v", err)
	}
}
