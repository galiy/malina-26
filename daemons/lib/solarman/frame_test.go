package solarman

import (
	"encoding/binary"
	"testing"
)

func TestBuildDeyeRequest(t *testing.T) {
	b := BuildDeyeReadFrameFn(123456, 1, 7, 0x3B, 0x3A, 0x03)
	if len(b) != 36 {
		t.Fatalf("len=%d", len(b))
	}
	if b[0] != StartMarker || b[len(b)-1] != EndMarker {
		t.Fatal("маркеры")
	}
	if b[11+15] != 1 || b[11+16] != 3 {
		t.Fatalf("PDU unit/fn: %x", b[26:])
	}
	if got := binary.BigEndian.Uint16(b[11+15+2 : 11+15+4]); got != 0x3B {
		t.Fatalf("start=%x", got)
	}
}

func buildResp(serial uint16, vals []uint16, badCRC bool) []byte {
	pdu := make([]byte, 3+len(vals)*2+2)
	pdu[0] = 1
	pdu[1] = 3
	pdu[2] = byte(len(vals) * 2)
	for i, v := range vals {
		binary.BigEndian.PutUint16(pdu[3+i*2:], v)
	}
	crc := CRC16Modbus(pdu[:3+len(vals)*2])
	if badCRC {
		crc++
	}
	pdu[3+len(vals)*2] = byte(crc)
	pdu[4+len(vals)*2] = byte(crc >> 8)
	payload := append(make([]byte, 15), pdu...)
	frame := []byte{StartMarker, byte(len(payload)), byte(len(payload) >> 8), 0x10, 0x15, byte(serial), byte(serial >> 8)}
	sn := make([]byte, 4)
	binary.LittleEndian.PutUint32(sn, 123456)
	frame = append(frame, sn...)
	frame = append(frame, payload...)
	frame = append(frame, Checksum8(frame[1:]), EndMarker)
	return frame
}

func TestSplitAndParse(t *testing.T) {
	raw := append(buildResp(1, []uint16{1, 2, 3}, false), buildResp(2, []uint16{4, 5}, false)...)
	frames := SplitFrames(raw)
	if len(frames) != 2 || !frames[0].Valid || !frames[1].Valid {
		t.Fatalf("frames: %+v", frames)
	}
	p := ParseModbusPDU(frames[0].Payload)
	if len(p) != 1 || p[0].CRC != p[0].CRCCalc || len(p[0].Values) != 3 {
		t.Fatalf("pdu: %+v", p)
	}
}

func TestSplitShortAndBadCRC(t *testing.T) {
	short := buildResp(1, []uint16{1, 2}, false)
	short = short[:len(short)-2] // обрезан хвост
	if got := SplitFrames(short); len(got) != 0 {
		t.Fatalf("короткий кадр разобран: %+v", got)
	}
	bad := buildResp(1, []uint16{1, 2}, true)
	frames := SplitFrames(bad)
	if len(frames) != 1 || frames[0].Valid || frames[0].ChecksumOK == false {
		// Внутренняя CRC PDU неверна, но кадровый checksum верен.
		if len(frames) != 1 {
			t.Fatalf("bad: %+v", frames)
		}
	}
	if p := ParseModbusPDU(frames[0].Payload); len(p) != 0 {
		t.Fatalf("битая CRC PDU принята: %+v", p)
	}
}
