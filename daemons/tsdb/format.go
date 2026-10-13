// format.go — двоичный формат сегмента (append-only, колоночные блоки).
//
//	segment := hdr block* [index_footer]
//
//	hdr:
//	  magic        u32  "MTS1"
//	  format_ver   u16
//	  device_type  str  (u8 len + UTF-8; map/kes/… — набор открыт)
//	  device_id    str  (u8 len + UTF-8; до 10 символов)
//	  time_base    i64  (мс)
//	  flags        u16
//	  crc32        u32  (по всем предыдущим байтам)
//
//	block:
//	  bhdr:
//	    rec_count  u16
//	    first_time i64
//	    last_time  i64
//	    version    u16  (версия контракта блока)
//	    col_count  u16  (время + поля)
//	    blen       u32  (длина ТЕЛА блока)
//	    crc32      u32  (по телу блока)
//	  col_offsets* : field_id u16 | offset u32 | len u32
//	                 (offset — от начала тела; field_id=0 — колонка времени)
//	  body := col_time | col_<field_id>*   (blen байт)
//	    col_time: rec_count дельт-varint от first_time
//	    col_<id>: rec_count значений типа поля из реестра контракта
//
//	index_footer (только у запечатанного, в хвосте файла):
//	  entries*: first_time i64 | offset u64 | blen u32
//	  entry_count u32 | magic u32 "MTSX" | crc32 u32
package main

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

const (
	segMagic   = 0x3153544D // "MTS1"
	idxMagic   = 0x5853544D // "MTSX"
	formatVer  = 2
	blkHdrSize = 30 // rec_count2 + first8 + last8 + ver2 + col2 + blen4 + crc4
	colEntSize = 10 // field_id2 + offset4 + len4
	idxEntSize = 20
	idxFootMin = 12
)

var (
	errShort = errors.New("короткий буфер")
	errMagic = errors.New("неверная магия")
	errCRC   = errors.New("несовпадение CRC")
)

func crc32of(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

// --- строки (u8 len + байты) ---

func appendStr(b []byte, s string) []byte {
	b = append(b, byte(len(s)))
	return append(b, s...)
}

func readStr(b []byte) (string, []byte, error) {
	if len(b) < 1 {
		return "", nil, errShort
	}
	n := int(b[0])
	if len(b) < 1+n {
		return "", nil, errShort
	}
	return string(b[1 : 1+n]), b[1+n:], nil
}

// --- заголовок сегмента ---

type segHeader struct {
	FormatVer  uint16
	DeviceType string
	DeviceID   string
	TimeBase   int64
	Flags      uint16
}

func encodeSegHeader(h segHeader) []byte {
	b := make([]byte, 0, 32)
	b = binary.LittleEndian.AppendUint32(b, segMagic)
	b = binary.LittleEndian.AppendUint16(b, h.FormatVer)
	b = appendStr(b, h.DeviceType)
	b = appendStr(b, h.DeviceID)
	b = binary.LittleEndian.AppendUint64(b, uint64(h.TimeBase))
	b = binary.LittleEndian.AppendUint16(b, h.Flags)
	return binary.LittleEndian.AppendUint32(b, crc32of(b))
}

func readStrAt(b []byte, pos int) (string, int, error) {
	if pos >= len(b) {
		return "", pos, errShort
	}
	n := int(b[pos])
	pos++
	if pos+n > len(b) {
		return "", pos, errShort
	}
	return string(b[pos : pos+n]), pos + n, nil
}

// decodeSegHeader разбирает заголовок из буфера b (b может быть длиннее) и
// возвращает разобранный заголовок и длину заголовка в байтах.
func decodeSegHeader(b []byte) (segHeader, int, error) {
	var h segHeader
	if len(b) < 6 {
		return h, 0, errShort
	}
	if binary.LittleEndian.Uint32(b[0:4]) != segMagic {
		return h, 0, errMagic
	}
	h.FormatVer = binary.LittleEndian.Uint16(b[4:6])
	pos := 6
	var err error
	if h.DeviceType, pos, err = readStrAt(b, pos); err != nil {
		return h, 0, err
	}
	if h.DeviceID, pos, err = readStrAt(b, pos); err != nil {
		return h, 0, err
	}
	if len(b) < pos+10+4 {
		return h, 0, errShort
	}
	h.TimeBase = int64(binary.LittleEndian.Uint64(b[pos : pos+8]))
	h.Flags = binary.LittleEndian.Uint16(b[pos+8 : pos+10])
	pos += 10
	if crc32of(b[:pos]) != binary.LittleEndian.Uint32(b[pos:pos+4]) {
		return h, 0, errCRC
	}
	return h, pos + 4, nil
}

// headerSize — длина закодированного заголовка.
func headerSize(h segHeader) int64 {
	return 4 + 2 + 1 + int64(len(h.DeviceType)) + 1 + int64(len(h.DeviceID)) + 8 + 2 + 4
}

// --- блок ---

type column struct {
	FieldID uint16
	Data    []byte
}

type blockMeta struct {
	RecCount  int
	FirstTime int64
	LastTime  int64
	Version   uint16
	ColCount  int
	Blen      uint32
}

type colEntry struct {
	FieldID uint16
	Offset  uint32
	Len     uint32
}

func encodeBlock(firstTime, lastTime int64, version uint16, recCount int, cols []column) []byte {
	body := make([]byte, 0)
	entries := make([]colEntry, len(cols))
	for i, c := range cols {
		entries[i] = colEntry{FieldID: c.FieldID, Offset: uint32(len(body)), Len: uint32(len(c.Data))}
		body = append(body, c.Data...)
	}
	hdr := make([]byte, 0, blkHdrSize+len(cols)*colEntSize)
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(recCount))
	hdr = binary.LittleEndian.AppendUint64(hdr, uint64(firstTime))
	hdr = binary.LittleEndian.AppendUint64(hdr, uint64(lastTime))
	hdr = binary.LittleEndian.AppendUint16(hdr, version)
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(len(cols)))
	hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(body)))
	hdr = binary.LittleEndian.AppendUint32(hdr, crc32of(body))
	for _, e := range entries {
		hdr = binary.LittleEndian.AppendUint16(hdr, e.FieldID)
		hdr = binary.LittleEndian.AppendUint32(hdr, e.Offset)
		hdr = binary.LittleEndian.AppendUint32(hdr, e.Len)
	}
	return append(hdr, body...)
}

