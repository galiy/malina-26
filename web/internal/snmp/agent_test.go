package snmp

import (
	"testing"
)

// fakeHandler реализует Handler на малой таблице OID.
type fakeHandler struct{ tbl map[string]Value }

func (f fakeHandler) Get(oid []int) (Value, bool) {
	v, ok := f.tbl[oidKeyStr(oid)]
	return v, ok
}

func (f fakeHandler) Next(oid []int) ([]int, Value, bool) {
	return nil, Value{}, false // не используется в этих тестах
}

func oidKeyStr(o []int) string {
	s := ""
	for i, x := range o {
		if i > 0 {
			s += "."
		}
		s += itoa(x)
	}
	return s
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestAgentHandleGet(t *testing.T) {
	oid := []int{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1}
	h := fakeHandler{tbl: map[string]Value{oidKeyStr(oid): IntValue(230)}}
	a := &Agent{community: "public", handler: h}

	req := EncodeMessage(&Message{Version: Version2c, Community: "public", PDU: &PDU{
		Type: PDUGetRequest, RequestID: 7, Varbinds: []Varbind{{OID: oid, Value: encodeNull()}},
	}})
	resp := a.handlePacket(req)
	if resp == nil {
		t.Fatal("нет ответа")
	}
	m, err := DecodeMessage(resp)
	if err != nil {
		t.Fatalf("decode ответ: %v", err)
	}
	if m.PDU.Type != PDUGetResponse || m.PDU.RequestID != 7 {
		t.Fatalf("pdu: %+v", m.PDU)
	}
	if len(m.PDU.Varbinds) != 1 {
		t.Fatalf("varbinds=%d", len(m.PDU.Varbinds))
	}
	v, _ := DecodeValue(m.PDU.Varbinds[0].Value)
	if v.Kind != VInt || v.Int != 230 {
		t.Fatalf("value=%+v", v)
	}
}

func TestAgentHandleGetUnknown(t *testing.T) {
	oid := []int{1, 3, 6, 1, 4, 1, 50330, 9, 9, 9}
	a := &Agent{community: "public", handler: fakeHandler{tbl: map[string]Value{}}}
	req := EncodeMessage(&Message{Version: Version2c, Community: "public", PDU: &PDU{
		Type: PDUGetRequest, RequestID: 9, Varbinds: []Varbind{{OID: oid, Value: encodeNull()}},
	}})
	resp := a.handlePacket(req)
	m, err := DecodeMessage(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	v, _ := DecodeValue(m.PDU.Varbinds[0].Value)
	if v.Kind != VExcep || v.Excep != ExcepNoSuchInstance {
		t.Fatalf("ожидали noSuchInstance, получили %+v", v)
	}
}

func TestAgentIgnoresWrongCommunity(t *testing.T) {
	oid := []int{1, 3, 6, 1, 4, 1, 50330, 1, 1, 1}
	a := &Agent{community: "secret", handler: fakeHandler{tbl: map[string]Value{oidKeyStr(oid): IntValue(1)}}}
	req := EncodeMessage(&Message{Version: Version2c, Community: "public", PDU: &PDU{
		Type: PDUGetRequest, RequestID: 1, Varbinds: []Varbind{{OID: oid, Value: encodeNull()}},
	}})
	if resp := a.handlePacket(req); resp != nil {
		t.Fatal("неверная community — ответа быть не должно")
	}
}
