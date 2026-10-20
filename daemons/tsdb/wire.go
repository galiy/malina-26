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

	magicMetaReq   = 0x4953544D // "MTSI" — мета-запрос (list/extremes)
	magicMetaReply = 0x4F53544D // "MTSO" — мета-ответ

	magicOffsetReq   = 0x5053544D // "MTSP" — committed offset / quiesce
	magicOffsetReply = 0x5153544D // "MTSQ" — ответ

	maxFrame = 64 << 20
)

// Мета-команды (meta_req.cmd).
const (
	metaList     = 1 // список устройств
	metaExtremes = 2 // крайние точки устройства
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
	Mode      uint8
	Type      string
	IDs       []string
	Metrics   []uint16
	From      int64
	To        int64
	Direction uint8  // 0 = from (вперёд), 1 = to (назад) — §9
	Limit     uint32 // 0 = без лимита
	Bucket    uint32 // мини-окно decimation, мс (0 = без прореживания) — §9
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
	pos += 16
	if pos+1+4+4 > len(body) {
		return q, errWire
	}
	q.Direction = body[pos]
	if q.Direction > 1 { // допустимы только from(0)/to(1) — §9
		return q, errWire
	}
	q.Limit = binary.LittleEndian.Uint32(body[pos+1 : pos+5])
	q.Bucket = binary.LittleEndian.Uint32(body[pos+5 : pos+9])
	return q, nil
}

// --- committed offset / quiesce ---

type offsetReq struct {
	Mode    uint8 // 0 = current, 1 = quiesce (финальный)
	Type    string
	ID      string
	Segment string
}

// offset_frame := magic u32 | mode u8 | type str | id str | segment str | crc32
func decodeOffsetReq(b []byte) (offsetReq, error) {
	var q offsetReq
	if len(b) < 4+1+4 || binary.LittleEndian.Uint32(b[0:4]) != magicOffsetReq || !checkCRC(b) {
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
	if q.ID, pos, err = readStrAt(body, pos); err != nil {
		return q, errWire
	}
	if q.Segment, pos, err = readStrAt(body, pos); err != nil {
		return q, errWire
	}
	if pos != len(body) {
		return q, errWire
	}
	if q.Mode > 1 {
		return q, errWire
	}
	return q, nil
}

// offset_reply := magic u32 | code u8 | committed u64 | final u8 | crc32
func encodeOffsetReply(code uint8, committed uint64, final bool) []byte {
	b := make([]byte, 0, 18)
	b = binary.LittleEndian.AppendUint32(b, magicOffsetReply)
	b = append(b, code)
	b = binary.LittleEndian.AppendUint64(b, committed)
	if final {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return finish(b)
}

// --- мета (list / extremes) ---

type metaReq struct {
	Cmd  uint8
	Type string
	ID   string
}

// meta_req := magic u32 | cmd u8 | type str | id str | crc32
func decodeMetaReq(b []byte) (metaReq, error) {
	var m metaReq
	if len(b) < 4+1+4 || binary.LittleEndian.Uint32(b[0:4]) != magicMetaReq || !checkCRC(b) {
		return m, errWire
	}
	body := b[:len(b)-4]
	pos := 4
	m.Cmd = body[pos]
	pos++
	var err error
	if m.Type, pos, err = readStrAt(body, pos); err != nil {
		return m, errWire
	}
	if m.ID, pos, err = readStrAt(body, pos); err != nil {
		return m, errWire
	}
	return m, nil
}

// meta_reply (list) := magic | cmd | count u16 | {type str, id str}* | crc32
func encodeMetaList(devs []devMeta) []byte {
	b := make([]byte, 0, 64+len(devs)*16)
	b = binary.LittleEndian.AppendUint32(b, magicMetaReply)
	b = append(b, metaList)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(devs)))
	for _, d := range devs {
		b = appendStr(b, d.Type)
		b = appendStr(b, d.ID)
	}
	return finish(b)
}

// meta_reply (extremes) := magic | cmd | found u8 | min i64 | max i64 | crc32
func encodeMetaExtremes(found bool, min, max int64) []byte {
	b := make([]byte, 0, 32)
	b = binary.LittleEndian.AppendUint32(b, magicMetaReply)
	b = append(b, metaExtremes)
	if found {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = binary.LittleEndian.AppendUint64(b, uint64(min))
	b = binary.LittleEndian.AppendUint64(b, uint64(max))
	return finish(b)
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
