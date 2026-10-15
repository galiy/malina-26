// Package tsdbclient — клиент хранилища tsdb (двоичные кадры §10) по Unix-сокету.
// Общий для модуля сохранения пулеров (lib/stat) и аккумулятора в web:
// один кодек, без дублирования серверного wire.go.
package tsdbclient

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net"
	"time"
)

// Магии кадров (§10).
const (
	magicWriteReq  = 0x5753544D // "MTSW"
	magicWriteAck  = 0x4B53544D // "MTSK"
	magicReadReq   = 0x5253544D // "MTSR"
	magicReadReply = 0x4153544D // "MTSA"
	magicReadEnd   = 0x4553544D // "MTSE"
)

// Режимы запроса (§1.4).
const (
	ModeWholeOne    = 1
	ModeSelectedOne = 2
	ModeSelectedMul = 3
	ModeWholeMul    = 4
)

// Статусы ответа чтения.
const (
	StatusOK      = 0
	StatusBadMode = 1
	StatusTooWide = 2
	StatusTooMany = 3
)

// Направление выборки (§9).
const (
	DirectionFrom uint8 = 0 // вперёд от from
	DirectionTo   uint8 = 1 // назад от to
)

var errFrame = errors.New("битый кадр tsdb")

// Row — строка для записи/чтения.
type Row struct {
	Type    string
	ID      string
	Time    int64
	Version uint16
	Payload []byte
}

// Client — подключение по Unix-сокету (соединение на вызов).
type Client struct {
	Sock    string
	Timeout time.Duration
}

func New(sock string) *Client { return &Client{Sock: sock, Timeout: 5 * time.Second} }

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return c.Timeout
}

func (c *Client) roundtrip(req []byte) ([]byte, error) {
	conn, err := net.DialTimeout("unix", c.Sock, c.timeout())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout()))
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(req)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return nil, err
	}
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64*1024)
	if _, err := readFull(br, hdr[:]); err != nil {
		return nil, err
	}
	l := binary.LittleEndian.Uint32(hdr[:])
	if l == 0 || l > 64<<20 {
		return nil, errFrame
	}
	buf := make([]byte, l)
	if _, err := readFull(br, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func readFull(br *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := br.Read(b[n:])
		if m > 0 {
			n += m
		}
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// --- кодирование ---

func appendStr(b []byte, s string) []byte {
	b = append(b, byte(len(s)))
	return append(b, s...)
}

func finish(b []byte) []byte { return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b)) }

func readStrAt(b []byte, pos int) (string, int, error) {
	if pos >= len(b) {
		return "", pos, errFrame
	}
	n := int(b[pos])
	pos++
	if pos+n > len(b) {
		return "", pos, errFrame
	}
	return string(b[pos : pos+n]), pos + n, nil
}

func checkCRC(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return crc32.ChecksumIEEE(b[:len(b)-4]) == binary.LittleEndian.Uint32(b[len(b)-4:])
}

// EncodeWriteReq собирает кадр записи (батч).
func EncodeWriteReq(rows []Row) []byte {
	b := make([]byte, 0, 64+len(rows)*32)
	b = binary.LittleEndian.AppendUint32(b, magicWriteReq)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(rows)))
	var tmp [binary.MaxVarintLen64]byte
	for _, r := range rows {
		b = appendStr(b, r.Type)
		b = appendStr(b, r.ID)
		b = binary.LittleEndian.AppendUint64(b, uint64(r.Time))
		b = binary.LittleEndian.AppendUint16(b, r.Version)
		n := binary.PutUvarint(tmp[:], uint64(len(r.Payload)))
		b = append(b, tmp[:n]...)
		b = append(b, r.Payload...)
	}
	return finish(b)
}

// EncodeReadReq собирает кадр чтения. bucket — мини-окно decimation в мс
// (0 = без прореживания), §9 docs/telemetry-store.md.
func EncodeReadReq(mode uint8, typ string, ids []string, metrics []uint16, from, to int64, direction uint8, limit uint32, bucket uint32) []byte {
	b := make([]byte, 0, 64)
	b = binary.LittleEndian.AppendUint32(b, magicReadReq)
	b = append(b, mode)
	b = appendStr(b, typ)
	b = append(b, byte(len(ids)))
	for _, id := range ids {
		b = appendStr(b, id)
	}
	b = append(b, byte(len(metrics)))
	for _, m := range metrics {
		b = binary.LittleEndian.AppendUint16(b, m)
	}
	b = binary.LittleEndian.AppendUint64(b, uint64(from))
	b = binary.LittleEndian.AppendUint64(b, uint64(to))
	b = append(b, direction)
	b = binary.LittleEndian.AppendUint32(b, limit)
	b = binary.LittleEndian.AppendUint32(b, bucket)
	return finish(b)
}

// --- операции ---

// Write отправляет батч строк; возвращает код ответа и число принятых строк.
func (c *Client) Write(rows []Row) (uint8, uint32, error) {
	rb, err := c.roundtrip(EncodeWriteReq(rows))
	if err != nil {
		return 0, 0, err
	}
	if len(rb) < 9 || binary.LittleEndian.Uint32(rb[0:4]) != magicWriteAck || !checkCRC(rb) {
		return 0, 0, errFrame
	}
	return rb[4], binary.LittleEndian.Uint32(rb[5:9]), nil
}

