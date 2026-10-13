package snmp

import "fmt"

// Версия протокола SNMP v2c.
const Version2c = 1

// Типы PDU, используемые проектом.
const (
	PDUGetRequest     byte = 0xA0
	PDUGetNextRequest byte = 0xA1
	PDUGetResponse    byte = 0xA2
	PDUSetRequest     byte = 0xA3
	PDUTrapV2         byte = 0xA7
)

// ValueKind — вид значения varbind.
type ValueKind byte

const (
	VNull  ValueKind = 0
	VInt   ValueKind = 1
	VStr   ValueKind = 2
	VExcep ValueKind = 3
)

// Value — типизированное значение varbind.
type Value struct {
	Kind  ValueKind
	Int   int64
	Str   []byte
	Excep byte // код исключения (ExcepNoSuch*)
}

// IntValue создаёт целочисленное значение (Integer32).
func IntValue(v int64) Value { return Value{Kind: VInt, Int: v} }

// StringValue создаёт строковое/октетное значение.
func StringValue(s []byte) Value { return Value{Kind: VStr, Str: s} }

// NullValue — пустой varbind.
func NullValue() Value { return Value{Kind: VNull} }

// ExceptionValue — значение-исключение SNMPv2.
func ExceptionValue(code byte) Value { return Value{Kind: VExcep, Excep: code} }

// Varbind — пара OID+значение.
type Varbind struct {
	OID   []int
	Value []byte // готовый BER-блок значения (не оборачивается в OID-тег)
}

// PDU — блок протокольных данных.
type PDU struct {
	Type        byte
	RequestID   int
	ErrorStatus int
	ErrorIndex  int
	Varbinds    []Varbind
}

// Message — SNMP-сообщение (version + community + PDU).
type Message struct {
	Version   int
	Community string
	PDU       *PDU
}

// EncodeValue кодирует значение в BER-блок (без внешнего оборачивания).
func EncodeValue(v Value) []byte {
	switch v.Kind {
	case VStr:
		return encodeOctetString(v.Str)
	case VExcep:
		return encodeException(v.Excep)
	case VNull:
		return encodeNull()
	default:
		return encodeInteger(v.Int)
	}
}

// DecodeValue разбирает BER-блок значения.
func DecodeValue(b []byte) (Value, error) {
	t, n, err := decodeTLV(b)
	if err != nil {
		return Value{}, err
	}
	_ = n
	switch t.tag {
	case tagInteger, tagCounter32, tagGauge32, tagTimeTicks, tagCounter64:
		// целые со знаком (Counter — беззнаковые, но для наших значений хватает int64)
		v, _, err := decodeInt(t.content)
		if err != nil {
			return Value{}, err
		}
		return IntValue(v), nil
	case tagOctetString:
		return StringValue(t.content), nil
	case tagNull:
		return NullValue(), nil
	case tagOpaque:
		return StringValue(t.content), nil
	case ExcepNoSuchObject, ExcepNoSuchInstance, ExcepEndOfMibView:
		// context-specific primitive теги исключений (нулевая длина)
		return ExceptionValue(t.tag), nil
	}
	return Value{}, fmt.Errorf("snmp: неожиданный тип значения 0x%02X", t.tag)
}

// decodeInt разбирает знаковое целое из BER-содержимого.
func decodeInt(b []byte) (int64, int, error) {
	if len(b) == 0 || len(b) > 8 {
		return 0, 0, ErrBadTLV
	}
	var v int64
	if b[0]&0x80 != 0 {
		v = -1
	}
	for _, x := range b {
		v = v<<8 | int64(x)
	}
	return v, len(b), nil
}

// EncodeVarbind кодирует пару OID+value.
func EncodeVarbind(o []int, v []byte) []byte {
	oid := encodeOID(o)
	return encodeTLV(tagSequence, append(oid, v...))
}

