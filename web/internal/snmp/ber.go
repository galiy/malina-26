// Package snmp — SNMP v2c поверх собственного BER/ASN.1-кода.
//
// Реализует минимальный, но корректный набор для двух задач проекта
// (docs/snmp.md):
//   - SNMP-агент (UDP-слушатель, GET/GETNEXT/WALK) — опрос телеметрии внешней
//     системой мониторинга;
//   - SNMP-trap (v2c) — активная отправка уведомлений движком событий и
//     периодическим «Мониторингом».
//
// Пакет протокольный: знает только BER-кодирование и структуру сообщений.
// За маппинг OID → значение (источник — shm наших демонов) отвечает слой выше.
package snmp

import "errors"

// Метки BER-типов, используемых SNMPv2.
const (
	tagInteger     = 0x02
	tagOctetString = 0x04
	tagNull        = 0x05
	tagOID         = 0x06
	tagOpaque      = 0x44
	tagCounter32   = 0x41
	tagGauge32     = 0x42
	tagTimeTicks   = 0x43
	tagCounter64   = 0x46
	tagSequence    = 0x30
	tagContext     = 0xA0 // constructed [0] — верхнеуровневый IPDU (v1/GetNext и т.п.)
)

// Ошибки декодера.
var (
	ErrTruncated = errors.New("snmp: сообщение обрезано")
	ErrBadTLV    = errors.New("snmp: некорректный TLV")
)

// tlv — разобранный блок (тег + содержимое).
type tlv struct {
	tag     byte
	content []byte
}

// decodeTLV читает одну TLV-структуру с начала b. Возвращает блок и число
// потреблённых байт.
func decodeTLV(b []byte) (tlv, int, error) {
	if len(b) < 2 {
		return tlv{}, 0, ErrTruncated
	}
	tag := b[0]
	i := 1
	length := uint32(b[i])
	i++
	if length&0x80 != 0 {
		n := int(length & 0x7F)
		if n == 0 || n > 4 || i+n > len(b) {
			return tlv{}, 0, ErrBadTLV
		}
		length = 0
		for j := 0; j < n; j++ {
			length = length<<8 | uint32(b[i+j])
		}
		i += n
	}
	if int(length) > len(b)-i {
		return tlv{}, 0, ErrTruncated
	}
	return tlv{tag: tag, content: b[i : i+int(length)]}, i + int(length), nil
}

// encodeTLV собирает блок tag+len+content.
func encodeTLV(tag byte, content []byte) []byte {
	out := []byte{tag}
	out = appendLen(out, len(content))
	return append(out, content...)
}

// appendLen дописывает длину в BER-форме (short/long).
func appendLen(b []byte, n int) []byte {
	if n < 0x80 {
		return append(b, byte(n))
	}
	var tmp [4]byte
	i := len(tmp)
	nn := n
	for nn > 0 {
		i--
		tmp[i] = byte(nn)
		nn >>= 8
	}
	b = append(b, byte(0x80|len(tmp)-i))
	return append(b, tmp[i:]...)
}

// encodeInt кодирует целое в минимальном числе знаковых октетов.
func encodeInt(v int64) []byte {
	var tmp [8]byte
	i := len(tmp)
	vv := v
	for {
		i--
		tmp[i] = byte(vv)
		vv >>= 8
		if (vv == 0 && tmp[i]&0x80 == 0) || (vv == -1 && tmp[i]&0x80 != 0) {
			break
		}
	}
	return tmp[i:]
}

// encodeInteger — INTEGER-блок.
func encodeInteger(v int64) []byte { return encodeTLV(tagInteger, encodeInt(v)) }

// encodeOctetString кодирует OCTET STRING.
func encodeOctetString(b []byte) []byte { return encodeTLV(tagOctetString, b) }

// encodeNull кодирует NULL.
func encodeNull() []byte { return encodeTLV(tagNull, nil) }

// Коды исключений SNMPv2 (RFC 3416).
const (
	ExcepNoSuchObject   = 0x80
	ExcepNoSuchInstance = 0x81
	ExcepEndOfMibView   = 0x82
)

// encodeException кодирует значение-исключение SNMPv2 (noSuchObject,
// noSuchInstance, endOfMibView). По RFC 3416 это context-specific primitive
// теги [0]/[1]/[2] IMPLICIT NULL с нулевой длиной (на проводе `0x80..0x82 00`).
func encodeException(code byte) []byte { return encodeTLV(code, nil) }

// encodeOID кодирует OBJECT IDENTIFIER из целочисленного пути.
func encodeOID(oids []int) []byte {
	buf := appendSubID(nil, oids[0]*40+oids[1])
	for _, o := range oids[2:] {
		buf = appendSubID(buf, o)
	}
	return encodeTLV(tagOID, buf)
}

// appendSubID дописывает суб-идентификатор в base-128 (старший бит — продолжение).
func appendSubID(b []byte, v int) []byte {
	if v == 0 {
		return append(b, 0)
	}
	var tmp [5]byte
	i := len(tmp)
	vv := uint32(v)
	for {
		i--
		tmp[i] = byte(vv & 0x7F)
		vv >>= 7
		if vv == 0 {
			break
		}
	}
	for j := i; j < len(tmp)-1; j++ {
		b = append(b, tmp[j]|0x80)
	}
	return append(b, tmp[len(tmp)-1])
}

// decodeOIDContent разбирает содержимое OID-блока (без тега/длины).
func decodeOIDContent(b []byte) ([]int, error) {
	if len(b) == 0 {
		return nil, ErrBadTLV
	}
	var oids []int
	// первый байт (возможно многобайтовый) сворачивает X*40+Y.
	sub, rest, err := decodeSubID(b)
	if err != nil {
		return nil, err
	}
	oids = append(oids, sub/40, sub%40)
	for len(rest) > 0 {
		sub, rest, err = decodeSubID(rest)
		if err != nil {
			return nil, err
		}
		oids = append(oids, sub)
	}
	return oids, nil
}

// decodeSubID читает base-128 суб-идентификатор с начала b.
func decodeSubID(b []byte) (int, []byte, error) {
	var v uint32
	for i := 0; i < len(b); i++ {
		v = v<<7 | uint32(b[i]&0x7F)
		if b[i]&0x80 == 0 {
			return int(v), b[i+1:], nil
		}
	}
	return 0, nil, ErrBadTLV
}

// parseTLVs — прочитать подряд идущие TLV-блоки, пока не кончился buf.
func parseTLVs(buf []byte) ([]tlv, error) {
	var out []tlv
	for len(buf) > 0 {
		t, n, err := decodeTLV(buf)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		buf = buf[n:]
	}
	return out, nil
}