// Query выполняет чтение; возвращает статус и строки. bucket — мини-окно
// decimation в мс (0 = без прореживания).
func (c *Client) Query(mode uint8, typ string, ids []string, metrics []uint16, from, to int64, direction uint8, limit uint32, bucket uint32) (uint8, []Row, error) {
	rb, err := c.roundtrip(EncodeReadReq(mode, typ, ids, metrics, from, to, direction, limit, bucket))
	if err != nil {
		return 0, nil, err
	}
	return ParseReadReply(rb)
}

// --- мета: список устройств и крайние точки (MTSI/MTSO) ---

const (
	magicMetaReq   = 0x4953544D // "MTSI"
	magicMetaReply = 0x4F53544D // "MTSO"
	MetaList       = 1
	MetaExtremes   = 2
)

// DevInfo — устройство в списке.
type DevInfo struct {
	Type string
	ID   string
}

func encodeMetaReq(cmd uint8, typ, id string) []byte {
	b := make([]byte, 0, 16)
	b = binary.LittleEndian.AppendUint32(b, magicMetaReq)
	b = append(b, cmd)
	b = appendStr(b, typ)
	b = appendStr(b, id)
	return finish(b)
}

// List возвращает список устройств (из памяти сервера).
func (c *Client) List() ([]DevInfo, error) {
	rb, err := c.roundtrip(encodeMetaReq(MetaList, "", ""))
	if err != nil {
		return nil, err
	}
	if len(rb) < 4+1+2+4 || binary.LittleEndian.Uint32(rb[0:4]) != magicMetaReply || !checkCRC(rb) || rb[4] != MetaList {
		return nil, errFrame
	}
	body := rb[:len(rb)-4]
	pos := 5
	count := int(binary.LittleEndian.Uint16(body[pos : pos+2]))
	pos += 2
	out := make([]DevInfo, 0, count)
	for i := 0; i < count; i++ {
		var t, id string
		var e error
		if t, pos, e = readStrAt(body, pos); e != nil {
			return nil, errFrame
		}
		if id, pos, e = readStrAt(body, pos); e != nil {
			return nil, errFrame
		}
		out = append(out, DevInfo{Type: t, ID: id})
	}
	return out, nil
}

// Extremes возвращает [min,max] времени устройства; found=false — данных нет.
func (c *Client) Extremes(typ, id string) (int64, int64, bool, error) {
	rb, err := c.roundtrip(encodeMetaReq(MetaExtremes, typ, id))
	if err != nil {
		return 0, 0, false, err
	}
	if len(rb) < 4+1+1+8+8+4 || binary.LittleEndian.Uint32(rb[0:4]) != magicMetaReply || !checkCRC(rb) || rb[4] != MetaExtremes {
		return 0, 0, false, errFrame
	}
	body := rb[:len(rb)-4]
	pos := 5
	found := body[pos] == 1
	pos++
	min := int64(binary.LittleEndian.Uint64(body[pos : pos+8]))
	max := int64(binary.LittleEndian.Uint64(body[pos+8 : pos+16]))
	return min, max, found, nil
}

// ParseReadReply разбирает ответ чтения (MTSA | status | rows | MTSE | crc).
func ParseReadReply(b []byte) (uint8, []Row, error) {
	if len(b) < 9 || binary.LittleEndian.Uint32(b[0:4]) != magicReadReply || !checkCRC(b) {
		return 0, nil, errFrame
	}
	status := b[4]
	body := b[:len(b)-4]
	pos := 5
	var rows []Row
	for pos < len(body) {
		if pos+4 <= len(body) && binary.LittleEndian.Uint32(body[pos:pos+4]) == magicReadEnd {
			break
		}
		if pos >= len(body) {
			return status, rows, errFrame
		}
		tn := int(body[pos])
		pos++
		if pos+tn > len(body) {
			return status, rows, errFrame
		}
		typ := string(body[pos : pos+tn])
		pos += tn
		if pos >= len(body) {
			return status, rows, errFrame
		}
		idn := int(body[pos])
		pos++
		if pos+idn > len(body) {
			return status, rows, errFrame
		}
		id := string(body[pos : pos+idn])
		pos += idn
		if pos+10 > len(body) {
			return status, rows, errFrame
		}
		tm := int64(binary.LittleEndian.Uint64(body[pos : pos+8]))
		ver := binary.LittleEndian.Uint16(body[pos+8 : pos+10])
		pos += 10
		pl, m := binary.Uvarint(body[pos:])
		if m <= 0 {
			return status, rows, errFrame
		}
		pos += m
		if pos+int(pl) > len(body) {
			return status, rows, errFrame
		}
		payload := append([]byte(nil), body[pos:pos+int(pl)]...)
		pos += int(pl)
		rows = append(rows, Row{Type: typ, ID: id, Time: tm, Version: ver, Payload: payload})
	}
	return status, rows, nil
}