func decodeBlockHeader(b []byte) (blockMeta, error) {
	if len(b) < blkHdrSize {
		return blockMeta{}, errShort
	}
	return blockMeta{
		RecCount:  int(binary.LittleEndian.Uint16(b[0:2])),
		FirstTime: int64(binary.LittleEndian.Uint64(b[2:10])),
		LastTime:  int64(binary.LittleEndian.Uint64(b[10:18])),
		Version:   binary.LittleEndian.Uint16(b[18:20]),
		ColCount:  int(binary.LittleEndian.Uint16(b[20:22])),
		Blen:      binary.LittleEndian.Uint32(b[22:26]),
	}, nil
}

func decodeColEntries(b []byte, colCount int) ([]colEntry, error) {
	if len(b) < blkHdrSize+colCount*colEntSize {
		return nil, errShort
	}
	out := make([]colEntry, colCount)
	pos := blkHdrSize
	for i := 0; i < colCount; i++ {
		out[i] = colEntry{
			FieldID: binary.LittleEndian.Uint16(b[pos : pos+2]),
			Offset:  binary.LittleEndian.Uint32(b[pos+2 : pos+6]),
			Len:     binary.LittleEndian.Uint32(b[pos+6 : pos+10]),
		}
		pos += colEntSize
	}
	return out, nil
}

// timeColumn декодирует колонку времени в абсолютные метки.
func timeColumn(data []byte, firstTime int64, recCount int) ([]int64, error) {
	out := make([]int64, recCount)
	base := uint64(firstTime)
	pos := 0
	for i := 0; i < recCount; i++ {
		d, n := binary.Uvarint(data[pos:])
		if n <= 0 {
			return nil, errShort
		}
		pos += n
		base += d
		out[i] = int64(base)
	}
	return out, nil
}

// --- index_footer ---

type idxEntry struct {
	FirstTime int64
	Offset    uint64
	Blen      uint32
}

func encodeIndexFooter(entries []idxEntry) []byte {
	buf := make([]byte, len(entries)*idxEntSize+idxFootMin)
	for i, e := range entries {
		o := i * idxEntSize
		binary.LittleEndian.PutUint64(buf[o:o+8], uint64(e.FirstTime))
		binary.LittleEndian.PutUint64(buf[o+8:o+16], e.Offset)
		binary.LittleEndian.PutUint32(buf[o+16:o+20], e.Blen)
	}
	entLen := len(entries) * idxEntSize
	binary.LittleEndian.PutUint32(buf[entLen:entLen+4], uint32(len(entries)))
	binary.LittleEndian.PutUint32(buf[entLen+4:entLen+8], idxMagic)
	binary.LittleEndian.PutUint32(buf[entLen+8:entLen+12], crc32of(buf[:entLen]))
	return buf
}

func decodeIndexFooter(data []byte) ([]idxEntry, int, bool) {
	if len(data) < idxFootMin {
		return nil, 0, false
	}
	pos := len(data) - idxFootMin
	count := binary.LittleEndian.Uint32(data[pos : pos+4])
	if binary.LittleEndian.Uint32(data[pos+4:pos+8]) != idxMagic {
		return nil, 0, false
	}
	entLen := int(count) * idxEntSize
	footLen := entLen + idxFootMin
	if footLen > len(data) {
		return nil, 0, false
	}
	entStart := len(data) - footLen
	ents := data[entStart : entStart+entLen]
	if crc32of(ents) != binary.LittleEndian.Uint32(data[pos+8:pos+12]) {
		return nil, 0, false
	}
	out := make([]idxEntry, count)
	for i := range out {
		o := i * idxEntSize
		out[i] = idxEntry{
			FirstTime: int64(binary.LittleEndian.Uint64(ents[o : o+8])),
			Offset:    binary.LittleEndian.Uint64(ents[o+8 : o+16]),
			Blen:      binary.LittleEndian.Uint32(ents[o+16 : o+20]),
		}
	}
	return out, footLen, true
}

// readBlockRaw читает заголовок+col_offsets+тело блока по смещению.
func readBlockRaw(f io.ReaderAt, off uint64) (blockMeta, []colEntry, []byte, error) {
	var hb [blkHdrSize]byte
	if _, err := f.ReadAt(hb[:], int64(off)); err != nil {
		return blockMeta{}, nil, nil, err
	}
	meta, err := decodeBlockHeader(hb[:])
	if err != nil {
		return blockMeta{}, nil, nil, err
	}
	total := blkHdrSize + meta.ColCount*colEntSize + int(meta.Blen)
	buf := make([]byte, total)
	if _, err := f.ReadAt(buf, int64(off)); err != nil {
		return blockMeta{}, nil, nil, err
	}
	body := buf[blkHdrSize+meta.ColCount*colEntSize:]
	if crc32of(body) != binary.LittleEndian.Uint32(hb[26:30]) {
		return blockMeta{}, nil, nil, errCRC
	}
	ents, err := decodeColEntries(buf, meta.ColCount)
	if err != nil {
		return blockMeta{}, nil, nil, err
	}
	return meta, ents, body, nil
}
