package snmp

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeInteger(t *testing.T) {
	for _, v := range []int64{0, 1, 127, 128, 255, 256, 1000, -1, -128, -129, 2147483647, -2147483648} {
		b := encodeInteger(v)
		got, err := DecodeValue(b)
		if err != nil {
			t.Fatalf("decode %d: %v", v, err)
		}
		if got.Kind != VInt || got.Int != v {
			t.Fatalf("value %d → %+v", v, got)
		}
	}
}

func TestOIDRoundTrip(t *testing.T) {
	cases := [][]int{
		{1, 3, 6, 1, 4, 1, 50330},
		{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1},
		{1, 3, 6, 1, 4, 1, 50330, 2, 3, 4},
	}
	for _, oid := range cases {
		b := encodeOID(oid)
		blk, _, err := decodeTLV(b)
		if err != nil {
			t.Fatalf("decode tlv: %v", err)
		}
		got, err := decodeOIDContent(blk.content)
		if err != nil {
			t.Fatalf("decode %v: %v", oid, err)
		}
		if !equalInts(got, oid) {
			t.Fatalf("oid %v → %v", oid, got)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMessageRoundTrip(t *testing.T) {
	msg := &Message{
		Version:   Version2c,
		Community: "public",
		PDU: &PDU{
			Type:        PDUGetRequest,
			RequestID:   12345,
			ErrorStatus: 0,
			ErrorIndex:  0,
			Varbinds: []Varbind{
				{OID: []int{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1}, Value: encodeNull()},
			},
		},
	}
	b := EncodeMessage(msg)
	// sanity: сообщение начинается с SEQUENCE
	if b[0] != 0x30 {
		t.Fatalf("ожидали SEQUENCE, получили 0x%02X", b[0])
	}
	got, err := DecodeMessage(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != Version2c || got.Community != "public" {
		t.Fatalf("msg: %+v", got)
	}
	if got.PDU.RequestID != 12345 || len(got.PDU.Varbinds) != 1 {
		t.Fatalf("pdu: %+v", got.PDU)
	}
	if !equalInts(got.PDU.Varbinds[0].OID, []int{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1}) {
		t.Fatalf("oid: %v", got.PDU.Varbinds[0].OID)
	}
	v, err := DecodeValue(got.PDU.Varbinds[0].Value)
	if err != nil || v.Kind != VNull {
		t.Fatalf("value: %+v %v", v, err)
	}
}

func TestTrapVarbinds(t *testing.T) {
	vbs := TrapVarbinds(1000, []int{1, 3, 6, 1, 4, 1, 50330, 0, 1},
		Varbind{OID: []int{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1}, Value: encodeInteger(230)})
	if len(vbs) != 3 {
		t.Fatalf("len=%d", len(vbs))
	}
	// системные varbind'ы на месте (sysUpTime и snmpTrapOID)
	if !equalInts(vbs[0].OID, []int{1, 3, 6, 1, 2, 1, 1, 3, 0}) {
		t.Fatalf("sysUpTime oid=%v", vbs[0].OID)
	}
	if !equalInts(vbs[1].OID, []int{1, 3, 6, 1, 6, 3, 1, 1, 4, 1, 0}) {
		t.Fatalf("trapOID oid=%v", vbs[1].OID)
	}
	// trapOID значение — правильный OID нотификации (полный BER-блок OID)
	if !bytes.Equal(vbs[1].Value, encodeOID([]int{1, 3, 6, 1, 4, 1, 50330, 0, 1})) {
		t.Fatalf("trapOID value mismatch")
	}
}

func TestExceptionValue(t *testing.T) {
	b := encodeException(ExcepNoSuchInstance)
	// по RFC 3416: context-specific primitive тег [1] + нулевая длина → 0x81 0x00
	if len(b) != 2 || b[0] != ExcepNoSuchInstance || b[1] != 0x00 {
		t.Fatalf("кодирование исключения: % X", b)
	}
	v, err := DecodeValue(b)
	if err != nil || v.Kind != VExcep || v.Excep != ExcepNoSuchInstance {
		t.Fatalf("excep: %+v %v", v, err)
	}
	// endOfMibView → 0x82 0x00
	if be := encodeException(ExcepEndOfMibView); len(be) != 2 || be[0] != 0x82 {
		t.Fatalf("endOfMibView: % X", be)
	}
}
