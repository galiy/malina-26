// wire.go — двоичные кадры API (§10). little-endian; str = u8 длина + UTF-8;
// varint = LEB128. Каждый кадр завершается crc32 по всем предыдущим байтам кадра.
package main

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	magicWriteReq  = 0x5753544D // "MTSW"
	magicWriteAck  = 0x4B53544D // "MTSK"
	magicReadReq   = 0x5253544D // "MTSR"
	magicReadReply = 0x4153544D // "MTSA"
	magicReadEnd   = 0x4553544D // "MTSE"

	maxFrame = 64 << 20
)

// Режимы запроса (§1.4).
const (
	modeWholeOne    = 1
	modeSelectedOne = 2
	modeSelectedMul = 3
	modeWholeMul    = 4
)

// Коды статуса ответа чтения (read_reply.status).
const (
	statusOK      uint8 = 0 // успех
	statusBadMode uint8 = 1 // неверные параметры запроса/режима
	statusTooWide uint8 = 2 // период превышает лимит
	statusTooMany uint8 = 3 // слишком много строк в ответе
)

var errWire = errors.New("битый кадр API")

// queryReq — разобранный кадр чтения.
type queryReq struct {
	Mode    uint8
	Type    string
	IDs     []string
	Metrics []uint16
	From    int64
	To      int64
}

func finish(b []byte) []byte { return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b)) }

func checkCRC(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return crc32.ChecksumIEEE(b[:len(b)-4]) == binary.LittleEndian.Uint32(b[len(b)-4:])
}

// --- запись ---

func decodeWriteReq(b []byte) ([]Row, error) {
	if len(b) < 4+2+4 || binary.LittleEndian.Uint32(b[0:4]) != magicWriteReq || !checkCRC(b) {
		return nil, errWire
	}
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	pos := 6
	rows := make([]Row, 0, count)
	for i := 0; i < count; i++ {
		var r Row
		var err error
		if r.Type, pos, err = readStrAt(b, pos); err != nil {
			return nil, errWire
		}
		if r.ID, pos, err = readStrAt(b, pos); err != nil {
			return nil, errWire
		}
		if pos+8+2 > len(b) {
			return nil, errWire
		}
		r.Time = int64(binary.LittleEndian.Uint64(b[pos : pos+8]))
		r.Version = binary.LittleEndian.Uint16(b[pos+8 : pos+10])
		pos += 10
		pl, n := binary.Uvarint(b[pos:])
		if n <= 0 {
			return nil, errWire
		}
		pos += n
		if pos+int(pl) > len(b) {
			return nil, errWire
		}
		r.Payload = append([]byte(nil), b[pos:pos+int(pl)]...)
		pos += int(pl)
		rows = append(rows, r)
	}
	return rows, nil
}

func encodeWriteAck(code uint8, written uint32) []byte {
	b := make([]byte, 0, 13)
	b = binary.LittleEndian.AppendUint32(b, magicWriteAck)
	b = append(b, code)
	b = binary.LittleEndian.AppendUint32(b, written)
	return finish(b)
}

// --- чтение (запрос) ---

func decodeReadReq(b []byte) (queryReq, error) {
	var q queryReq
	if len(b) < 4+1+4 || binary.LittleEndian.Uint32(b[0:4]) != magicReadReq || !checkCRC(b) {
		return q, errWire
	}
	body := b[:len(b)-4]
	pos := 4
	q.Mode = body[pos]
	pos++
	var err error
	if q.Type, pos, err = readStrAt(body, pos); err != nil {
		return q, errWire
	}
	if pos >= len(body) {
		return q, errWire
	}
	dn := int(body[pos])
	pos++
	for i := 0; i < dn; i++ {
		var id string
		if id, pos, err = readStrAt(body, pos); err != nil {
			return q, errWire
		}
		q.IDs = append(q.IDs, id)
	}
	if pos >= len(body) {
		return q, errWire
	}
	mn := int(body[pos])
	pos++
	for i := 0; i < mn; i++ {
		if pos+2 > len(body) {
			return q, errWire
		}
		q.Metrics = append(q.Metrics, binary.LittleEndian.Uint16(body[pos:pos+2]))
		pos += 2
	}
	if pos+16 > len(body) {
		return q, errWire
	}
	q.From = int64(binary.LittleEndian.Uint64(body[pos : pos+8]))
	q.To = int64(binary.LittleEndian.Uint64(body[pos+8 : pos+16]))
	return q, nil
}

// --- чтение (ответ) ---

func encodeReadReply(status uint8, rows []Row) []byte {
	b := make([]byte, 0, 64)
	b = binary.LittleEndian.AppendUint32(b, magicReadReply)
	b = append(b, status)
	for _, r := range rows {
		b = appendStr(b, r.Type)
		b = appendStr(b, r.ID)
		b = binary.LittleEndian.AppendUint64(b, uint64(r.Time))
		b = binary.LittleEndian.AppendUint16(b, r.Version)
		var tmp [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(tmp[:], uint64(len(r.Payload)))
		b = append(b, tmp[:n]...)
		b = append(b, r.Payload...)
	}
	b = binary.LittleEndian.AppendUint32(b, magicReadEnd)
	return finish(b)
}