// EncodeMessage сериализует сообщение в сетевой вид.
func EncodeMessage(m *Message) []byte {
	ver := encodeInteger(int64(m.Version))
	comm := encodeOctetString([]byte(m.Community))
	seqContent := append(ver, comm...)
	seqContent = append(seqContent, buildPDU(m.PDU)...)
	return encodeTLV(tagSequence, seqContent)
}

// buildPDU собирает PDU-блок: [SEQUENCE] request-id, error-status, error-index, varbinds.
func buildPDU(p *PDU) []byte {
	var seq []byte
	seq = append(seq, encodeInteger(int64(p.RequestID))...)
	seq = append(seq, encodeInteger(int64(p.ErrorStatus))...)
	seq = append(seq, encodeInteger(int64(p.ErrorIndex))...)
	var s []byte
	for _, vb := range p.Varbinds {
		s = append(s, EncodeVarbind(vb.OID, vb.Value)...)
	}
	seq = append(seq, encodeTLV(tagSequence, s)...)
	return encodeTLV(p.Type, seq)
}

// DecodeMessage разбирает SNMP-сообщение.
func DecodeMessage(b []byte) (*Message, error) {
	root, n, err := decodeTLV(b)
	if err != nil {
		return nil, err
	}
	if root.tag != tagSequence {
		return nil, ErrBadTLV
	}
	_ = n
	items, err := parseTLVs(root.content)
	if err != nil {
		return nil, err
	}
	if len(items) < 3 {
		return nil, ErrBadTLV
	}
	verT, commT, pduT := items[0], items[1], items[2]
	if verT.tag != tagInteger {
		return nil, ErrBadTLV
	}
	ver, _, err := decodeInt(verT.content)
	if err != nil {
		return nil, ErrBadTLV
	}
	msg := &Message{Version: int(ver), Community: string(commT.content)}
	p, err := decodePDU(pduT)
	if err != nil {
		return nil, err
	}
	msg.PDU = p
	return msg, nil
}

func decodePDU(t tlv) (*PDU, error) {
	// верхнеуровневые PDU — constructed; содержимое — SEQUENCE.
	items, err := parseTLVs(t.content)
	if err != nil {
		return nil, err
	}
	if len(items) < 4 {
		return nil, ErrBadTLV
	}
	reqID, _, err := decodeInt(items[0].content)
	if err != nil {
		return nil, ErrBadTLV
	}
	es, _, err := decodeInt(items[1].content)
	if err != nil {
		return nil, ErrBadTLV
	}
	ei, _, err := decodeInt(items[2].content)
	if err != nil {
		return nil, ErrBadTLV
	}
	p := &PDU{Type: t.tag, RequestID: int(reqID), ErrorStatus: int(es), ErrorIndex: int(ei)}
	vbls, err := parseTLVs(items[3].content)
	if err != nil {
		return nil, err
	}
	for _, vb := range vbls {
		if vb.tag != tagSequence {
			continue
		}
		subs, err := parseTLVs(vb.content)
		if err != nil || len(subs) < 2 {
			return nil, ErrBadTLV
		}
		oid, err := decodeOIDContent(subs[0].content)
		if err != nil {
			return nil, err
		}
		// Value хранится как готовый BER-блок значения (тег+длина+содержимое).
		p.Varbinds = append(p.Varbinds, Varbind{OID: oid, Value: encodeTLV(subs[1].tag, subs[1].content)})
	}
	return p, nil
}

// TrapVarbinds — стандартные varbind'ы v2c-trap: sysUpTime.0 и snmpTrapOID.0.
// uptime — в сотых долях секунды (TimeTicks); trapOID — OID нотификации.
func TrapVarbinds(uptimeMillis int64, trapOID []int, extra ...Varbind) []Varbind {
	out := []Varbind{
		{OID: []int{1, 3, 6, 1, 2, 1, 1, 3, 0}, Value: encodeTLV(tagTimeTicks, encodeInt(uptimeMillis/10))},
		{OID: []int{1, 3, 6, 1, 6, 3, 1, 1, 4, 1, 0}, Value: encodeOID(trapOID)},
	}
	return append(out, extra...)
}
